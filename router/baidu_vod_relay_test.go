package router

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Fork: the built-in baidu-vod plugin sends each model family to the format
// Baidu's model service serves it in, with the channel key as a Bearer token
// and nowhere else. The models are ones with a default price; the routes pick
// the family by the prefix of the name.
func TestBaiduModelServicePluginRoutesEachFamily(t *testing.T) {
	const (
		key         = "sk-baidu-test"
		claudeReply = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`
		geminiReply = `{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`
		geminiSSE   = "data: " + geminiReply + "\n\n"
		geminiBody  = `{"contents":[{"role":"user","parts":[{"text":"hello there"}]}]}`
		geminiModel = "/v1beta/models/gemini-2.5-flash"
		// The old system's path prefix of the Gemini format.
		geminiPrefix = "/v3/chat/gc"
		models       = "gpt-5.5,gpt-image-1,claude-opus-4-6,gemini-2.5-flash"
	)
	_, found := jsplugin.DefaultRegistry.Get("baidu-vod")
	require.True(t, found, "the built-in plugin is not served")

	harness := newRelayHarness(t)
	chat := func(model, extra string) string {
		return `{"model":"` + model + `","messages":[{"role":"user","content":"hello there"}],"max_tokens":32` + extra + `}`
	}

	for name, tc := range map[string]struct {
		path, body string
		// geminiAddress is set when the channel has a Gemini format address,
		// which is the upstream under geminiPrefix.
		geminiAddress bool
		// reply is the upstream's reply, a chat completion when empty.
		reply     string
		wantPath  string
		wantQuery string
		sent      func(t *testing.T, request capturedRequest)
	}{
		"gpt on chat completions": {
			path: "/v1/chat/completions", body: chat("gpt-5.5", ""),
			wantPath: "/v1/chat/completions",
		},
		"claude on chat completions": {
			path: "/v1/chat/completions", body: chat("claude-opus-4-6", ""), reply: claudeReply,
			wantPath: "/v1/messages",
		},
		"claude in its own format": {
			path: "/v1/messages", body: chat("claude-opus-4-6", ""), reply: claudeReply,
			wantPath: "/v1/messages",
		},
		"gemini on chat completions": {
			path: "/v1/chat/completions", body: chat("gemini-2.5-flash", ""), reply: geminiReply,
			wantPath: geminiModel + ":generateContent",
			sent: func(t *testing.T, request capturedRequest) {
				assert.True(t, gjson.Get(request.Body, "contents").IsArray(), request.Body)
			},
		},
		"gemini stream on chat completions": {
			path: "/v1/chat/completions", body: chat("gemini-2.5-flash", `,"stream":true`), reply: geminiSSE,
			wantPath: geminiModel + ":streamGenerateContent", wantQuery: "alt=sse",
		},
		"gemini stream in its own format": {
			path: geminiModel + ":streamGenerateContent?alt=sse", body: geminiBody, reply: geminiSSE,
			wantPath: geminiModel + ":streamGenerateContent", wantQuery: "alt=sse",
		},
		"gemini at the Gemini format address": {
			path: geminiModel + ":generateContent", body: geminiBody, reply: geminiReply, geminiAddress: true,
			wantPath: geminiPrefix + geminiModel + ":generateContent",
		},
		"gemini stream at the Gemini format address": {
			path: geminiModel + ":streamGenerateContent?alt=sse", body: geminiBody, reply: geminiSSE, geminiAddress: true,
			wantPath: geminiPrefix + geminiModel + ":streamGenerateContent", wantQuery: "alt=sse",
		},
		"gpt-image without response_format": {
			path: "/v1/images/generations", body: `{"model":"gpt-image-1","prompt":"a cat","response_format":"b64_json"}`,
			reply:    `{"created":1730000000,"data":[{"b64_json":"AAEC"}]}`,
			wantPath: "/v1/images/generations",
			sent: func(t *testing.T, request capturedRequest) {
				assert.False(t, gjson.Get(request.Body, "response_format").Exists(), request.Body)
				assert.Equal(t, "a cat", gjson.Get(request.Body, "prompt").String())
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			channel := harness.pluginChannel("baidu-vod", key)
			channel.Models = models
			if tc.geminiAddress {
				channel.SetOtherSettings(dto.ChannelOtherSettings{PluginConfig: []byte(`{"geminiBaseUrl":"` + harness.upstream.URL + geminiPrefix + `"}`)})
			}
			harness.install(t, channel)
			harness.upstream.reset()
			if tc.reply != "" {
				harness.upstream.respond = func(w http.ResponseWriter, _ int, _ capturedRequest) {
					contentType := "application/json"
					if strings.HasPrefix(tc.reply, "data: ") {
						contentType = "text/event-stream"
					}
					w.Header().Set("Content-Type", contentType)
					_, _ = io.WriteString(w, tc.reply)
				}
				t.Cleanup(func() {
					harness.upstream.respond = func(w http.ResponseWriter, _ int, request capturedRequest) { writeChat(w, request) }
				})
			}

			outcome := harness.post(t, tc.path, tc.body)

			require.Equal(t, http.StatusOK, outcome.status, outcome.body)
			requests := harness.upstream.captured()
			require.Len(t, requests, 1)
			sent, err := url.Parse(requests[0].URL)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPath, sent.Path)
			assert.Equal(t, tc.wantQuery, sent.RawQuery, "the key is not in the address")
			assert.Equal(t, "Bearer "+key, requests[0].Header.Get("Authorization"))
			assert.Empty(t, requests[0].Header.Get("x-api-key"))
			assert.Empty(t, requests[0].Header.Get("x-goog-api-key"))
			assert.NotContains(t, requests[0].Body, key)
			if tc.sent != nil {
				tc.sent(t, requests[0])
			}
		})
	}

	// Baidu serves Claude in the Claude Messages format only and no converter
	// turns a Responses request into it, so the channel leaves such a request
	// to other channels instead of sending it in the OpenAI format.
	t.Run("claude on responses is not served", func(t *testing.T) {
		channel := harness.pluginChannel("baidu-vod", key)
		channel.Models = models
		harness.install(t, channel)
		harness.upstream.reset()

		outcome := harness.post(t, "/v1/responses", `{"model":"claude-opus-4-6","input":"hello there"}`)

		assert.Equal(t, http.StatusServiceUnavailable, outcome.status, outcome.body)
		assert.Empty(t, harness.upstream.captured())
	})
}

// Fork: the built-in vod-baidu-task plugin serves Baidu's asynchronous video
// interfaces and its speech synthesis at each vendor's own path. Its key sorts
// after the vendors' plugins, so a model or a path it shares with one of them
// stays that plugin's: the model-level price labels and the pricing of the
// vendor's own channels do not change.
func TestBaiduTaskPluginLeavesSharedModelsToTheirPlugins(t *testing.T) {
	generation := jsplugin.DefaultRegistry.Generation()
	plugin, found := generation.Get("vod-baidu-task")
	require.True(t, found, "the built-in plugin is not served")

	shared := map[string][]string{}
	for _, name := range plugin.Meta.Models {
		owner, ok := generation.GetByModel(name)
		require.True(t, ok, name)
		if owner == plugin {
			continue
		}
		shared[owner.Meta.Key] = append(shared[owner.Meta.Key], name)
		// The same usage facts, so the model's price applies to both plugins.
		ownerSchema, _ := owner.Meta.UsageForModel(name)
		schema, _ := plugin.Meta.UsageForModel(name)
		assert.ElementsMatch(t, slices.Collect(maps.Keys(ownerSchema)), slices.Collect(maps.Keys(schema)), name)
		for field, declared := range schema {
			assert.Equal(t, ownerSchema[field].Unit, declared.Unit, name+" "+field)
			assert.Equal(t, ownerSchema[field].Enum, declared.Enum, name+" "+field)
		}
	}
	assert.Equal(t, []string{"doubao", "hailuo", "kling", "vidu"}, slices.Sorted(maps.Keys(shared)))
	assert.Len(t, shared["doubao"], 4)
	assert.ElementsMatch(t, []string{"kling-4.0", "kling-v2-5-turbo", "kling-v2-6", "kling-v3"}, shared["kling"])
	assert.ElementsMatch(t, []string{"MiniMax-H3", "MiniMax-Hailuo-2.3", "MiniMax-Hailuo-2.3-Fast", "MiniMax-Hailuo-02"}, shared["hailuo"])
	assert.Len(t, shared["vidu"], 13)

	// A path both declare reaches the vendor's plugin first.
	for path, owner := range map[string]string{
		"/api/v3/contents/generations/tasks":                     "doubao",
		"/v1/videos/text2video":                                  "kling",
		"/text-to-video/kling-4.0":                               "kling",
		"/ent/v2/text2video":                                     "vidu",
		"/v2/video_generation":                                   "hailuo",
		"/v1/video_generation":                                   "hailuo",
		"/api/v1/services/aigc/video-generation/video-synthesis": "alibaba",
		"/openapi/v2/video/text/generate":                        "vod-baidu-task",
		"/openapi/v2/video/extend/generate":                      "vod-baidu-task",
		"/openapi/v2/video/fusion/generate":                      "vod-baidu-task",
		"/v1/videos/effects":                                     "vod-baidu-task",
		"/v1/t2a_v2":                                             "vod-baidu-task",
		"/v1/t2a_async_v2":                                       "vod-baidu-task",
		"/v1/voice_design":                                       "vod-baidu-task",
	} {
		binding, ok := generation.LookupDeclaredRoute(http.MethodPost, path)
		require.True(t, ok, path)
		assert.Equal(t, owner, binding.Plugin.Meta.Key, path)
	}
	// The provider plugin of phase one keeps no task part.
	provider, found := generation.Get("baidu-vod")
	require.True(t, found)
	assert.False(t, provider.Meta.HasTaskPart())

	// On /v1/videos every video model is offered to this plugin after the
	// vendor's own, and no speech, voice design or video effects model is,
	// since none of them is a video request. The plugin reads a shared model as
	// the vendor's plugin does, refusals in its decoder included, and keeps the
	// refusals of its own models for the request it builds; a body no decoder
	// accepts tells the two apart.
	for _, name := range plugin.Meta.Models {
		candidates := generation.LookupEndpointCandidates(http.MethodPost, "/v1/videos", name)
		if strings.HasPrefix(name, "speech-") || name == "kling-effects" || name == "minimax-voice-design" {
			assert.Empty(t, candidates, name)
			continue
		}
		require.NotEmpty(t, candidates, name)
		owner, _ := generation.GetByModel(name)
		assert.Same(t, owner, candidates[0].Plugin, name)
		assert.Same(t, plugin, candidates[len(candidates)-1].Plugin, name)
		_, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "decodeRequest"},
			map[string]any{"model": name, "body": map[string]any{"kind": "json", "value": []any{}}})
		if owner == plugin {
			assert.NoError(t, err, name)
		} else {
			assert.ErrorContains(t, err, "JSON object required", name)
		}
	}
}

// Fork: a request at a vendor's own path reaches Baidu's address for that
// vendor with the channel key as a Bearer token, the vendor's query reply
// drives the task, and the usage reserved at submission is settled on what the
// vendor reports or returned when the task fails.
func TestBaiduTaskPluginSubmitsPollsAndBills(t *testing.T) {
	const key = "sk-baidu-test"
	harness := newRelayHarness(t)
	engine := gin.New()
	SetRelayRouter(engine)
	SetTaskPluginProtocolRouter(engine)
	require.NoError(t, jsplugin.DefaultRegistry.SetGenerationPreparer(newPluginGenerationBuilder(engine.Routes(), nil, productionPluginRouteHandlers).prepare))
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.SetGenerationPreparer(nil)) })
	require.Empty(t, jsplugin.DefaultRegistry.RoutingErrors())
	require.Empty(t, jsplugin.DefaultRegistry.Generation().SkippedRoutes())
	engine.NoRoute((&pluginRouteDispatcher{registry: jsplugin.DefaultRegistry}).dispatch)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		harness.active.Add(1)
		defer harness.active.Add(-1)
		engine.ServeHTTP(w, r)
	}))
	t.Cleanup(gateway.Close)
	previousAdaptor := service.GetTaskAdaptorFunc
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor { return relay.GetTaskAdaptor(platform) }
	t.Cleanup(func() { service.GetTaskAdaptorFunc = previousAdaptor })

	cases := []struct {
		name, model string
		// expression prices the model, in US dollars.
		expression string
		// path and body are the client's request, in the vendor's format.
		path, body string
		// submitPath and queryPath are where Baidu serves the vendor.
		submitPath, queryPath string
		submitHeader          [2]string
		created               string
		running, done, failed string
		// reserved and settled are the dollars of the estimate and of the
		// usage the vendor reported.
		reserved, settled float64
		// statusPath is the client's status request, answered with wantStatus.
		statusPath, wantStatus string
	}{
		{
			name: "seedance", model: "doubao-seedance-2-0-260128", expression: `tier("base", u("tokens") * 10 / 1000000)`,
			path:       "/api/v3/contents/generations/tasks",
			body:       `{"model":"doubao-seedance-2-0-260128","content":[{"type":"text","text":"a cat"}],"duration":5,"resolution":"720p"}`,
			submitPath: "/async/seedance/api/v3/contents/generations/tasks", queryPath: "/async/seedance/api/v3/contents/generations/tasks/cgt-1",
			created: `{"id":"cgt-1"}`,
			running: `{"id":"cgt-1","status":"running"}`,
			done:    `{"id":"cgt-1","status":"succeeded","resolution":"720p","content":{"video_url":"https://cdn.example/seedance.mp4"},"usage":{"completion_tokens":100000,"total_tokens":100000}}`,
			failed:  `{"id":"cgt-1","status":"failed","error":{"message":"blocked"}}`,
			// 5 s x 1280 x 720 x 24 / 1024 = 108000 tokens; Ark reports 100000.
			reserved: 1.08, settled: 1,
			statusPath: "/api/v3/contents/generations/tasks/", wantStatus: `"status":"succeeded"`,
		},
		{
			name: "kling 4.0", model: "kling-4.0", expression: `tier("base", u("units") * 0.1)`,
			path: "/text-to-video/kling-4.0", body: `{"prompt":"a cat","settings":{"duration":5,"resolution":"720p"}}`,
			submitPath: "/async/kling/text-to-video/kling-4.0", queryPath: "/async/kling/tasks?task_ids=k4-1",
			created: `{"code":0,"message":"SUCCEED","data":{"id":"k4-1","status":"submitted"}}`,
			running: `{"code":0,"data":[{"id":"k4-1","status":"processing"}]}`,
			done:    `{"code":0,"data":[{"id":"k4-1","status":"succeeded","outputs":[{"type":"video","url":"https://cdn.example/kling.mp4","duration":"5.1"}],"billing":[{"charge_type":"unit","amount":"4"}]}]}`,
			failed:  `{"code":0,"data":[{"id":"k4-1","status":"failed","message":"blocked"}]}`,
			// 5 s at one unit per second; Kling deducted 4 units.
			reserved: 0.5, settled: 0.4,
			statusPath: "/tasks?task_ids=", wantStatus: `"status":"succeeded"`,
		},
		{
			name: "kling v3", model: "kling-v3", expression: `tier("base", u("units") * 0.1)`,
			path: "/v1/videos/text2video", body: `{"model_name":"kling-v3","prompt":"a cat","mode":"pro","duration":"5"}`,
			submitPath: "/v3/aigc/kl/videos/text2video", queryPath: "/v3/aigc/kl/videos/text2video/tsk-k3",
			submitHeader: [2]string{"X-Response-Format", "orion"},
			created:      `{"code":0,"message":"SUCCEED","data":{"task_id":"tsk-k3","task_status":"submitted"}}`,
			running:      `{"code":0,"data":{"task_id":"tsk-k3","task_status":"processing"}}`,
			done:         `{"code":0,"data":{"task_id":"tsk-k3","task_status":"succeed","task_result":{"videos":[{"url":"https://cdn.example/kling3.mp4","duration":"5"}]},"final_unit_deduction":"3.5"}}`,
			failed:       `{"code":0,"data":{"task_id":"tsk-k3","task_status":"failed","task_status_msg":"blocked"}}`,
			// 5 s in pro mode at 0.8 units per second; Kling deducted 3.5 units.
			reserved: 0.4, settled: 0.35,
			statusPath: "/v1/videos/text2video/", wantStatus: `"task_status":"succeed"`,
		},
		{
			name: "kling v2 master", model: "kling-v2-master",
			expression: `u("resolution") == "1080p" ? tier("pro", u("seconds") * 0.2) : tier("other", u("seconds") * 0.1)`,
			path:       "/v1/videos/image2video", body: `{"model_name":"kling-v2-master","image":"https://cdn.example/cat.png","mode":"pro","duration":"10"}`,
			submitPath: "/v3/aigc/kl/videos/image2video", queryPath: "/v3/aigc/kl/videos/image2video/tsk-k2",
			submitHeader: [2]string{"X-Response-Format", "orion"},
			created:      `{"code":0,"message":"SUCCEED","data":{"task_id":"tsk-k2","task_status":"submitted"}}`,
			running:      `{"code":0,"data":{"task_id":"tsk-k2","task_status":"processing"}}`,
			done:         `{"code":0,"data":{"task_id":"tsk-k2","task_status":"succeed","task_result":{"videos":[{"url":"https://cdn.example/kling2.mp4","duration":"10"}]},"final_unit_deduction":"20"}}`,
			failed:       `{"code":0,"data":{"task_id":"tsk-k2","task_status":"failed","task_status_msg":"blocked"}}`,
			// Billed per second of the request; the unit deduction is not read.
			reserved: 2, settled: 2,
			statusPath: "/v1/videos/image2video/", wantStatus: `"task_status":"succeed"`,
		},
		{
			name: "vidu", model: "viduq3-pro",
			expression: `u("resolution") == "1080p" ? tier("1080p", u("duration") * 0.1) : tier("other", u("duration") * 0.05)`,
			path:       "/ent/v2/text2video", body: `{"model":"viduq3-pro","prompt":"a cat","duration":5,"resolution":"1080p"}`,
			submitPath: "/v3/aigc/vd/ent/v2/text2video", queryPath: "/v3/aigc/vd/ent/v2/tasks/tsk-v1/creations",
			created: `{"task_id":"tsk-v1"}`,
			running: `{"state":"processing"}`,
			done:    `{"state":"success","credits":120,"creations":[{"url":"https://cdn.example/vidu.mp4"}]}`,
			failed:  `{"state":"failed","err_code":"blocked"}`,
			// Vidu reports its credits only for the finished task, so a price
			// cannot read them: the host evaluates it at submission too.
			reserved: 0.5, settled: 0.5,
			statusPath: "/ent/v2/tasks/%s/creations", wantStatus: `"state":"success"`,
		},
		{
			name: "pixverse", model: "pixversev5.6", expression: `tier("base", u("seconds") * 0.04)`,
			path: "/openapi/v2/video/text/generate", body: `{"model":"v5.6","prompt":"a cat","duration":5,"quality":"540p","aspect_ratio":"16:9"}`,
			submitPath: "/v3/aigc/pv/video/text/generate", queryPath: "/v3/tasks/tsk-p1",
			created:  `{"taskId":"tsk-p1","requestId":"req-1"}`,
			running:  `{"taskId":"tsk-p1","status":"RUNNING"}`,
			done:     `{"taskId":"tsk-p1","status":"SUCCESS","videoGenerateTaskInfo":{"videoGenerateTaskOutput":{"mediaBasicInfos":[{"mediaId":"mda-1","source":{"sourceUrl":"https://cdn.example/pixverse.mp4"}}]}}}`,
			failed:   `{"taskId":"tsk-p1","status":"FAILED","errorMessage":"blocked"}`,
			reserved: 0.2, settled: 0.2,
			statusPath: "/openapi/v2/video/result/", wantStatus: `"url":"https://cdn.example/pixverse.mp4"`,
		},
		{
			name: "happyhorse", model: "happyhorse-1.0-t2v",
			expression: `u("resolution") == "720P" ? tier("720P", u("seconds") * 0.1) : tier("1080P", u("seconds") * 0.2)`,
			path:       "/api/v1/services/aigc/video-generation/video-synthesis",
			body:       `{"model":"happyhorse-1.0-t2v","input":{"prompt":"a cat"},"parameters":{"resolution":"720P","ratio":"16:9"}}`,
			submitPath: "/v3/aigc/bailian/api/v1/services/aigc/video-generation/video-synthesis", queryPath: "/v3/aigc/bailian/api/v1/tasks/tsk-h1",
			submitHeader: [2]string{"X-DashScope-Async", "enable"},
			created:      `{"request_id":"req-1","output":{"task_id":"tsk-h1","task_status":"PENDING"}}`,
			running:      `{"output":{"task_id":"tsk-h1","task_status":"RUNNING"}}`,
			done:         `{"output":{"task_id":"tsk-h1","task_status":"SUCCEEDED","video_url":"https://cdn.example/horse.mp4"},"usage":{"duration":5,"SR":720}}`,
			failed:       `{"output":{"task_id":"tsk-h1","task_status":"FAILED","code":"DataInspectionFailed","message":"blocked"}}`,
			// Without a duration the longest, 15 s, is reserved; Bailian made 5 s.
			reserved: 1.5, settled: 0.5,
			statusPath: "/api/v1/tasks/", wantStatus: `"task_status":"SUCCEEDED"`,
		},
		{
			name: "minimax h3", model: "MiniMax-H3",
			expression: `tier("base", u("seconds") * 0.1 + u("input_images") * 0.02 + u("input_video_seconds") * 0.1)`,
			path:       "/v2/video_generation",
			body:       `{"model":"MiniMax-H3","content":[{"type":"text","text":"a cat"},{"type":"video_url","video_url":{"url":"https://cdn.example/in.mp4"},"role":"reference_video"}],"duration":5,"resolution":"768P"}`,
			submitPath: "/v3/aigc/minimax/v2/video_generation", queryPath: "/v3/aigc/minimax/v2/query/video_generation/tsk-m1",
			created: `{"task_id":"tsk-m1"}`,
			running: `{"task":{"id":"tsk-m1","status":"processing"}}`,
			done:    `{"task":{"id":"tsk-m1","model":"MiniMax-H3","status":"succeeded","content":{"url":"https://cdn.example/h3.mp4"},"resolution":"768P","duration":5,"usage":{"total_seconds":9,"input_seconds":4,"output_seconds":5,"input_image_count":0}}}`,
			failed:  `{"task":{"id":"tsk-m1","status":"failed","error":{"message":"blocked"}}}`,
			// The longest input video, 15 s, is reserved; MiniMax read 4 s.
			reserved: 2, settled: 0.9,
			statusPath: "/v2/query/video_generation/", wantStatus: `"status":"succeeded"`,
		},
		{
			name: "minimax hailuo v1", model: "MiniMax-Hailuo-2.3",
			expression: `u("resolution") == "1080P" ? tier("1080P", u("seconds") * 0.06) : tier("other", u("seconds") * 0.03)`,
			path:       "/v1/video_generation", body: `{"model":"MiniMax-Hailuo-2.3","prompt":"a cat","duration":6,"resolution":"1080P"}`,
			submitPath: "/v3/aigc/minimax/v1/video_generation", queryPath: "/v3/aigc/minimax/v1/query/video_generation?task_id=tsk-mv1",
			created: `{"task_id":"tsk-mv1","base_resp":{"status_code":0,"status_msg":"success"}}`,
			running: `{"task_id":"tsk-mv1","status":"Processing","base_resp":{"status_code":0,"status_msg":"success"}}`,
			done:    `{"task_id":"tsk-mv1","status":"Success","file":{"download_url":"https://cdn.example/hailuo.mp4"},"video_width":1366,"video_height":768,"base_resp":{"status_code":0,"status_msg":"success"}}`,
			failed:  `{"task_id":"tsk-mv1","status":"Fail","base_resp":{"status_code":1026,"status_msg":"blocked"}}`,
			// 6 s at 1080P are reserved; the 1366x768 video settles at 768P.
			reserved: 0.36, settled: 0.18,
			statusPath: "/v1/query/video_generation?task_id=", wantStatus: `"status":"Success"`,
		},
		{
			name: "kling effects", model: "kling-effects", expression: `tier("base", u("units") * 0.1)`,
			path: "/v1/videos/effects", body: `{"effect_scene":"color_mixing","input":{"image":"https://cdn.example/cat.png"}}`,
			submitPath: "/v3/aigc/kl/videos/effects", queryPath: "/v3/aigc/kl/videos/effects/tsk-e1",
			created: `{"code":0,"message":"SUCCEED","request_id":"req-1","data":{"task_id":"tsk-e1","task_status":"submitted","task_info":{"external_task_id":""}}}`,
			running: `{"code":0,"data":{"task_id":"tsk-e1","task_status":"processing"}}`,
			done:    `{"code":0,"data":{"task_id":"tsk-e1","task_status":"succeed","task_result":{"videos":[{"id":"9","url":"https://cdn.example/effect.mp4","duration":"10.666"}]},"final_unit_deduction":"3.5","final_balance_deduction":{"quota":"0","list_price":"0"}}}`,
			failed:  `{"code":0,"data":{"task_id":"tsk-e1","task_status":"failed","task_status_msg":"blocked"}}`,
			// Baidu's list deducts 4 for color_mixing; the task deducted 3.5.
			reserved: 0.4, settled: 0.35,
			statusPath: "/v1/videos/effects/", wantStatus: `"task_status":"succeed"`,
		},
		{
			name: "pixverse extend", model: "pixversev6.0",
			expression: `u("operation") == "extend" ? tier("extend", u("seconds") * 0.05) : tier("other", u("seconds") * 0.01)`,
			path:       "/openapi/v2/video/extend/generate",
			body:       `{"model":"v6","source_video_url":"https://cdn.example/in.mp4","prompt":"go on","duration":5,"quality":"720p","water_mark":false}`,
			submitPath: "/v3/aigc/pv/video/extend/generate", queryPath: "/v3/tasks/tsk-p2",
			created:  `{"taskId":"tsk-p2","requestId":"req-1"}`,
			running:  `{"taskId":"tsk-p2","status":"RUNNING"}`,
			done:     `{"taskId":"tsk-p2","status":"SUCCESS","videoGenerateTaskInfo":{"videoGenerateTaskOutput":{"mediaBasicInfos":[{"mediaId":"mda-2","source":{"sourceUrl":"https://cdn.example/extended.mp4"}}]}}}`,
			failed:   `{"taskId":"tsk-p2","status":"FAILED","errorMessage":"blocked"}`,
			reserved: 0.25, settled: 0.25,
			statusPath: "/openapi/v2/video/result/", wantStatus: `"url":"https://cdn.example/extended.mp4"`,
		},
		{
			name: "pixverse fusion", model: "pixverse5.5",
			expression: `u("operation") == "fusion" ? tier("fusion", u("seconds") * 0.06) : tier("other", u("seconds") * 0.01)`,
			path:       "/openapi/v2/video/fusion/generate",
			body:       `{"model":"v5.5","prompt":"@cat plays at @ground","duration":5,"quality":"540p","image_references":[{"type":"subject","image_url":"https://cdn.example/cat.png","ref_name":"cat"},{"type":"background","image_url":"https://cdn.example/ground.png","ref_name":"ground"}]}`,
			submitPath: "/v3/aigc/pv/video/fusion/generate", queryPath: "/v3/tasks/tsk-p3",
			created:  `{"taskId":"tsk-p3","requestId":"req-1"}`,
			running:  `{"taskId":"tsk-p3","status":"RUNNING"}`,
			done:     `{"taskId":"tsk-p3","status":"SUCCESS","videoGenerateTaskInfo":{"videoGenerateTaskOutput":{"mediaBasicInfos":[{"mediaId":"mda-3","source":{"sourceUrl":"https://cdn.example/fusion.mp4"}}]}}}`,
			failed:   `{"taskId":"tsk-p3","status":"FAILED","errorMessage":"blocked"}`,
			reserved: 0.3, settled: 0.3,
			statusPath: "/openapi/v2/video/result/", wantStatus: `"url":"https://cdn.example/fusion.mp4"`,
		},
	}

	const speechModel, voiceDesignModel = "speech-2.8-hd", "minimax-voice-design"
	modes := map[string]string{speechModel: "tiered_expr", voiceDesignModel: "tiered_expr"}
	expressions := map[string]string{
		speechModel:      `tier("base", u("characters") * 500 / 1000000)`,
		voiceDesignModel: `tier("base", u("voices") * 1 + u("characters") * 200 / 1000000)`,
	}
	models := []string{speechModel, voiceDesignModel}
	for _, tc := range cases {
		modes[tc.model], expressions[tc.model] = "tiered_expr", tc.expression
		models = append(models, tc.model)
	}
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(option, value string) error {
		if option == "billing_setting.billing_mode" || option == "billing_setting.billing_expr" {
			saved[option] = value
		}
		return nil
	}))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	modeJSON, err := common.Marshal(modes)
	require.NoError(t, err)
	expressionJSON, err := common.Marshal(expressions)
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": string(modeJSON), "billing_setting.billing_expr": string(expressionJSON)}))

	install := func(t *testing.T, channelKey string) *model.Channel {
		channel := harness.pluginChannel("vod-baidu-task", channelKey)
		channel.Models = strings.Join(models, ",")
		// Both of Baidu's systems are the fake upstream.
		channel.SetOtherSettings(dto.ChannelOtherSettings{PluginConfig: []byte(`{"asyncBaseUrl":"` + harness.upstream.URL + `"}`)})
		return harness.install(t, channel)
	}
	call := func(t *testing.T, method, path, body string) (int, string) {
		request, err := http.NewRequest(method, gateway.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer "+relayTestToken)
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		defer response.Body.Close()
		received, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		harness.waitIdle(t)
		return response.StatusCode, string(received)
	}
	spent := func(t *testing.T) int {
		var user model.User
		require.NoError(t, model.DB.First(&user, harness.userID).Error)
		return 100_000_000 - user.Quota
	}
	quota := func(dollars float64) int { return int(dollars*common.QuotaPerUnit + 0.5) }
	lastTask := func(t *testing.T) model.Task {
		var task model.Task
		require.NoError(t, model.DB.Order("id DESC").First(&task).Error)
		return task
	}
	poll := func(t *testing.T, channel *model.Channel) model.Task {
		task := lastTask(t)
		upstreamID := task.GetUpstreamTaskID()
		service.DispatchPlatformUpdate(context.Background(), task.Platform, map[int][]string{channel.Id: {upstreamID}}, map[string]*model.Task{upstreamID: &task})
		return lastTask(t)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channel := install(t, key)
			var reply atomic.Pointer[string]
			harness.upstream.respond = func(w http.ResponseWriter, _ int, request capturedRequest) {
				w.Header().Set("Content-Type", "application/json")
				if request.Method == http.MethodPost {
					_, _ = io.WriteString(w, tc.created)
					return
				}
				_, _ = io.WriteString(w, *reply.Load())
			}
			harness.upstream.reset()
			before := spent(t)

			status, body := call(t, http.MethodPost, tc.path, tc.body)

			require.Equal(t, http.StatusOK, status, body)
			requests := harness.upstream.captured()
			require.Len(t, requests, 1)
			assert.Equal(t, tc.submitPath, requests[0].URL)
			assert.Equal(t, "Bearer "+key, requests[0].Header.Get("Authorization"))
			if tc.submitHeader[0] != "" {
				assert.Equal(t, tc.submitHeader[1], requests[0].Header.Get(tc.submitHeader[0]))
			}
			assert.JSONEq(t, gjson.Get(tc.body, "@this").Raw, requests[0].Body, "the vendor's body goes as the client sent it")
			task := lastTask(t)
			assert.Equal(t, constant.TaskPlatform("vod-baidu-task"), task.Platform)
			assert.Contains(t, body, task.TaskID)
			assert.NotContains(t, body, task.GetUpstreamTaskID(), "the client gets the gateway's task id")
			assert.Equal(t, quota(tc.reserved), spent(t)-before, "the estimate is reserved")

			reply.Store(&tc.running)
			task = poll(t, channel)
			requests = harness.upstream.captured()
			require.Len(t, requests, 2)
			assert.Equal(t, http.MethodGet, requests[1].Method)
			assert.Equal(t, tc.queryPath, requests[1].URL)
			assert.Equal(t, "Bearer "+key, requests[1].Header.Get("Authorization"))
			assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.Status)
			assert.Equal(t, quota(tc.reserved), spent(t)-before)

			reply.Store(&tc.done)
			task = poll(t, channel)
			assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
			assert.Equal(t, quota(tc.settled), task.Quota)
			assert.Equal(t, quota(tc.settled), spent(t)-before, "the reported usage is settled")

			statusPath := tc.statusPath + task.TaskID
			if strings.Contains(tc.statusPath, "%s") {
				statusPath = strings.Replace(tc.statusPath, "%s", task.TaskID, 1)
			}
			status, body = call(t, http.MethodGet, statusPath, "")
			require.Equal(t, http.StatusOK, status, body)
			assert.Contains(t, body, tc.wantStatus)
			assert.Contains(t, body, task.TaskID)

			// A task the vendor fails costs nothing.
			before = spent(t)
			status, body = call(t, http.MethodPost, tc.path, tc.body)
			require.Equal(t, http.StatusOK, status, body)
			assert.Equal(t, quota(tc.reserved), spent(t)-before)
			reply.Store(&tc.failed)
			task = poll(t, channel)
			assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
			assert.Contains(t, task.FailReason, "blocked")
			assert.Zero(t, spent(t)-before, "the reservation is returned")
		})
	}

	// MiniMax's speech synthesis answers with the audio. Baidu serves it with
	// a request signature only, so the key carries an AccessKey: 你好 counts
	// four characters and ", world" seven.
	t.Run("speech", func(t *testing.T) {
		const speech = `{"model":"speech-2.8-hd","text":"你好, world","voice_setting":{"voice_id":"voice-1"}}`
		install(t, key+"|ak-test|sk-test")
		harness.upstream.respond = func(w http.ResponseWriter, _ int, _ capturedRequest) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"audio":"494433","status":2},"extra_info":{"audio_length":900,"usage_characters":10},"base_resp":{"status_code":0,"status_msg":"OK"}}`)
		}
		harness.upstream.reset()
		before := spent(t)

		status, body := call(t, http.MethodPost, "/v1/t2a_v2", speech)

		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "494433", gjson.Get(body, "data.audio").String())
		requests := harness.upstream.captured()
		require.Len(t, requests, 1)
		assert.Equal(t, "/v2/mini/t2a_v2", requests[0].URL)
		// bce-auth-v1/{AccessKey}/{time}/{seconds}/{signed headers}/{signature}
		authorization := strings.Split(requests[0].Header.Get("Authorization"), "/")
		require.Len(t, authorization, 6, requests[0].Header.Get("Authorization"))
		prefix := strings.Join(authorization[:4], "/")
		assert.Equal(t, "bce-auth-v1/ak-test/"+requests[0].Header.Get("X-Bce-Date")+"/1800", prefix)
		assert.Equal(t, "host;x-bce-date", authorization[4])
		sign := func(secret, message string) string {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write([]byte(message))
			return hex.EncodeToString(mac.Sum(nil))
		}
		upstreamURL, err := url.Parse(harness.upstream.URL)
		require.NoError(t, err)
		canonical := "POST\n/v2/mini/t2a_v2\n\nhost:" + url.QueryEscape(upstreamURL.Host) + "\nx-bce-date:" + url.QueryEscape(requests[0].Header.Get("X-Bce-Date"))
		assert.Equal(t, sign(sign("sk-test", prefix), canonical), authorization[5])
		for _, secret := range []string{key, "sk-test"} {
			assert.NotContains(t, requests[0].Header.Get("Authorization"), secret)
			assert.NotContains(t, requests[0].Body, secret)
		}
		task := lastTask(t)
		assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
		// Eleven characters are reserved and the ten MiniMax billed are settled.
		assert.Equal(t, quota(10*500.0/1_000_000), spent(t)-before)

		// Without an AccessKey nothing is sent and nothing is charged.
		install(t, key)
		harness.upstream.reset()
		before = spent(t)
		status, body = call(t, http.MethodPost, "/v1/t2a_v2", speech)
		// MiniMax answers an error with HTTP 200 and its code in base_resp.
		assert.Equal(t, http.StatusOK, status)
		assert.NotZero(t, gjson.Get(body, "base_resp.status_code").Int(), body)
		assert.Contains(t, body, "API Key|AccessKey|SecretKey")
		assert.Empty(t, harness.upstream.captured())
		assert.Zero(t, spent(t)-before)
	})

	// MiniMax's asynchronous synthesis is a task Baidu answers in its own task
	// structure, signed like the synthesis. The text is counted as for the
	// synthesis, 11 characters; Baidu's estimate, 13, is above that and within
	// what the text could count, and settles the task.
	t.Run("asynchronous speech", func(t *testing.T) {
		const speech = `{"model":"speech-2.8-hd","text":"你好, world","voice_setting":{"voice_id":"voice-1"},"audio_setting":{"format":"mp3"}}`
		channel := install(t, key+"|ak-test|sk-test")
		var reply atomic.Pointer[string]
		harness.upstream.respond = func(w http.ResponseWriter, _ int, request capturedRequest) {
			w.Header().Set("Content-Type", "application/json")
			if request.Method == http.MethodPost {
				_, _ = io.WriteString(w, `{"task_id":"tsk-async-1","usage_characters":13,"base_resp":{"status_code":0,"status_msg":"OK"}}`)
				return
			}
			_, _ = io.WriteString(w, *reply.Load())
		}
		harness.upstream.reset()
		signed := func(t *testing.T, request capturedRequest) {
			assert.True(t, strings.HasPrefix(request.Header.Get("Authorization"), "bce-auth-v1/ak-test/"), request.Header.Get("Authorization"))
			assert.NotEmpty(t, request.Header.Get("X-Bce-Date"))
		}
		before := spent(t)

		status, body := call(t, http.MethodPost, "/v1/t2a_async_v2", speech)

		require.Equal(t, http.StatusOK, status, body)
		requests := harness.upstream.captured()
		require.Len(t, requests, 1)
		assert.Equal(t, "/v2/mini/t2a_async_v2", requests[0].URL)
		signed(t, requests[0])
		assert.JSONEq(t, speech, requests[0].Body)
		task := lastTask(t)
		assert.Equal(t, task.TaskID, gjson.Get(body, "task_id").String())
		assert.NotContains(t, body, "tsk-async-1")
		assert.Equal(t, quota(11*500.0/1_000_000), spent(t)-before, "the counted text is reserved")

		running := `{"taskId":"tsk-async-1","type":"TTS","status":"RUNNING","createTime":"2024-11-15T10:00:00Z","ttsTaskInfo":{}}`
		reply.Store(&running)
		task = poll(t, channel)
		requests = harness.upstream.captured()
		require.Len(t, requests, 2)
		assert.Equal(t, http.MethodGet, requests[1].Method)
		assert.Equal(t, "/v2/task/tsk-async-1", requests[1].URL)
		signed(t, requests[1])
		assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.Status)

		done := `{"taskId":"tsk-async-1","type":"TTS","status":"SUCCESS","createTime":"2024-11-15T10:00:00Z","finishTime":"2024-11-15T10:01:30Z","ttsTaskInfo":{"url":"https://cdn.example/tmp/tsk-async-1.mp3?sign=1","code":"0"}}`
		reply.Store(&done)
		task = poll(t, channel)
		assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
		assert.Equal(t, quota(13*500.0/1_000_000), spent(t)-before, "Baidu's estimate settles")

		// MiniMax's query names the audio by a file id, which the gateway's
		// file retrieval answers with its own address of the audio.
		status, body = call(t, http.MethodGet, "/v1/query/t2a_async_query_v2?task_id="+task.TaskID, "")
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "Success", gjson.Get(body, "status").String(), body)
		assert.Equal(t, task.TaskID, gjson.Get(body, "file_id").String(), body)
		status, body = call(t, http.MethodGet, "/v1/files/retrieve?file_id="+task.TaskID, "")
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "t2a_async_output", gjson.Get(body, "file.purpose").String(), body)
		assert.Equal(t, task.TaskID+".mp3", gjson.Get(body, "file.filename").String(), body)
		assert.NotEmpty(t, gjson.Get(body, "file.download_url").String(), body)
		assert.NotContains(t, body, "cdn.example", "the audio is served at the gateway's address")

		// A synthesis Baidu fails costs nothing.
		before = spent(t)
		status, body = call(t, http.MethodPost, "/v1/t2a_async_v2", speech)
		require.Equal(t, http.StatusOK, status, body)
		failed := `{"taskId":"tsk-async-1","type":"TTS","status":"FAILED","ttsTaskInfo":{"code":"1013","errMsg":"Audio synthesis failed due to invalid input"}}`
		reply.Store(&failed)
		task = poll(t, channel)
		assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
		assert.Contains(t, task.FailReason, "invalid input")
		assert.Zero(t, spent(t)-before, "the reservation is returned")
		status, body = call(t, http.MethodGet, "/v1/query/t2a_async_query_v2?task_id="+task.TaskID, "")
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "Failed", gjson.Get(body, "status").String(), body)

		// A text in a file cannot be counted, so it is not sent.
		harness.upstream.reset()
		before = spent(t)
		status, body = call(t, http.MethodPost, "/v1/t2a_async_v2", `{"model":"speech-2.8-hd","text_file_id":"file-1","voice_setting":{"voice_id":"voice-1"}}`)
		assert.Equal(t, http.StatusOK, status)
		assert.NotZero(t, gjson.Get(body, "base_resp.status_code").Int(), body)
		assert.Contains(t, body, "text_file_id is not supported")
		assert.Empty(t, harness.upstream.captured())
		assert.Zero(t, spent(t)-before)
	})

	// Baidu charges for a designed voice when it is first used, which the
	// gateway cannot see, so the voice is charged when it is made, with the
	// characters of its preview: 1 voice and 11 characters.
	t.Run("voice design", func(t *testing.T) {
		const design = `{"prompt":"温柔的女声","preview_text":"你好, world","voice_id":"myVoice01"}`
		install(t, key+"|ak-test|sk-test")
		designed := `{"voice_id":"myVoice01","trial_audio":"fffb9064","trial_audio_url":"https://cdn.example/design.mp3","base_resp":{"status_code":0,"status_msg":"OK"}}`
		var answer atomic.Pointer[string]
		answer.Store(&designed)
		harness.upstream.respond = func(w http.ResponseWriter, _ int, _ capturedRequest) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, *answer.Load())
		}
		harness.upstream.reset()
		before := spent(t)

		status, body := call(t, http.MethodPost, "/v1/voice_design", design)

		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "myVoice01", gjson.Get(body, "voice_id").String(), body)
		requests := harness.upstream.captured()
		require.Len(t, requests, 1)
		assert.Equal(t, "/v2/mini/voice_design", requests[0].URL)
		assert.True(t, strings.HasPrefix(requests[0].Header.Get("Authorization"), "bce-auth-v1/ak-test/"), requests[0].Header.Get("Authorization"))
		assert.JSONEq(t, design, requests[0].Body, "a voice design names no model")
		assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), lastTask(t).Status)
		assert.Equal(t, quota(1+11*200.0/1_000_000), spent(t)-before)

		// A voice Baidu does not make costs nothing.
		refused := `{"base_resp":{"status_code":2013,"status_msg":"invalid prompt"}}`
		answer.Store(&refused)
		before = spent(t)
		status, body = call(t, http.MethodPost, "/v1/voice_design", design)
		assert.Equal(t, http.StatusOK, status)
		assert.NotZero(t, gjson.Get(body, "base_resp.status_code").Int(), body)
		assert.Zero(t, spent(t)-before)

		// A preview past Baidu's longest is not sent.
		harness.upstream.reset()
		status, body = call(t, http.MethodPost, "/v1/voice_design", `{"prompt":"a voice","preview_text":"`+strings.Repeat("a", 501)+`"}`)
		assert.Equal(t, http.StatusOK, status)
		assert.Contains(t, body, "preview_text must be at most 500 characters")
		assert.Empty(t, harness.upstream.captured())
		assert.Zero(t, spent(t)-before)
	})
}

// Fork: every request quantity the plugin turns into usage is bounded before
// it is reserved, a quantity the request leaves to the vendor reserves the
// most the model can make, and a quantity the vendor reports settles the task
// only within what the request could cost.
func TestBaiduTaskPluginBoundsWhatIsBilled(t *testing.T) {
	plugin, found := jsplugin.DefaultRegistry.Get("vod-baidu-task")
	require.True(t, found, "the built-in plugin is not served")
	object := func(t *testing.T, value any) map[string]any {
		encoded, err := common.Marshal(value)
		require.NoError(t, err)
		var decoded map[string]any
		require.NoError(t, common.Unmarshal(encoded, &decoded))
		return decoded
	}
	reserve := func(t *testing.T, decode, path, body string) (map[string]any, error) {
		var value any
		require.NoError(t, common.Unmarshal([]byte(body), &value))
		intent, err := plugin.Engine.CallPath(t.Context(), "native", []string{decode}, map[string]any{"path": path, "body": map[string]any{"kind": "json", "value": value}})
		if err != nil {
			return nil, err
		}
		decoded := object(t, intent)
		usage, err := plugin.Engine.Call(t.Context(), "extractUsage", map[string]any{"requestBody": decoded["requestBody"], "model": decoded["model"], "upstreamModel": decoded["model"]})
		if err != nil {
			return nil, err
		}
		return object(t, usage), nil
	}
	const (
		seedance, klingVideo, vidu = "/api/v3/contents/generations/tasks", "/v1/videos/", "/ent/v2/"
		pixverse, happyHorse       = "/openapi/v2/video/img/generate", "/api/v1/services/aigc/video-generation/video-synthesis"
	)

	for name, tc := range map[string]struct {
		decode, path, body string
		want               map[string]any
		wantErr            string
	}{
		"seedance picks the length": {
			decode: "createSeedanceTask", path: seedance,
			body: `{"model":"doubao-seedance-2-0-fast-260128","duration":-1,"content":[{"type":"text","text":"a cat"},{"type":"video_url","video_url":{"url":"https://cdn.example/in.mp4"}}]}`,
			// The longest, 15 s, at the model's highest resolution, 720p.
			want: map[string]any{"tokens": float64(15 * 1280 * 720 * 24 / 1024), "resolution": "720p", "video_input": "video"},
		},
		"seedance resolution the model lacks": {
			decode: "createSeedanceTask", path: seedance,
			body:    `{"model":"doubao-seedance-2-0-fast-260128","resolution":"1080p","content":[{"type":"text","text":"a cat"}]}`,
			wantErr: "resolution must be one of 480p, 720p",
		},
		"kling 4.0 flash follows an input video": {
			decode: "kling_omni_video_kling_4_0_flash", path: "/omni-video/kling-4.0-flash",
			body: `{"contents":[{"type":"prompt","text":"a cat"},{"type":"base_video","url":"https://cdn.example/in.mp4"}]}`,
			// The longest output twice, or the longest input and output.
			want: map[string]any{"seconds": float64(60), "resolution": "1080p", "audio": "on", "video_input": "video"},
		},
		"kling 3.0 turbo as asked": {
			decode: "kling_text_to_video_kling_3_0_turbo", path: "/text-to-video/kling-3.0-turbo",
			body: `{"prompt":"a cat","settings":{"duration":5,"resolution":"720p","audio":"off"}}`,
			want: map[string]any{"seconds": float64(5), "resolution": "720p", "audio": "off", "video_input": "none"},
		},
		"kling 3.0 turbo past its longest": {
			decode: "kling_text_to_video_kling_3_0_turbo", path: "/text-to-video/kling-3.0-turbo",
			body: `{"prompt":"a cat","settings":{"duration":16}}`, wantErr: "duration must be an integer from 3 to 15",
		},
		"kling 4.0 units with an input video": {
			decode: "kling_omni_video_kling_4_0", path: "/omni-video/kling-4.0",
			body: `{"contents":[{"type":"feature_video","url":"https://cdn.example/in.mp4"}],"settings":{"duration":5,"resolution":"1080p"}}`,
			// 0.9 units a second of the longest input, 30 s, and the output.
			want: map[string]any{"units": 31.5},
		},
		"kling v3 omni by default": {
			decode: "createKlingVideo", path: klingVideo + "omni-video",
			body: `{"model_name":"kling-v3-omni","prompt":"a cat","sound":"on","video_list":[{"video_url":"https://cdn.example/in.mp4"}]}`,
			want: map[string]any{"seconds": float64(5), "resolution": "720p", "audio": "on", "video_input": "video"},
		},
		"kling mode outside the list": {
			decode: "createKlingVideo", path: klingVideo + "text2video",
			body: `{"model_name":"kling-v1-5","prompt":"a cat","mode":"ultra","duration":"10"}`,
			want: map[string]any{"seconds": float64(10), "resolution": "4k", "audio": "off", "video_input": "none"},
		},
		"kling v2.6 units with a voice": {
			decode: "createKlingVideo", path: klingVideo + "text2video",
			body: `{"model_name":"kling-v2-6","prompt":"a cat","mode":"pro","sound":"on","voice_list":[{"voice_id":"v1"}],"duration":"10"}`,
			want: map[string]any{"units": float64(12)},
		},
		"kling duration of zero": {
			decode: "createKlingVideo", path: klingVideo + "text2video",
			body: `{"model_name":"kling-v3","prompt":"a cat","duration":"0"}`, wantErr: "duration must be an integer from 1 to 3600",
		},
		"kling 4.0 on the model_name path": {
			decode: "createKlingVideo", path: klingVideo + "text2video",
			body: `{"model_name":"kling-4.0","prompt":"a cat"}`, wantErr: "kling-4.0 is served at /text-to-video/kling-4.0",
		},
		"vidu defaults of the endpoint": {
			decode: "createViduTask", path: vidu + "reference2video",
			body: `{"model":"viduq3-drama","prompt":"a scene","images":["https://cdn.example/a.png"]}`,
			want: map[string]any{"duration": float64(8), "resolution": "1080p"},
		},
		"vidu q4 in 4K": {
			decode: "createViduTask", path: vidu + "img2video",
			body: `{"model":"viduq4","images":["https://cdn.example/a.png"],"resolution":"4K","duration":16}`,
			want: map[string]any{"duration": float64(16), "resolution": "4K"},
		},
		"vidu duration it cannot read": {
			decode: "createViduTask", path: vidu + "text2video",
			body: `{"model":"viduq3-pro","prompt":"a cat","duration":"long"}`,
			want: map[string]any{"duration": float64(16), "resolution": "1080p"},
		},
		"pixverse conditions": {
			decode: "createPixverseVideo", path: pixverse,
			body: `{"model":"pixverse5.5","prompt":"a cat","duration":"8","quality":"1080P","motion_mode":"fast","generate_audio_switch":true,"generate_multi_clip_switch":true,"image_list":[{"image_url":"https://cdn.example/a.png"}]}`,
			want: map[string]any{"seconds": float64(8), "resolution": "1080p", "motion_mode": "fast", "audio": "on", "multi_clip": true, "operation": "generate"},
		},
		"pixverse extend by the requested duration": {
			decode: "createPixverseVideo", path: "/openapi/v2/video/extend/generate",
			body: `{"model":"v6","source_video_url":"https://cdn.example/in.mp4","prompt":"go on","duration":12,"quality":"1080p","water_mark":false}`,
			want: map[string]any{"seconds": float64(12), "resolution": "1080p", "motion_mode": "normal", "audio": "off", "multi_clip": false, "operation": "extend"},
		},
		"pixverse extend of a media id": {
			decode: "createPixverseVideo", path: "/openapi/v2/video/extend/generate",
			body: `{"model":"v6","video_media_id":"mda-other","prompt":"go on","duration":5,"quality":"720p"}`, wantErr: "video_media_id is not supported; send source_video_url",
		},
		"pixverse fusion model outside its list": {
			decode: "createPixverseVideo", path: "/openapi/v2/video/fusion/generate",
			body: `{"model":"v6","prompt":"@cat runs","duration":5,"quality":"720p"}`, wantErr: "model must be one of v4.5, v5, v5.5, v5.6 on this endpoint",
		},
		"kling effect from Baidu's list": {
			decode: "createKlingEffect", path: "/v1/videos/effects",
			body: `{"effect_scene":"bicycle_kick","input":{"image":"https://cdn.example/a.png"}}`, want: map[string]any{"units": float64(12)},
		},
		"kling effect the list writes with a space": {
			decode: "createKlingEffect", path: "/v1/videos/effects",
			body: `{"effect_scene":"twist_ shake","input":{"image":"https://cdn.example/a.png"}}`, want: map[string]any{"units": 2.5},
		},
		"kling effect the list lacks": {
			decode: "createKlingEffect", path: "/v1/videos/effects",
			body: `{"effect_scene":"brand_new_effect","input":{"image":"https://cdn.example/a.png"}}`,
			// The dearest of the list.
			want: map[string]any{"units": float64(12)},
		},
		"kling effect without a scene": {
			decode: "createKlingEffect", path: "/v1/videos/effects",
			body: `{"input":{"image":"https://cdn.example/a.png"}}`, wantErr: "effect_scene is required",
		},
		"hailuo v1 by default": {
			decode: "createHailuoTask", path: "/v1/video_generation",
			body: `{"model":"MiniMax-Hailuo-02","prompt":"a cat","first_frame_image":"https://cdn.example/a.png"}`,
			want: map[string]any{"seconds": float64(6), "resolution": "768P", "input_images": float64(0), "input_video_seconds": float64(0)},
		},
		"hailuo v1 resolution it cannot read": {
			decode: "createHailuoTask", path: "/v1/video_generation",
			body: `{"model":"MiniMax-Hailuo-2.3","prompt":"a cat","duration":6,"resolution":"4K"}`,
			want: map[string]any{"seconds": float64(6), "resolution": "1080P", "input_images": float64(0), "input_video_seconds": float64(0)},
		},
		"hailuo v1 duration the model lacks": {
			decode: "createHailuoTask", path: "/v1/video_generation",
			body: `{"model":"MiniMax-Hailuo-2.3","prompt":"a cat","duration":7}`, wantErr: "MiniMax-Hailuo-2.3 duration must be 6 or 10",
		},
		"pixverse duration past the longest": {
			decode: "createPixverseVideo", path: pixverse,
			body: `{"model":"v6","prompt":"a cat","duration":16,"quality":"720p"}`, wantErr: "duration must be an integer from 1 to 15",
		},
		"pixverse without a quality": {
			decode: "createPixverseVideo", path: pixverse,
			body: `{"model":"v6","prompt":"a cat","duration":5}`, wantErr: "quality must be one of",
		},
		"pixverse model outside the list": {
			decode: "createPixverseVideo", path: pixverse,
			body: `{"model":"c1","prompt":"a cat","duration":5,"quality":"720p"}`, wantErr: "model must be one of",
		},
		"happyhorse leaves the length out": {
			decode: "createHappyHorseTask", path: happyHorse,
			body: `{"model":"happyhorse-1.0-video-edit","input":{"prompt":"brighter"}}`,
			want: map[string]any{"seconds": float64(15), "resolution": "1080P"},
		},
		"happyhorse duration past the bound": {
			decode: "createHappyHorseTask", path: happyHorse,
			body: `{"model":"happyhorse-1.1-t2v","input":{"prompt":"a cat"},"parameters":{"duration":99999}}`, wantErr: "parameters.duration must be an integer from 1 to 3600",
		},
		"h3 leaves the length out": {
			decode: "createH3Task", path: "/v2/video_generation",
			body: `{"model":"MiniMax-H3","content":[{"type":"text","text":"a cat"},{"type":"image_url","image_url":{"url":"https://cdn.example/a.png"},"role":"first_frame"}]}`,
			want: map[string]any{"seconds": float64(15), "resolution": "2K", "input_images": float64(1), "input_video_seconds": float64(0)},
		},
		"h3 max": {
			decode: "createH3Task", path: "/v2/video_generation",
			body: `{"model":"MiniMax-H3-MAX","content":[{"type":"text","text":"a cat"}],"duration":6,"resolution":"480P"}`,
			want: map[string]any{"seconds": float64(6), "resolution": "480P"},
		},
		"h3 duration past the longest": {
			decode: "createH3Task", path: "/v2/video_generation",
			body: `{"model":"MiniMax-H3","content":[{"type":"text","text":"a cat"}],"duration":16}`, wantErr: "duration must be an integer between 4 and 15",
		},
		"speech at the longest text": {
			decode: "createSpeech", path: "/v1/t2a_v2",
			body: `{"model":"speech-02-turbo","text":"` + strings.Repeat("好", 4000) + strings.Repeat("a", 2000) + `"}`,
			want: map[string]any{"characters": float64(10000)},
		},
		"speech past the longest text": {
			decode: "createSpeech", path: "/v1/t2a_v2",
			body: `{"model":"speech-02-turbo","text":"` + strings.Repeat("好", 5000) + `a"}`, wantErr: "text must be at most 10000 characters",
		},
		"asynchronous speech at the longest text": {
			decode: "createAsyncSpeech", path: "/v1/t2a_async_v2",
			body: `{"model":"speech-2.8-turbo","text":"` + strings.Repeat("好", 25000) + `"}`, want: map[string]any{"characters": float64(50000)},
		},
		"asynchronous speech past the longest text": {
			decode: "createAsyncSpeech", path: "/v1/t2a_async_v2",
			body: `{"model":"speech-2.8-turbo","text":"` + strings.Repeat("好", 25000) + `a"}`, wantErr: "text must be at most 50000 characters",
		},
		"voice design with its preview": {
			decode: "createVoiceDesign", path: "/v1/voice_design",
			body: `{"prompt":"a calm voice","preview_text":"你好, world"}`, want: map[string]any{"voices": float64(1), "characters": float64(11)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			facts, err := reserve(t, tc.decode, tc.path, tc.body)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, facts)
		})
	}

	klingState := func(seconds int, videoInput string, total bool) map[string]any {
		return map[string]any{"vendor": "kling", "api": "path", "open": true, "requested": 0, "total": total,
			"facts": map[string]any{"seconds": seconds, "resolution": "1080p", "audio": "on", "video_input": videoInput}}
	}
	klingReply := func(output, input string) string {
		return `{"code":0,"data":[{"id":"k-1","status":"succeeded","outputs":[{"type":"video","url":"https://cdn.example/k.mp4","duration":"` + output + `"}],"usage":{"input_duration":"` + input + `"}}]}`
	}
	for name, tc := range map[string]struct {
		model string
		state map[string]any
		reply string
		want  map[string]any
	}{
		"kling settles the length it picked": {
			model: "kling-3.0-turbo", state: klingState(15, "none", false), reply: klingReply("7.4", ""),
			want: map[string]any{"seconds": float64(8), "resolution": "1080p", "audio": "on", "video_input": "none"},
		},
		"kling bills a longer input with the output": {
			model: "kling-4.0-flash", state: klingState(60, "video", true), reply: klingReply("5.2", "8"),
			want: map[string]any{"seconds": float64(14), "resolution": "1080p", "audio": "on", "video_input": "video"},
		},
		"kling cannot report more than was reserved": {
			model: "kling-3.0-turbo", state: klingState(15, "none", false), reply: klingReply("99999", ""),
			want: map[string]any{"seconds": float64(15), "resolution": "1080p", "audio": "on", "video_input": "none"},
		},
		"happyhorse settles the reported length and resolution": {
			model: "happyhorse-1.0-video-edit", state: map[string]any{"vendor": "happyhorse"},
			reply: `{"output":{"task_status":"SUCCEEDED"},"usage":{"output_video_duration":6,"SR":"1080"}}`,
			want:  map[string]any{"seconds": float64(6), "resolution": "1080P"},
		},
		"h3 usage outside the model's range is not believed": {
			model: "MiniMax-H3", state: map[string]any{"vendor": "minimax"},
			reply: `{"task":{"status":"succeeded","resolution":"8K","usage":{"output_seconds":900,"input_seconds":4,"input_image_count":-1}}}`,
			want:  map[string]any{"input_video_seconds": float64(4)},
		},
		"kling effect cannot settle above the list": {
			model: "kling-effects", state: map[string]any{"vendor": "kling", "api": "effects", "endpoint": "effects", "reserved": 4},
			reply: `{"code":0,"data":{"task_status":"succeed","final_unit_deduction":"99"}}`,
			want:  map[string]any{"units": float64(4)},
		},
		"kling effect without a deduction keeps the reservation": {
			model: "kling-effects", state: map[string]any{"vendor": "kling", "api": "effects", "endpoint": "effects", "reserved": 4},
			reply: `{"code":0,"data":{"task_status":"succeed","final_unit_deduction":"0"}}`,
			want:  nil,
		},
		"hailuo v1 settles the resolution of the video": {
			model: "MiniMax-Hailuo-2.3", state: map[string]any{"vendor": "minimax", "api": "v1"},
			reply: `{"status":"Success","video_width":1920,"video_height":1080}`,
			want:  map[string]any{"resolution": "1080P"},
		},
		"seedance settles the tokens Ark reports": {
			model: "doubao-seedance-2-5-260628", state: map[string]any{"vendor": "seedance"},
			reply: `{"status":"succeeded","usage":{"completion_tokens":0,"total_tokens":246840},"content":{"resolution":"4k"}}`,
			want:  map[string]any{"tokens": float64(246840)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var reply any
			require.NoError(t, common.Unmarshal([]byte(tc.reply), &reply))
			facts, err := plugin.Engine.Call(t.Context(), "extractUsageOnComplete",
				map[string]any{"taskId": "k-1", "model": tc.model, "upstreamModel": tc.model, "state": tc.state}, map[string]any{"status": "SUCCESS"}, reply)
			require.NoError(t, err)
			assert.Equal(t, tc.want, object(t, facts))
		})
	}
	// A character count MiniMax could not have billed keeps the estimate.
	facts, err := plugin.Engine.Call(t.Context(), "extractUsageOnComplete", map[string]any{"state": map[string]any{"vendor": "speech"}}, map[string]any{"status": "SUCCESS"},
		map[string]any{"extra_info": map[string]any{"usage_characters": 20000}})
	require.NoError(t, err)
	assert.Nil(t, facts)
}

// Fork: /v1/videos, the OpenAI video protocol, serves every video model of the
// plugin on a Baidu channel. A model a vendor's plugin also declares is served
// by that plugin on the vendor's channel, and a client cannot tell the two
// channels apart: the same request is accepted or refused alike, the same body
// reaches the upstream, the same usage is reserved and settled and the same
// replies come back, also when one channel fails and the other takes over.
func TestBaiduTaskPluginServesOpenAIVideoAsTheVendorPluginsDo(t *testing.T) {
	const (
		key   = "sk-baidu-test"
		image = "https://cdn.example/cat.png"
		clip  = "not really a video"
	)
	// The fake upstream is a loopback address, which the gateway would not
	// fetch a video from.
	fetchSetting := system_setting.GetFetchSetting()
	previousFetchSetting := *fetchSetting
	fetchSetting.AllowPrivateIp, fetchSetting.AllowedPorts = true, []string{"1-65535"}
	previousRetry := common.RetryTimes
	t.Cleanup(func() {
		*fetchSetting, common.RetryTimes = previousFetchSetting, previousRetry
		service.InitHttpClient()
	})
	harness := newRelayHarness(t)
	previousAdaptor := service.GetTaskAdaptorFunc
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor { return relay.GetTaskAdaptor(platform) }
	t.Cleanup(func() { service.GetTaskAdaptorFunc = previousAdaptor })
	video := harness.upstream.URL + "/files/video.mp4"

	upload := &bytes.Buffer{}
	form := multipart.NewWriter(upload)
	for name, value := range map[string]string{"model": "kling-v3", "prompt": "a cat", "seconds": "5", "metadata": `{"negative_prompt":"blur"}`} {
		require.NoError(t, form.WriteField(name, value))
	}
	part, err := form.CreateFormFile("input_reference", "cat.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("\x89PNG not really an image"))
	require.NoError(t, err)
	require.NoError(t, form.Close())

	type refusal struct{ body, message string }
	executingPlugin := regexp.MustCompile(`plugin \S+ hook buildSubmitRequest failed: Error: | at [^"]+\(\S+\.js:\d+:\d+\)`)
	shared := []struct {
		name, model string
		// vendor is the vendor's plugin and vendorType a channel type it serves.
		vendor     string
		vendorType int
		// expression prices the model, in US dollars.
		expression        string
		contentType, body string
		// sent is the body both channels send, at the vendor's path and at the
		// path Baidu serves the vendor under.
		sent                      string
		vendorSubmit, baiduSubmit string
		vendorQuery, baiduQuery   string
		// vendorRetrieve is the request the vendor's plugin makes for the
		// video's address before it fetches the video, if any.
		vendorRetrieve        string
		created, done, failed string
		facts                 string
		reserved, settled     float64
		// retrieved is what the plugin adds to the video the host renders.
		retrieved, retrievedFailed string
		refusals                   []refusal
	}{
		{
			name: "seedance", model: "doubao-seedance-2-0-260128", vendor: "doubao", vendorType: constant.ChannelTypeDoubaoVideo,
			expression:   `u("video_input") == "video" ? tier("video", u("tokens") * 6 / 1000000) : tier("base", u("tokens") * 10 / 1000000)`,
			body:         `{"model":"doubao-seedance-2-0-260128","prompt":"a cat","seconds":"5","size":"1280x720","input_reference":"` + image + `","metadata":{"watermark":false,"content":[{"type":"text","text":"ignored"},{"type":"video_url","video_url":{"url":"https://cdn.example/in.mp4"}}]}}`,
			sent:         `{"model":"doubao-seedance-2-0-260128","watermark":false,"content":[{"type":"image_url","image_url":{"url":"` + image + `"}},{"type":"video_url","video_url":{"url":"https://cdn.example/in.mp4"}},{"type":"text","text":"a cat"}],"duration":5}`,
			vendorSubmit: "/api/v3/contents/generations/tasks", baiduSubmit: "/async/seedance/api/v3/contents/generations/tasks",
			vendorQuery: "/api/v3/contents/generations/tasks/cgt-1", baiduQuery: "/async/seedance/api/v3/contents/generations/tasks/cgt-1",
			created: `{"id":"cgt-1"}`,
			done:    `{"id":"cgt-1","status":"succeeded","resolution":"720p","content":{"video_url":"` + video + `"},"usage":{"completion_tokens":100000,"total_tokens":100000}}`,
			failed:  `{"id":"cgt-1","status":"failed","error":{"code":"OutputVideoSensitiveContentDetected","message":"blocked"}}`,
			// 5 s x 1280 x 720 x 24 / 1024 tokens at the size's tier; Ark reports 100000.
			facts:    `{"tokens":108000,"resolution":"720p","video_input":"video"}`,
			reserved: 0.648, settled: 0.6,
			retrieved: `{}`, retrievedFailed: `{"error":{"message":"blocked","code":"OutputVideoSensitiveContentDetected"}}`,
			refusals: []refusal{
				// Refused by the decoders, before a channel is picked.
				{`{"model":"doubao-seedance-2-0-260128","prompt":"a cat","seconds":0}`, "seconds must be -1 or between 1 and 3600"},
				// Refused when the request is built, on the channel.
				{`{"model":"doubao-seedance-2-0-fast-260128","prompt":"a cat","size":"1920x1080"}`, "doubao-seedance-2-0-fast-260128 resolution must be one of 480p, 720p"},
			},
		},
		{
			name: "kling v3", model: "kling-v3", vendor: "kling", vendorType: constant.ChannelTypeKling, expression: `tier("base", u("units") * 0.1)`,
			body:         `{"model":"kling-v3","prompt":"a cat","seconds":10,"size":"720x1280","mode":"pro","metadata":{"sound":"on","negative_prompt":"blur"}}`,
			sent:         `{"model_name":"kling-v3","prompt":"a cat","mode":"pro","aspect_ratio":"9:16","sound":"on","negative_prompt":"blur","duration":"10"}`,
			vendorSubmit: "/v1/videos/text2video", baiduSubmit: "/v3/aigc/kl/videos/text2video",
			vendorQuery: "/v1/videos/text2video/tsk-k3", baiduQuery: "/v3/aigc/kl/videos/text2video/tsk-k3",
			created: `{"code":0,"message":"SUCCEED","data":{"task_id":"tsk-k3","task_status":"submitted"}}`,
			done:    `{"code":0,"data":{"task_id":"tsk-k3","task_status":"succeed","task_result":{"videos":[{"url":"` + video + `","duration":"10.04"}]},"final_unit_deduction":"11"}}`,
			failed:  `{"code":0,"data":{"task_id":"tsk-k3","task_status":"failed","task_status_msg":"blocked"}}`,
			// 10 s in pro mode with audio at 1.2 units per second; Kling deducted 11.
			facts: `{"units":12}`, reserved: 1.2, settled: 1.1,
			retrieved: `{"seconds":"10.04"}`, retrievedFailed: `{"error":{"message":"blocked","code":""}}`,
			refusals: []refusal{
				{`{"model":"kling-v3","prompt":"a cat","seconds":"long"}`, "duration must be a positive integer of at most 3600"},
				{`{"model":"kling-v3","prompt":"a cat","size":"wide"}`, "size must be WIDTHxHEIGHT, such as 1280x720"},
			},
		},
		{
			name: "kling v3 with an uploaded image", model: "kling-v3", vendor: "kling", vendorType: constant.ChannelTypeKling, expression: `tier("base", u("units") * 0.1)`,
			contentType: form.FormDataContentType(), body: upload.String(),
			sent:         `{"model_name":"kling-v3","prompt":"a cat","image":"iVBORyBub3QgcmVhbGx5IGFuIGltYWdl","mode":"std","negative_prompt":"blur","duration":"5"}`,
			vendorSubmit: "/v1/videos/image2video", baiduSubmit: "/v3/aigc/kl/videos/image2video",
			vendorQuery: "/v1/videos/image2video/tsk-k3", baiduQuery: "/v3/aigc/kl/videos/image2video/tsk-k3",
			created: `{"code":0,"message":"SUCCEED","data":{"task_id":"tsk-k3","task_status":"submitted"}}`,
			done:    `{"code":0,"data":{"task_id":"tsk-k3","task_status":"succeed","task_result":{"videos":[{"url":"` + video + `","duration":"5"}]}}}`,
			failed:  `{"code":5000,"message":"internal error","data":{"task_id":"tsk-k3","task_status":"failed","task_status_msg":""}}`,
			// 5 s in std mode at 0.6 units per second; without a deduction the estimate stays.
			facts: `{"units":3}`, reserved: 0.3, settled: 0.3,
			retrieved: `{"seconds":"5"}`, retrievedFailed: `{"error":{"message":"","code":""}}`,
		},
		{
			name: "kling 4.0", model: "kling-4.0", vendor: "kling", vendorType: constant.ChannelTypeKling, expression: `tier("base", u("units") * 0.1)`,
			body:         `{"model":"kling-4.0","prompt":"a cat","seconds":5,"size":"1280x720","image":"` + image + `","metadata":{"image_tail":"https://cdn.example/end.png","options":{"watermark":false}}}`,
			sent:         `{"contents":[{"type":"prompt","text":"a cat"},{"type":"first_frame","url":"` + image + `"},{"type":"last_frame","url":"https://cdn.example/end.png"}],"settings":{"resolution":"720p","duration":5},"options":{"watermark":false}}`,
			vendorSubmit: "/image-to-video/kling-4.0", baiduSubmit: "/async/kling/image-to-video/kling-4.0",
			vendorQuery: "/tasks?task_ids=k4-1", baiduQuery: "/async/kling/tasks?task_ids=k4-1",
			created: `{"code":0,"message":"SUCCEED","data":{"id":"k4-1","status":"submitted"}}`,
			done:    `{"code":0,"data":[{"id":"k4-1","status":"succeeded","outputs":[{"type":"video","url":"` + video + `","duration":"5.1"}],"billing":[{"charge_type":"unit","amount":"4"}]}]}`,
			failed:  `{"code":0,"data":[{"id":"k4-1","status":"failed","message":"blocked"}]}`,
			// 5 s at one unit per second; Kling deducted 4 units.
			facts: `{"units":5}`, reserved: 0.5, settled: 0.4,
			retrieved: `{"seconds":"5.1"}`, retrievedFailed: `{"error":{"message":"blocked","code":""}}`,
			refusals: []refusal{
				{`{"model":"kling-4.0","prompt":"a cat","seconds":31}`, "duration must be an integer from 3 to 30"},
				{`{"model":"kling-4.0","prompt":"a cat","metadata":{"options":{"callback_url":"https://client.example/hook"}}}`, "options.callback_url is not supported"},
			},
		},
		{
			name: "vidu", model: "viduq3-pro", vendor: "vidu", vendorType: constant.ChannelTypeVidu,
			expression:   `u("resolution") == "1080p" ? tier("1080p", u("duration") * 0.1) : tier("other", u("duration") * 0.05)`,
			body:         `{"model":"viduq3-pro","prompt":"a cat","seconds":8,"size":"1920x1080","metadata":{"movement_amplitude":"auto","bgm":true,"duration":99,"model":"viduq1"}}`,
			sent:         `{"model":"viduq3-pro","movement_amplitude":"auto","bgm":true,"duration":8,"resolution":"1080p","prompt":"a cat"}`,
			vendorSubmit: "/ent/v2/text2video", baiduSubmit: "/v3/aigc/vd/ent/v2/text2video",
			vendorQuery: "/ent/v2/tasks/tsk-v1/creations", baiduQuery: "/v3/aigc/vd/ent/v2/tasks/tsk-v1/creations",
			created: `{"task_id":"tsk-v1","state":"created"}`,
			done:    `{"state":"success","credits":120,"creations":[{"url":"` + video + `"}]}`,
			failed:  `{"state":"failed","err_code":"blocked"}`,
			facts:   `{"duration":8,"resolution":"1080p"}`, reserved: 0.8, settled: 0.8,
			retrieved: `{}`, retrievedFailed: `{"error":{"message":"blocked","code":"blocked"}}`,
			refusals: []refusal{
				{`{"model":"viduq3-pro","prompt":"a cat","seconds":2.5}`, "seconds must be between 1 and 3600"},
			},
		},
		{
			name: "minimax h3", model: "MiniMax-H3", vendor: "hailuo", vendorType: constant.ChannelTypeMiniMax,
			expression:   `tier("base", u("seconds") * 0.1 + u("input_images") * 0.02 + u("input_video_seconds") * 0.1)`,
			body:         `{"model":"MiniMax-H3","prompt":"a cat","seconds":6,"size":"2K","input_reference":"` + image + `","metadata":{"aigc_watermark":true}}`,
			sent:         `{"model":"MiniMax-H3","content":[{"type":"text","text":"a cat"},{"type":"image_url","role":"first_frame","image_url":{"url":"` + image + `"}}],"resolution":"2K","duration":6,"ratio":"adaptive","aigc_watermark":true}`,
			vendorSubmit: "/v2/video_generation", baiduSubmit: "/v3/aigc/minimax/v2/video_generation",
			vendorQuery: "/v2/query/video_generation/tsk-m1", baiduQuery: "/v3/aigc/minimax/v2/query/video_generation/tsk-m1",
			created: `{"task_id":"tsk-m1"}`,
			done:    `{"task":{"id":"tsk-m1","model":"MiniMax-H3","status":"succeeded","content":{"url":"` + video + `"},"resolution":"768P","duration":6,"usage":{"output_seconds":6,"input_seconds":0,"input_image_count":1}}}`,
			failed:  `{"task":{"id":"tsk-m1","status":"failed","error":{"message":"blocked"}}}`,
			// 6 s and one input image at 2K; MiniMax rendered 768P, which this price does not tell apart.
			facts: `{"seconds":6,"resolution":"2K","input_images":1,"input_video_seconds":0}`, reserved: 0.62, settled: 0.62,
			retrieved: `{}`, retrievedFailed: `{}`,
			refusals: []refusal{
				{`{"model":"MiniMax-H3","prompt":"a cat","seconds":3}`, "MiniMax-H3 duration must be an integer between 4 and 15 seconds"},
				{`{"model":"MiniMax-H3","prompt":"a cat","size":"1080P"}`, "MiniMax-H3 resolution must be 768P or 2K"},
				{`{"model":"MiniMax-H3","seconds":5,"metadata":{"reference_video":"https://cdn.example/in.mp4"}}`, "MiniMax-H3 requires a non-empty text item"},
			},
		},
		{
			name: "minimax hailuo v1", model: "MiniMax-Hailuo-02", vendor: "hailuo", vendorType: constant.ChannelTypeMiniMax,
			expression:   `u("resolution") == "1080P" ? tier("1080P", u("seconds") * 0.06) : tier("other", u("seconds") * 0.03)`,
			body:         `{"model":"MiniMax-Hailuo-02","prompt":"a cat","seconds":6,"size":"1920x1080","input_reference":"` + image + `","metadata":{"prompt_optimizer":false}}`,
			sent:         `{"model":"MiniMax-Hailuo-02","prompt":"a cat","duration":6,"resolution":"1080P","prompt_optimizer":false,"first_frame_image":"` + image + `"}`,
			vendorSubmit: "/v1/video_generation", baiduSubmit: "/v3/aigc/minimax/v1/video_generation",
			vendorQuery: "/v1/query/video_generation?task_id=tsk-mv1", baiduQuery: "/v3/aigc/minimax/v1/query/video_generation?task_id=tsk-mv1",
			// MiniMax hands out the video's address for its file id only; Baidu
			// reports it with the status. The fake upstream answers the file
			// request with the status reply, which holds both.
			vendorRetrieve: "/v1/files/retrieve?file_id=f-1",
			created:        `{"task_id":"tsk-mv1","base_resp":{"status_code":0,"status_msg":"success"}}`,
			done:           `{"task_id":"tsk-mv1","status":"Success","file_id":"f-1","video_width":1366,"video_height":768,"file":{"file_id":"f-1","download_url":"` + video + `"},"base_resp":{"status_code":0,"status_msg":"success"}}`,
			failed:         `{"task_id":"tsk-mv1","status":"Fail","base_resp":{"status_code":1026,"status_msg":"blocked"}}`,
			// 6 s at the size's 1080P; the 1366x768 video settles at 768P.
			facts: `{"seconds":6,"resolution":"1080P","input_images":0,"input_video_seconds":0}`, reserved: 0.36, settled: 0.18,
			retrieved: `{}`, retrievedFailed: `{"error":{"message":"blocked","code":"1026"}}`,
			refusals: []refusal{
				{`{"model":"MiniMax-Hailuo-02","prompt":"a cat","seconds":7}`, "MiniMax-Hailuo-02 duration must be 6 or 10"},
				{`{"model":"MiniMax-Hailuo-02","prompt":"a cat","seconds":10,"size":"1920x1080"}`, "MiniMax-Hailuo-02 duration 10 only allows resolution 768P"},
			},
		},
	}

	own := []struct {
		name, body string
		// path and sent are the request Baidu receives.
		path, sent string
		header     [2]string
		created    string
		facts      string
		reserved   float64
		// message is the refusal of a request the plugin cannot bill.
		message string
	}{
		{
			name: "pixverse from a size", body: `{"model":"pixversev5.6","prompt":"a cat","seconds":8,"size":"1920x1080","metadata":{"motion_mode":"fast","generate_audio_switch":true}}`,
			path: "/v3/aigc/pv/video/text/generate", sent: `{"model":"v5.6","prompt":"a cat","aspect_ratio":"16:9","motion_mode":"fast","generate_audio_switch":true,"duration":8,"quality":"1080p"}`,
			created: `{"taskId":"tsk-p1"}`, facts: `{"seconds":8,"resolution":"1080p","motion_mode":"fast","audio":"on","multi_clip":false,"operation":"generate"}`, reserved: 0.8,
		},
		{
			name: "pixverse by default with an image", body: `{"model":"pixverse5.5","prompt":"a cat","image":"` + image + `"}`,
			path: "/v3/aigc/pv/video/img/generate", sent: `{"model":"v5.5","prompt":"a cat","image_list":[{"image_url":"` + image + `"}],"duration":5,"quality":"720p"}`,
			created: `{"taskId":"tsk-p1"}`, facts: `{"seconds":5,"resolution":"720p","motion_mode":"normal","audio":"off","multi_clip":false,"operation":"generate"}`, reserved: 0.5,
		},
		{name: "pixverse past the longest", body: `{"model":"pixversev6.0","prompt":"a cat","seconds":16}`, message: "duration must be an integer from 1 to 15"},
		{name: "pixverse action outside the list", body: `{"model":"pixversev6.0","prompt":"a cat","metadata":{"action":"extend"}}`, message: "metadata.action must be one of text_to_video, image_to_video, first_tail_to_video"},
		{
			name: "happyhorse", body: `{"model":"happyhorse-1.0-i2v","prompt":"a cat","seconds":5,"size":"1280x720","image":"` + image + `","metadata":{"parameters":{"watermark":false}}}`,
			path: "/v3/aigc/bailian/api/v1/services/aigc/video-generation/video-synthesis", header: [2]string{"X-DashScope-Async", "enable"},
			sent:    `{"model":"happyhorse-1.0-i2v","input":{"prompt":"a cat","media":[{"type":"first_frame","url":"` + image + `"}]},"parameters":{"resolution":"720P","duration":5,"watermark":false}}`,
			created: `{"output":{"task_id":"tsk-h1","task_status":"PENDING"}}`, facts: `{"seconds":5,"resolution":"720P"}`, reserved: 0.5,
		},
		{
			// Without a duration or a size the longest and the dearest are reserved.
			name: "happyhorse left to Bailian", body: `{"model":"happyhorse-1.1-t2v","prompt":"a cat"}`,
			path: "/v3/aigc/bailian/api/v1/services/aigc/video-generation/video-synthesis", header: [2]string{"X-DashScope-Async", "enable"},
			sent:    `{"model":"happyhorse-1.1-t2v","input":{"prompt":"a cat"},"parameters":{}}`,
			created: `{"output":{"task_id":"tsk-h1","task_status":"PENDING"}}`, facts: `{"seconds":15,"resolution":"1080P"}`, reserved: 1.5,
		},
		{name: "happyhorse duration past the bound", body: `{"model":"happyhorse-1.1-t2v","prompt":"a cat","seconds":99999}`, message: "parameters.duration must be an integer from 1 to 3600"},
		{
			name: "kling 4.0 flash", body: `{"model":"kling-4.0-flash","prompt":"a cat","seconds":5,"size":"1920x1080"}`,
			path: "/async/kling/text-to-video/kling-4.0-flash", sent: `{"prompt":"a cat","settings":{"resolution":"1080p","aspect_ratio":"16:9","duration":5}}`,
			created: `{"code":0,"data":{"id":"k4-1","status":"submitted"}}`, facts: `{"seconds":5,"resolution":"1080p","audio":"on","video_input":"none"}`, reserved: 0.5,
		},
		{name: "kling 3.0 turbo past its longest", body: `{"model":"kling-3.0-turbo","prompt":"a cat","seconds":16}`, message: "duration must be an integer from 3 to 15"},
		{
			name: "kling v2 master", body: `{"model":"kling-v2-master","prompt":"a cat","seconds":10,"mode":"pro","input_reference":"` + image + `"}`,
			path: "/v3/aigc/kl/videos/image2video", header: [2]string{"X-Response-Format", "orion"},
			sent:    `{"model_name":"kling-v2-master","prompt":"a cat","image":"` + image + `","mode":"pro","duration":"10"}`,
			created: `{"code":0,"data":{"task_id":"tsk-k2","task_status":"submitted"}}`, facts: `{"seconds":10,"resolution":"1080p","audio":"off","video_input":"none"}`, reserved: 1,
		},
		{
			name: "kling omni", body: `{"model":"kling-video-o1","prompt":"a cat","image":"` + image + `","size":"1280x720"}`,
			path: "/v3/aigc/kl/videos/omni-video", header: [2]string{"X-Response-Format", "orion"},
			sent:    `{"model_name":"kling-video-o1","prompt":"a cat","mode":"std","duration":"5","image_list":[{"image_url":"` + image + `","type":"first_frame"}]}`,
			created: `{"code":0,"data":{"task_id":"tsk-k2","task_status":"submitted"}}`, facts: `{"seconds":5,"resolution":"720p","audio":"off","video_input":"none"}`, reserved: 0.5,
		},
		{name: "kling duration of zero", body: `{"model":"kling-v2-master","prompt":"a cat","seconds":0}`, message: "duration must be a positive integer of at most 3600"},
		{
			name: "vidu q4", body: `{"model":"viduq4","prompt":"a cat","image":"` + image + `","size":"3840x2160"}`,
			path: "/v3/aigc/vd/ent/v2/img2video", sent: `{"model":"viduq4","prompt":"a cat","images":["` + image + `"],"duration":5,"resolution":"4K"}`,
			created: `{"task_id":"tsk-v1"}`, facts: `{"duration":5,"resolution":"4K"}`, reserved: 0.5,
		},
		{
			name: "minimax h3 max", body: `{"model":"MiniMax-H3-MAX","prompt":"a cat","size":"480P"}`,
			path: "/v3/aigc/minimax/v2/video_generation", sent: `{"model":"MiniMax-H3-MAX","content":[{"type":"text","text":"a cat"}],"resolution":"480P","duration":5,"ratio":"16:9"}`,
			created: `{"task_id":"tsk-m1"}`, facts: `{"seconds":5,"resolution":"480P"}`, reserved: 0.5,
		},
		{name: "minimax h3 max resolution of h3", body: `{"model":"MiniMax-H3-MAX","prompt":"a cat","size":"2K"}`, message: "MiniMax-H3-MAX resolution must be 480P or 768P"},
	}

	modes, expressions := map[string]string{}, map[string]string{}
	models := []string{"doubao-seedance-2-0-fast-260128"}
	for _, tc := range shared {
		modes[tc.model], expressions[tc.model] = "tiered_expr", tc.expression
		models = append(models, tc.model)
	}
	modes["doubao-seedance-2-0-fast-260128"], expressions["doubao-seedance-2-0-fast-260128"] = "tiered_expr", shared[0].expression
	for _, tc := range own {
		name := gjson.Get(tc.body, "model").String()
		usage := "seconds"
		if name == "viduq4" {
			usage = "duration"
		}
		modes[name], expressions[name] = "tiered_expr", `tier("base", u("`+usage+`") * 0.1)`
		models = append(models, name)
	}
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(option, value string) error {
		if option == "billing_setting.billing_mode" || option == "billing_setting.billing_expr" {
			saved[option] = value
		}
		return nil
	}))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	modeJSON, err := common.Marshal(modes)
	require.NoError(t, err)
	expressionJSON, err := common.Marshal(expressions)
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": string(modeJSON), "billing_setting.billing_expr": string(expressionJSON)}))

	baiduChannel := func() *model.Channel {
		channel := harness.pluginChannel("vod-baidu-task", key)
		channel.Models = strings.Join(models, ",")
		// Both of Baidu's systems are the fake upstream.
		channel.SetOtherSettings(dto.ChannelOtherSettings{PluginConfig: []byte(`{"asyncBaseUrl":"` + harness.upstream.URL + `"}`)})
		return channel
	}
	vendorChannel := func(channelType int) *model.Channel {
		channel := harness.newChannel(channelType, "vendor-key")
		channel.Models = strings.Join(models, ",")
		return channel
	}
	// install makes the channels the only ones, each preferred to the next.
	install := func(t *testing.T, channels ...*model.Channel) {
		for index, channel := range channels {
			priority := int64(len(channels) - index)
			channel.Priority = &priority
			if index == 0 {
				harness.install(t, channel)
				continue
			}
			channel.Id = 0
			require.NoError(t, channel.Insert())
		}
		model.InitChannelCache()
	}
	get := func(t *testing.T, path string) (int, string) {
		request, err := http.NewRequest(http.MethodGet, harness.gateway.URL+path, nil)
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer "+relayTestToken)
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		defer response.Body.Close()
		received, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		harness.waitIdle(t)
		return response.StatusCode, string(received)
	}
	// The task ids and the times are the gateway's and differ on every task.
	object := func(t *testing.T, body string) map[string]any {
		var decoded map[string]any
		require.NoError(t, common.Unmarshal([]byte(body), &decoded), body)
		for _, field := range []string{"id", "created_at", "completed_at"} {
			if _, present := decoded[field]; present {
				decoded[field] = field
			}
		}
		return decoded
	}
	quota := func(dollars float64) int { return int(dollars*common.QuotaPerUnit + 0.5) }
	balance := func(t *testing.T) int {
		var user model.User
		require.NoError(t, model.DB.First(&user, harness.userID).Error)
		return user.Quota
	}
	var queryReply atomic.Pointer[string]
	// failing is how many submissions the upstream still answers with an error.
	var failing atomic.Int32
	respond := func(created string) {
		harness.upstream.respond = func(w http.ResponseWriter, _ int, request capturedRequest) {
			switch {
			case request.URL == "/files/video.mp4":
				w.Header().Set("Content-Type", "video/mp4")
				_, _ = io.WriteString(w, clip)
			case request.Method == http.MethodPost && failing.Add(-1) >= 0:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"error":{"message":"upstream is down"}}`)
			case request.Method == http.MethodPost:
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, created)
			default:
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, *queryReply.Load())
			}
		}
	}
	poll := func(t *testing.T, reply string) model.Task {
		queryReply.Store(&reply)
		var task model.Task
		require.NoError(t, model.DB.Order("id DESC").First(&task).Error)
		upstreamID := task.GetUpstreamTaskID()
		service.DispatchPlatformUpdate(context.Background(), task.Platform, map[int][]string{task.ChannelId: {upstreamID}}, map[string]*model.Task{upstreamID: &task})
		require.NoError(t, model.DB.Order("id DESC").First(&task).Error)
		return task
	}

	// served is everything a client and the bill see of one request that is
	// polled to its end, and of a second one the vendor fails.
	type served struct {
		requests          []capturedRequest
		platform          constant.TaskPlatform
		create            map[string]any
		facts             string
		reserved, settled int
		retrieve          map[string]any
		content           string
		failed            map[string]any
		failReason        string
	}
	for _, tc := range shared {
		t.Run(tc.name, func(t *testing.T) {
			contentType := tc.contentType
			if contentType == "" {
				contentType = "application/json"
			}
			respond(tc.created)
			// serve sends the request with the channels installed in order; the
			// upstream fails the submissions of all but the last.
			serve := func(t *testing.T, channels ...*model.Channel) served {
				install(t, channels...)
				common.RetryTimes = len(channels) - 1
				harness.upstream.reset()
				failing.Store(int32(len(channels) - 1))

				outcome := harness.send(t, "/v1/videos", contentType, tc.body)

				require.Equal(t, http.StatusOK, outcome.status, outcome.body)
				result := served{requests: harness.upstream.captured(), create: object(t, outcome.body), facts: gjson.Get(outcome.log.Other, "usage_facts").Raw, reserved: outcome.quota}
				require.Len(t, result.requests, len(channels))
				assert.NotContains(t, outcome.body, gjson.Get(tc.created, "id").String()+gjson.Get(tc.created, "task_id").String()+gjson.Get(tc.created, "data.id").String()+gjson.Get(tc.created, "data.task_id").String(), "the client gets the gateway's task id")

				task := poll(t, tc.done)
				require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status, task.FailReason)
				result.platform, result.settled = task.Platform, task.Quota
				status, body := get(t, "/v1/videos/"+task.TaskID)
				require.Equal(t, http.StatusOK, status, body)
				assert.Equal(t, task.TaskID, gjson.Get(body, "id").String())
				result.retrieve = object(t, body)
				status, result.content = get(t, "/v1/videos/"+task.TaskID+"/content")
				require.Equal(t, http.StatusOK, status, result.content)
				// The submissions, the poll, the vendor's file request and the video.
				result.requests = harness.upstream.captured()
				fetches := 2
				if tc.vendorRetrieve != "" && channels[len(channels)-1].Type == tc.vendorType {
					fetches = 3
				}
				require.Len(t, result.requests, len(channels)+fetches)

				// A task the vendor fails costs nothing.
				failing.Store(int32(len(channels) - 1))
				before := balance(t)
				outcome = harness.send(t, "/v1/videos", contentType, tc.body)
				require.Equal(t, http.StatusOK, outcome.status, outcome.body)
				task = poll(t, tc.failed)
				require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
				assert.Equal(t, before, balance(t), "the reservation is returned")
				result.failReason = task.FailReason
				status, body = get(t, "/v1/videos/"+task.TaskID)
				require.Equal(t, http.StatusOK, status, body)
				result.failed = object(t, body)
				return result
			}

			vendor := serve(t, vendorChannel(tc.vendorType))
			baidu := serve(t, baiduChannel())

			// Each channel is asked in its own way for the same thing.
			assert.Equal(t, constant.TaskPlatform(tc.vendor), vendor.platform)
			assert.Equal(t, constant.TaskPlatform("vod-baidu-task"), baidu.platform)
			vendorRequests := []string{tc.vendorSubmit, tc.vendorQuery, tc.vendorRetrieve, "/files/video.mp4"}
			if tc.vendorRetrieve == "" {
				vendorRequests = slices.Delete(vendorRequests, 2, 3)
			}
			sentURLs := make([]string, 0, len(vendor.requests))
			for _, request := range vendor.requests {
				sentURLs = append(sentURLs, request.URL)
			}
			assert.Equal(t, vendorRequests, sentURLs)
			assert.Equal(t, []string{tc.baiduSubmit, tc.baiduQuery, "/files/video.mp4"}, []string{baidu.requests[0].URL, baidu.requests[1].URL, baidu.requests[2].URL})
			assert.Equal(t, "Bearer "+key, baidu.requests[0].Header.Get("Authorization"))
			assert.Equal(t, "Bearer "+key, baidu.requests[1].Header.Get("Authorization"))
			assert.Empty(t, baidu.requests[2].Header.Get("Authorization"), "the video's address takes no credentials")
			assert.JSONEq(t, tc.sent, vendor.requests[0].Body)
			assert.JSONEq(t, tc.sent, baidu.requests[0].Body)

			// The client and the bill see no difference.
			assert.JSONEq(t, tc.facts, vendor.facts)
			assert.Equal(t, quota(tc.reserved), vendor.reserved, "the estimate is reserved")
			assert.Equal(t, quota(tc.settled), vendor.settled, "the reported usage is settled")
			assert.Equal(t, "queued", vendor.create["status"])
			assert.Equal(t, tc.model, vendor.create["model"])
			assert.Equal(t, "completed", vendor.retrieve["status"])
			assert.Equal(t, "failed", vendor.failed["status"])
			for field, value := range object(t, tc.retrieved) {
				assert.Equal(t, value, vendor.retrieve[field], field)
			}
			assert.Equal(t, object(t, tc.retrievedFailed)["error"], vendor.failed["error"])
			assert.Equal(t, clip, vendor.content)
			same := func(t *testing.T, other served) {
				assert.Equal(t, vendor.create, other.create)
				assert.JSONEq(t, vendor.facts, other.facts)
				assert.Equal(t, vendor.reserved, other.reserved)
				assert.Equal(t, vendor.settled, other.settled)
				assert.Equal(t, vendor.retrieve, other.retrieve)
				assert.Equal(t, vendor.content, other.content)
				assert.Equal(t, vendor.failed, other.failed)
				assert.Equal(t, vendor.failReason, other.failReason)
			}
			same(t, baidu)

			// When the first channel's upstream is down the other serves the
			// request, in either order, and the client sees what it would have
			// seen from one channel alone.
			t.Run("the vendor's channel fails over to Baidu's", func(t *testing.T) {
				result := serve(t, vendorChannel(tc.vendorType), baiduChannel())
				assert.Equal(t, []string{tc.vendorSubmit, tc.baiduSubmit, tc.baiduQuery}, []string{result.requests[0].URL, result.requests[1].URL, result.requests[2].URL})
				assert.Equal(t, constant.TaskPlatform("vod-baidu-task"), result.platform)
				same(t, result)
			})
			t.Run("Baidu's channel fails over to the vendor's", func(t *testing.T) {
				result := serve(t, baiduChannel(), vendorChannel(tc.vendorType))
				assert.Equal(t, []string{tc.baiduSubmit, tc.vendorSubmit, tc.vendorQuery}, []string{result.requests[0].URL, result.requests[1].URL, result.requests[2].URL})
				assert.Equal(t, constant.TaskPlatform(tc.vendor), result.platform)
				same(t, result)
			})

			// A request one channel refuses the other refuses in the same
			// words, before anything is sent or reserved. Around a refusal of
			// the request it built, the host names the plugin and the place in
			// its source: the only difference a client can see.
			for _, refused := range tc.refusals {
				var replies []string
				for _, channel := range []*model.Channel{vendorChannel(tc.vendorType), baiduChannel()} {
					install(t, channel)
					harness.upstream.reset()
					outcome := harness.post(t, "/v1/videos", refused.body)
					// 400, or 503 when the gateway's OpenAI video plugin would
					// take the request and has no channel.
					assert.GreaterOrEqual(t, outcome.status, http.StatusBadRequest, outcome.body)
					assert.Contains(t, outcome.body, refused.message)
					assert.Empty(t, harness.upstream.captured())
					assert.Zero(t, outcome.quota)
					replies = append(replies, strconv.Itoa(outcome.status)+" "+executingPlugin.ReplaceAllString(outcome.body, ""))
				}
				assert.Equal(t, replies[0], replies[1], refused.body)
			}
		})
	}

	// The models no other plugin declares, on a Baidu channel.
	install(t, baiduChannel())
	common.RetryTimes = 0
	failing.Store(0)
	for _, tc := range own {
		t.Run(tc.name, func(t *testing.T) {
			respond(tc.created)
			harness.upstream.reset()

			outcome := harness.post(t, "/v1/videos", tc.body)

			if tc.message != "" {
				assert.Equal(t, http.StatusBadRequest, outcome.status, outcome.body)
				assert.Contains(t, outcome.body, tc.message)
				assert.Empty(t, harness.upstream.captured())
				assert.Zero(t, outcome.quota)
				return
			}
			require.Equal(t, http.StatusOK, outcome.status, outcome.body)
			requests := harness.upstream.captured()
			require.Len(t, requests, 1)
			assert.Equal(t, tc.path, requests[0].URL)
			assert.Equal(t, "Bearer "+key, requests[0].Header.Get("Authorization"))
			if tc.header[0] != "" {
				assert.Equal(t, tc.header[1], requests[0].Header.Get(tc.header[0]))
			}
			assert.JSONEq(t, tc.sent, requests[0].Body)
			assert.JSONEq(t, tc.facts, gjson.Get(outcome.log.Other, "usage_facts").Raw)
			assert.Equal(t, quota(tc.reserved), outcome.quota, "the estimate is reserved")
			assert.Equal(t, "queued", gjson.Get(outcome.body, "status").String())
			assert.Equal(t, gjson.Get(tc.body, "model").String(), gjson.Get(outcome.body, "model").String())
		})
	}
}
