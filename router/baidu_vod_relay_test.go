package router

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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
	assert.Equal(t, []string{"MiniMax-H3"}, shared["hailuo"])
	assert.Len(t, shared["vidu"], 13)

	// A path both declare reaches the vendor's plugin first.
	for path, owner := range map[string]string{
		"/api/v3/contents/generations/tasks":                     "doubao",
		"/v1/videos/text2video":                                  "kling",
		"/text-to-video/kling-4.0":                               "kling",
		"/ent/v2/text2video":                                     "vidu",
		"/v2/video_generation":                                   "hailuo",
		"/api/v1/services/aigc/video-generation/video-synthesis": "alibaba",
		"/openapi/v2/video/text/generate":                        "vod-baidu-task",
		"/v1/t2a_v2":                                             "vod-baidu-task",
	} {
		binding, ok := generation.LookupDeclaredRoute(http.MethodPost, path)
		require.True(t, ok, path)
		assert.Equal(t, owner, binding.Plugin.Meta.Key, path)
	}
	// The provider plugin of phase one keeps no task part.
	provider, found := generation.Get("baidu-vod")
	require.True(t, found)
	assert.False(t, provider.Meta.HasTaskPart())
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
	}

	const speechModel = "speech-2.8-hd"
	modes, expressions := map[string]string{speechModel: "tiered_expr"}, map[string]string{speechModel: `tier("base", u("characters") * 500 / 1000000)`}
	models := []string{speechModel}
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
			want: map[string]any{"seconds": float64(8), "resolution": "1080p", "motion_mode": "fast", "audio": "on", "multi_clip": true},
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
