package router

import (
	"bytes"
	"context"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestMuleRouterPluginContractsAndPricingSchemas(t *testing.T) {
	plugin, found := jsplugin.DefaultRegistry.Get("mulerouter")
	require.True(t, found, "mulerouter built-in plugin must be registered")
	assert.True(t, plugin.Meta.HasTaskPart())
	assert.True(t, plugin.Meta.HasProviderPart())

	taskModels := []string{
		"berry-1.0",
		"berry-1.0-pro",
		"berry-1.0-turbo",
		"berry-1.0-pro-turbo",
		"wan2.7-i2v-spicy",
		"wan2.2-i2v-spicy",
		"w2.2-i2v-lora",
		"wan-animate",
		"flashvsr",
		"z-image-spicy",
		"z-image-spicy-pro",
		"qwen-image-edit-spicy",
		"face-swap",
		"head-swap",
	}
	assert.ElementsMatch(t, taskModels, plugin.Meta.Models)
	assert.NotContains(t, plugin.Meta.Models, "carrothub-papaya-1.0")
	assert.True(t, plugin.Meta.RelaysModel("carrothub-papaya-1.0"))
	for _, m := range taskModels {
		assert.False(t, plugin.Meta.RelaysModel(m), "task model %s must not be marked as relayed", m)
	}

	expressions := map[string]string{
		"berry-1.0":             `u("resolution") == "480p" ? tier("480p", u("seconds") * 0.05) : u("resolution") == "720p" ? tier("720p", u("seconds") * 0.10) : tier("1080p", u("seconds") * 0.20)`,
		"berry-1.0-pro":         `u("resolution") == "1080p" ? tier("1080p", u("seconds") * 0.18) : u("resolution") == "2k" ? tier("2k", u("seconds") * 0.20) : tier("4k", u("seconds") * 0.23)`,
		"berry-1.0-turbo":       `u("resolution") == "480p" ? tier("480p", u("seconds") * 0.068) : u("resolution") == "720p" ? tier("720p", u("seconds") * 0.14) : tier("1080p", u("seconds") * 0.28)`,
		"berry-1.0-pro-turbo":   `u("resolution") == "1080p" ? tier("1080p", u("seconds") * 0.26) : u("resolution") == "2k" ? tier("2k", u("seconds") * 0.28) : tier("4k", u("seconds") * 0.31)`,
		"wan2.7-i2v-spicy":      `u("resolution") == "720p" ? tier("720p", u("seconds") * 0.13) : tier("1080p", u("seconds") * 0.20)`,
		"wan2.2-i2v-spicy":      `u("resolution") == "480p" ? tier("480p", u("seconds") * 0.02 + (u("prompt_extend") ? 0.001 : 0)) : tier("720p", u("seconds") * 0.04 + (u("prompt_extend") ? 0.001 : 0))`,
		"w2.2-i2v-lora":         `u("resolution") == "480p" ? tier("480p", u("seconds") * 0.02) : tier("720p", u("seconds") * 0.04)`,
		"wan-animate":           `u("resolution") == "480p" ? tier("480p", u("seconds") * 0.053) : tier("720p", u("seconds") * 0.11)`,
		"flashvsr":              `u("resolution") == "720p" ? tier("720p", u("seconds") * 0.016) : u("resolution") == "1080p" ? tier("1080p", u("seconds") * 0.024) : u("resolution") == "2k" ? tier("2k", u("seconds") * 0.032) : tier("4k", u("seconds") * 0.053)`,
		"z-image-spicy":         `tier("base", u("image_count") * 0.013 + (u("prompt_extend") ? 0.001 : 0))`,
		"z-image-spicy-pro":     `tier("base", u("image_count") * 0.02)`,
		"qwen-image-edit-spicy": `u("input_images") == "1" ? tier("1img", u("image_count") * 0.04) : u("input_images") == "2" ? tier("2img", u("image_count") * 0.043) : tier("3img", u("image_count") * 0.046)`,
		"face-swap":             `tier("base", u("image_count") * 0.013)`,
		"head-swap":             `tier("base", u("image_count") * 0.013)`,
	}
	expectedFields := map[string][]string{
		"berry-1.0":             {"seconds", "resolution"},
		"berry-1.0-pro":         {"seconds", "resolution"},
		"berry-1.0-turbo":       {"seconds", "resolution"},
		"berry-1.0-pro-turbo":   {"seconds", "resolution"},
		"wan2.7-i2v-spicy":      {"seconds", "resolution"},
		"wan2.2-i2v-spicy":      {"seconds", "resolution", "prompt_extend"},
		"w2.2-i2v-lora":         {"seconds", "resolution"},
		"wan-animate":           {"seconds", "resolution"},
		"flashvsr":              {"seconds", "resolution"},
		"z-image-spicy":         {"image_count", "prompt_extend"},
		"z-image-spicy-pro":     {"image_count"},
		"qwen-image-edit-spicy": {"image_count", "input_images"},
		"face-swap":             {"image_count"},
		"head-swap":             {"image_count"},
	}

	generation := jsplugin.DefaultRegistry.Generation()
	for _, m := range taskModels {
		assert.Equal(t, []*jsplugin.LoadedPlugin{plugin}, generation.PluginsByModel(m), m)
		schema, _ := plugin.Meta.UsageForModel(m)
		assert.ElementsMatch(t, expectedFields[m], slices.Collect(maps.Keys(schema)), m)
		assert.NoError(t, billing_setting.SmokeTestTaskExpr(expressions[m], schema), m)
	}
}

func TestMuleRouterPluginServesChatAndAsyncTasksEndToEnd(t *testing.T) {
	const (
		mrKey = "sk-mulerouter-test"
		all15 = "berry-1.0,berry-1.0-pro,berry-1.0-turbo,berry-1.0-pro-turbo,wan2.7-i2v-spicy,wan2.2-i2v-spicy,w2.2-i2v-lora,wan-animate,flashvsr,z-image-spicy,z-image-spicy-pro,qwen-image-edit-spicy,face-swap,head-swap,carrothub-papaya-1.0"
	)

	fetchSetting := system_setting.GetFetchSetting()
	previousFetch := *fetchSetting
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = []string{"1-65535"}
	t.Cleanup(func() {
		*fetchSetting = previousFetch
		service.InitHttpClient()
	})

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

	expressions := map[string]string{
		"carrothub-papaya-1.0":  `tier("base", p * 0.15 + c * 0.45)`,
		"berry-1.0":             `u("resolution") == "480p" ? tier("480p", u("seconds") * 0.05) : u("resolution") == "720p" ? tier("720p", u("seconds") * 0.10) : tier("1080p", u("seconds") * 0.20)`,
		"berry-1.0-pro":         `u("resolution") == "1080p" ? tier("1080p", u("seconds") * 0.18) : u("resolution") == "2k" ? tier("2k", u("seconds") * 0.20) : tier("4k", u("seconds") * 0.23)`,
		"wan2.7-i2v-spicy":      `u("resolution") == "720p" ? tier("720p", u("seconds") * 0.13) : tier("1080p", u("seconds") * 0.20)`,
		"wan2.2-i2v-spicy":      `u("resolution") == "480p" ? tier("480p", u("seconds") * 0.02 + (u("prompt_extend") ? 0.001 : 0)) : tier("720p", u("seconds") * 0.04 + (u("prompt_extend") ? 0.001 : 0))`,
		"w2.2-i2v-lora":         `u("resolution") == "480p" ? tier("480p", u("seconds") * 0.02) : tier("720p", u("seconds") * 0.04)`,
		"wan-animate":           `u("resolution") == "480p" ? tier("480p", u("seconds") * 0.053) : tier("720p", u("seconds") * 0.11)`,
		"flashvsr":              `u("resolution") == "720p" ? tier("720p", u("seconds") * 0.016) : u("resolution") == "1080p" ? tier("1080p", u("seconds") * 0.024) : u("resolution") == "2k" ? tier("2k", u("seconds") * 0.032) : tier("4k", u("seconds") * 0.053)`,
		"z-image-spicy":         `tier("base", u("image_count") * 0.013 + (u("prompt_extend") ? 0.001 : 0))`,
		"z-image-spicy-pro":     `tier("base", u("image_count") * 0.02)`,
		"qwen-image-edit-spicy": `u("input_images") == "1" ? tier("1img", u("image_count") * 0.04) : u("input_images") == "2" ? tier("2img", u("image_count") * 0.043) : tier("3img", u("image_count") * 0.046)`,
		"face-swap":             `tier("base", u("image_count") * 0.013)`,
		"head-swap":             `tier("base", u("image_count") * 0.013)`,
	}
	modes := make(map[string]string, len(expressions))
	for m := range expressions {
		modes[m] = "tiered_expr"
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
	exprJSON, err := common.Marshal(expressions)
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": string(modeJSON),
		"billing_setting.billing_expr": string(exprJSON),
	}))

	installVendorChannel := func(t *testing.T) *model.Channel {
		ch := harness.pluginChannel("mulerouter", mrKey)
		ch.Models = all15
		return harness.install(t, ch)
	}

	callJSON := func(t *testing.T, method, path, body string) (int, string) {
		req, err := http.NewRequest(method, gateway.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+relayTestToken)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		harness.waitIdle(t)
		return resp.StatusCode, string(raw)
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
	pollNext := func(t *testing.T, channel *model.Channel) model.Task {
		task := lastTask(t)
		upID := task.GetUpstreamTaskID()
		service.DispatchPlatformUpdate(context.Background(), task.Platform, map[int][]string{channel.Id: {upID}}, map[string]*model.Task{upID: &task})
		return lastTask(t)
	}

	t.Run("papaya text model relays across chat, responses, messages, and gemini", func(t *testing.T) {
		installVendorChannel(t)
		harness.upstream.respond = func(w http.ResponseWriter, _ int, request capturedRequest) {
			writeChat(w, request)
		}
		for name, tc := range map[string]struct {
			path string
			body string
		}{
			"chat completions": {
				path: "/v1/chat/completions",
				body: `{"model":"carrothub-papaya-1.0","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`,
			},
			"responses": {
				path: "/v1/responses",
				body: `{"model":"carrothub-papaya-1.0","input":"hi"}`,
			},
			"anthropic messages": {
				path: "/v1/messages",
				body: `{"model":"carrothub-papaya-1.0","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`,
			},
			"gemini generateContent": {
				path: "/v1beta/models/carrothub-papaya-1.0:generateContent",
				body: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			},
		} {
			t.Run(name, func(t *testing.T) {
				harness.upstream.reset()
				status, body := callJSON(t, http.MethodPost, tc.path, tc.body)
				require.Equal(t, http.StatusOK, status, body)
				reqs := harness.upstream.captured()
				require.Len(t, reqs, 1)
				assert.Equal(t, "/vendors/openai/v1/chat/completions", reqs[0].URL)
				assert.Equal(t, "Bearer "+mrKey, reqs[0].Header.Get("Authorization"))
			})
		}
	})

	t.Run("native video, super-resolution, lora, image submit/poll/settle/refund", func(t *testing.T) {
		ch := installVendorChannel(t)
		for _, tc := range []struct {
			name         string
			path         string
			body         string
			wantUpPath   string
			wantUpBody   string
			created      string
			running      string
			done         string
			failed       string
			reserved     float64
			settled      float64
			wantMediaKey string
		}{
			{
				name:       "berry-1.0 pending->completed",
				path:       "/vendors/carrothub/v1/berry-1.0/generation",
				body:       `{"prompt":"a cat","resolution":"720p","duration":5,"audio":false}`,
				wantUpPath: "/vendors/carrothub/v1/berry-1.0/generation",
				wantUpBody: `{"prompt":"a cat","resolution":"720p","ratio":"adaptive","duration":5,"audio":false,"prompt_extend":true}`,
				created:    `{"task_info":{"id":"up-berry-1","status":"pending","created_at":"2026-08-21T00:00:00Z","updated_at":"2026-08-21T00:00:00Z"}}`,
				running:    `{"task_info":{"id":"up-berry-1","status":"processing","created_at":"2026-08-21T00:00:00Z","updated_at":"2026-08-21T00:00:05Z"}}`,
				done:       `{"task_info":{"id":"up-berry-1","status":"completed","created_at":"2026-08-21T00:00:00Z","updated_at":"2026-08-21T00:01:00Z"},"videos":["` + harness.upstream.URL + `/media/berry.mp4"]}`,
				failed:     `{"task_info":{"id":"up-berry-1","status":"failed","error":{"code":400,"title":"Invalid Request","detail":"content policy"}}}`,
				reserved:   0.5, settled: 0.5,
				wantMediaKey: "videos.0",
			},
			{
				name:       "flashvsr queued->succeeded strips extra fields and normalizes target_resolution",
				path:       "/vendors/mulerouter/v1/flashvsr/generation",
				body:       `{"video":"https://cdn.example/in.mp4","target_resolution":"4K","duration":10}`,
				wantUpPath: "/vendors/mulerouter/v1/flashvsr/generation",
				wantUpBody: `{"video":"https://cdn.example/in.mp4","resolution":"4k"}`,
				created:    `{"task_info":{"id":"up-vsr-1","status":"queued"}}`,
				running:    `{"task_info":{"id":"up-vsr-1","status":"running"}}`,
				done:       `{"task_info":{"id":"up-vsr-1","status":"succeeded"},"videos":["` + harness.upstream.URL + `/media/vsr.mp4"],"usage":{"duration":10}}`,
				failed:     `{"task_info":{"id":"up-vsr-1","status":"failed"},"error":{"code":500,"detail":"upstream crash"}}`,
				reserved:   120 * 0.053, settled: 10 * 0.053,
				wantMediaKey: "videos.0",
			},
			{
				name:       "w2.2-i2v-lora with loras",
				path:       "/vendors/carrothub/v1/w2.2-i2v-lora/generation",
				body:       `{"prompt":"dance","image":"https://cdn.example/a.png","duration":8,"resolution":"720p","loras":[{"url":"https://civitai.com/models/1","scale":1.5}]}`,
				wantUpPath: "/vendors/carrothub/v1/w2.2-i2v-lora/generation",
				wantUpBody: `{"prompt":"dance","image":"https://cdn.example/a.png","resolution":"720p","duration":8,"loras":[{"url":"https://civitai.com/models/1","scale":1.5}]}`,
				created:    `{"task_info":{"id":"up-lora-1","status":"pending"}}`,
				running:    `{"task_info":{"id":"up-lora-1","status":"processing"}}`,
				done:       `{"task_info":{"id":"up-lora-1","status":"completed"},"videos":["` + harness.upstream.URL + `/media/lora.mp4"]}`,
				failed:     `{"task_info":{"id":"up-lora-1","status":"failed","error":{"code":422,"detail":"LORA_URL_INVALID"}}}`,
				reserved:   0.32, settled: 0.32,
				wantMediaKey: "videos.0",
			},
			{
				name:       "qwen-image-edit-spicy with 2 images",
				path:       "/vendors/carrothub/v1/qwen-image-edit-spicy/generation",
				body:       `{"image":"https://cdn.example/main.png","reference_images":["https://cdn.example/ref2.png"],"prompt":"swap shirt"}`,
				wantUpPath: "/vendors/carrothub/v1/qwen-image-edit-spicy/generation",
				wantUpBody: `{"image":"https://cdn.example/main.png","reference_images":["https://cdn.example/ref2.png"],"prompt":"swap shirt"}`,
				created:    `{"task_info":{"id":"up-qwen-1","status":"pending"}}`,
				running:    `{"task_info":{"id":"up-qwen-1","status":"processing"}}`,
				done:       `{"task_info":{"id":"up-qwen-1","status":"completed"},"images":["` + harness.upstream.URL + `/media/edit.png"]}`,
				failed:     `{"task_info":{"id":"up-qwen-1","status":"failed","error":{"code":400,"detail":"bad image"}}}`,
				reserved:   0.043, settled: 0.043,
				wantMediaKey: "images.0",
			},
			{
				name:       "z-image-spicy with prompt_extend default true",
				path:       "/vendors/carrothub/v1/z-image-spicy/generation",
				body:       `{"prompt":"mountain lake"}`,
				wantUpPath: "/vendors/carrothub/v1/z-image-spicy/generation",
				wantUpBody: `{"prompt":"mountain lake","width":1024,"height":1536,"prompt_extend":true}`,
				created:    `{"task_info":{"id":"up-zimg-1","status":"pending"}}`,
				running:    `{"task_info":{"id":"up-zimg-1","status":"processing"}}`,
				done:       `{"task_info":{"id":"up-zimg-1","status":"completed"},"images":["` + harness.upstream.URL + `/media/z.png"]}`,
				failed:     `{"task_info":{"id":"up-zimg-1","status":"failed","error":{"code":400,"detail":"nsfw"}}}`,
				reserved:   0.014, settled: 0.014,
				wantMediaKey: "images.0",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var reply atomic.Pointer[string]
				harness.upstream.respond = func(w http.ResponseWriter, _ int, req capturedRequest) {
					w.Header().Set("Content-Type", "application/json")
					if req.Method == http.MethodPost {
						_, _ = io.WriteString(w, tc.created)
						return
					}
					_, _ = io.WriteString(w, *reply.Load())
				}
				harness.upstream.reset()
				before := spent(t)

				status, body := callJSON(t, http.MethodPost, tc.path, tc.body)
				require.Equal(t, http.StatusOK, status, body)
				reqs := harness.upstream.captured()
				require.Len(t, reqs, 1)
				assert.Equal(t, tc.wantUpPath, reqs[0].URL)
				assert.Equal(t, "Bearer "+mrKey, reqs[0].Header.Get("Authorization"))
				assert.JSONEq(t, tc.wantUpBody, reqs[0].Body)

				task := lastTask(t)
				assert.Equal(t, task.TaskID, gjson.Get(body, "task_info.id").String())
				assert.NotContains(t, body, task.GetUpstreamTaskID())
				assert.Equal(t, quota(tc.reserved), spent(t)-before)

				reply.Store(&tc.running)
				task = pollNext(t, ch)
				assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.Status)

				reply.Store(&tc.done)
				task = pollNext(t, ch)
				assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
				assert.Equal(t, quota(tc.settled), spent(t)-before)

				status, statusBody := callJSON(t, http.MethodGet, tc.path+"/"+task.TaskID, "")
				require.Equal(t, http.StatusOK, status, statusBody)
				assert.Equal(t, task.TaskID, gjson.Get(statusBody, "task_info.id").String())
				assert.NotEmpty(t, gjson.Get(statusBody, tc.wantMediaKey).String())

				// Failure returns reservation in full.
				beforeFail := spent(t)
				status, _ = callJSON(t, http.MethodPost, tc.path, tc.body)
				require.Equal(t, http.StatusOK, status)
				reply.Store(&tc.failed)
				task = pollNext(t, ch)
				assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
				assert.Zero(t, spent(t)-beforeFail)
			})
		}
	})

	t.Run("openai_video protocol covers berry -1 duration, reference videos, wan27/wan22/animate and /content", func(t *testing.T) {
		ch := installVendorChannel(t)
		const fakeMP4 = "FAKE_MP4_BYTES"
		var reply atomic.Pointer[string]
		harness.upstream.respond = func(w http.ResponseWriter, _ int, req capturedRequest) {
			if req.URL == "/media/video.mp4" {
				w.Header().Set("Content-Type", "video/mp4")
				_, _ = io.WriteString(w, fakeMP4)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if req.Method == http.MethodPost {
				_, _ = io.WriteString(w, `{"task_info":{"id":"up-v1","status":"pending"}}`)
				return
			}
			_, _ = io.WriteString(w, *reply.Load())
		}

		// Berry duration -1 reserves 30s, settles on reported 6s, and serves /content.
		harness.upstream.reset()
		before := spent(t)
		status, body := callJSON(t, http.MethodPost, "/v1/videos", `{"model":"berry-1.0","prompt":"drone mountain","seconds":-1,"size":"1280x720"}`)
		require.Equal(t, http.StatusOK, status, body)
		reqs := harness.upstream.captured()
		require.Len(t, reqs, 1)
		assert.Equal(t, "/vendors/carrothub/v1/berry-1.0/generation", reqs[0].URL)
		assert.JSONEq(t, `{"prompt":"drone mountain","resolution":"720p","ratio":"16:9","duration":-1,"audio":true,"prompt_extend":true}`, reqs[0].Body)
		assert.Equal(t, quota(3.0), spent(t)-before, "duration -1 reserves max 30s at 720p ($0.10/s)")

		done := `{"task_info":{"id":"up-v1","status":"completed"},"videos":["` + harness.upstream.URL + `/media/video.mp4"],"usage":{"output_video_duration":6}}`
		reply.Store(&done)
		task := pollNext(t, ch)
		assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
		assert.Equal(t, quota(0.6), spent(t)-before, "settled at reported 6s ($0.60)")

		status, statusBody := callJSON(t, http.MethodGet, "/v1/videos/"+task.TaskID, "")
		require.Equal(t, http.StatusOK, status, statusBody)
		assert.Equal(t, "completed", gjson.Get(statusBody, "status").String())
		assert.Equal(t, "berry-1.0", gjson.Get(statusBody, "model").String())

		contentReq, err := http.NewRequest(http.MethodGet, gateway.URL+"/v1/videos/"+task.TaskID+"/content", nil)
		require.NoError(t, err)
		contentReq.Header.Set("Authorization", "Bearer "+relayTestToken)
		contentResp, err := http.DefaultClient.Do(contentReq)
		require.NoError(t, err)
		contentBytes, err := io.ReadAll(contentResp.Body)
		contentResp.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, contentResp.StatusCode)
		assert.Equal(t, fakeMP4, string(contentBytes))

		// Berry with reference_videos reserves duration + 15s.
		beforeRef := spent(t)
		status, _ = callJSON(t, http.MethodPost, "/v1/videos", `{"model":"berry-1.0","prompt":"v","metadata":{"reference_videos":["https://cdn.example/ref.mp4"]},"seconds":5,"resolution":"480p"}`)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, quota(20*0.05), spent(t)-beforeRef)

		// Wan 2.7, Wan 2.2, Wan Animate via /v1/videos.
		for _, tc := range []struct {
			body, upPath, upBody string
			reserved             float64
		}{
			{
				body:     `{"model":"wan2.7-i2v-spicy","prompt":"dance","image":"https://cdn.example/p.png","resolution":"720p","seconds":6}`,
				upPath:   "/vendors/carrothub/v1/wan2.7-i2v-spicy/generation",
				upBody:   `{"prompt":"dance","image":"https://cdn.example/p.png","resolution":"720p","duration":6,"audio":true,"prompt_extend":true}`,
				reserved: 6 * 0.13,
			},
			{
				body:     `{"model":"wan2.2-i2v-spicy","prompt":"wave","image":"https://cdn.example/f1.png","metadata":{"last_image":"https://cdn.example/f2.png","prompt_extend":false},"duration":8,"resolution":"480p"}`,
				upPath:   "/vendors/carrothub/v1/wan2.2-i2v-spicy/generation",
				upBody:   `{"image":"https://cdn.example/f1.png","last_image":"https://cdn.example/f2.png","prompt":"wave","duration":8,"resolution":"480p","prompt_extend":false}`,
				reserved: 8 * 0.02,
			},
			{
				body:     `{"model":"wan-animate","image":"https://cdn.example/char.png","video":"https://cdn.example/tmpl.mp4","mode":"replace","resolution":"480p"}`,
				upPath:   "/vendors/carrothub/v1/wan-animate/generation",
				upBody:   `{"image":"https://cdn.example/char.png","video":"https://cdn.example/tmpl.mp4","mode":"replace","resolution":"480p"}`,
				reserved: 30 * 0.053,
			},
		} {
			harness.upstream.reset()
			b := spent(t)
			s, out := callJSON(t, http.MethodPost, "/v1/videos", tc.body)
			require.Equal(t, http.StatusOK, s, out)
			reqs = harness.upstream.captured()
			require.Len(t, reqs, 1)
			assert.Equal(t, tc.upPath, reqs[0].URL)
			assert.JSONEq(t, tc.upBody, reqs[0].Body)
			assert.Equal(t, quota(tc.reserved), spent(t)-b)
		}
	})

	t.Run("openai_image and openai_responses protocols", func(t *testing.T) {
		ch := installVendorChannel(t)
		var mode atomic.Pointer[string]
		m := "image"
		mode.Store(&m)
		harness.upstream.respond = func(w http.ResponseWriter, _ int, req capturedRequest) {
			w.Header().Set("Content-Type", "application/json")
			if req.Method == http.MethodPost {
				_, _ = io.WriteString(w, `{"task_info":{"id":"up-sync-1","status":"pending"}}`)
				return
			}
			if *mode.Load() == "video" {
				_, _ = io.WriteString(w, `{"task_info":{"id":"up-sync-1","status":"completed"},"videos":["https://cdn.example/out.mp4"]}`)
				return
			}
			_, _ = io.WriteString(w, `{"task_info":{"id":"up-sync-1","status":"completed"},"images":["https://cdn.example/out.png"]}`)
		}

		// Background poll helper while sync openai_image / openai_responses waits for completion.
		drivePoll := func() {
			for range 100 {
				var task model.Task
				if err := model.DB.Where("status = ? AND channel_id = ?", model.TaskStatusSubmitted, ch.Id).Order("id DESC").First(&task).Error; err == nil {
					upID := task.GetUpstreamTaskID()
					service.DispatchPlatformUpdate(context.Background(), task.Platform, map[int][]string{ch.Id: {upID}}, map[string]*model.Task{upID: &task})
					return
				}
				time.Sleep(15 * time.Millisecond)
			}
		}

		go drivePoll()
		before := spent(t)
		status, body := callJSON(t, http.MethodPost, "/v1/images/generations", `{"model":"z-image-spicy-pro","prompt":"astronaut","size":"1280x720"}`)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, quota(0.02), spent(t)-before)
		assert.Contains(t, gjson.Get(body, "data.0.url").String(), "out.png")

		// face-swap via multipart /v1/images/edits with image + face_image upload.
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		require.NoError(t, mw.WriteField("model", "face-swap"))
		imgPart, err := mw.CreateFormFile("image", "target.png")
		require.NoError(t, err)
		_, _ = io.WriteString(imgPart, "PNGDATA")
		facePart, err := mw.CreateFormFile("face_image", "face.png")
		require.NoError(t, err)
		_, _ = io.WriteString(facePart, "FACEDATA")
		require.NoError(t, mw.Close())

		harness.upstream.reset()
		go drivePoll()
		req, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/images/edits", &buf)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+relayTestToken)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		editBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		harness.waitIdle(t)
		require.Equal(t, http.StatusOK, resp.StatusCode, string(editBytes))
		reqs := harness.upstream.captured()
		require.GreaterOrEqual(t, len(reqs), 1)
		lastPost := reqs[0]
		assert.Equal(t, "/vendors/carrothub/v1/face-swap/generation", lastPost.URL)
		assert.True(t, strings.HasPrefix(gjson.Get(lastPost.Body, "image").String(), "data:image/png;base64,"))
		assert.True(t, strings.HasPrefix(gjson.Get(lastPost.Body, "face_image").String(), "data:image/png;base64,"))

		// openai_responses background mode for berry-1.0-pro.
		vMode := "video"
		mode.Store(&vMode)
		beforeResp := spent(t)
		status, respBody := callJSON(t, http.MethodPost, "/v1/responses", `{"model":"berry-1.0-pro","input":"sunset sea","resolution":"2k","duration":4,"background":true}`)
		require.Equal(t, http.StatusOK, status, respBody)
		assert.Equal(t, "queued", gjson.Get(respBody, "status").String())
		assert.Equal(t, quota(4*0.20), spent(t)-beforeResp)
		responseID := gjson.Get(respBody, "id").String()
		require.True(t, strings.HasPrefix(responseID, "resp_"), respBody)
		pollNext(t, ch)
		status, respGet := callJSON(t, http.MethodGet, "/v1/responses/"+responseID, "")
		require.Equal(t, http.StatusOK, status, respGet)
		assert.Equal(t, "completed", gjson.Get(respGet, "status").String())
		assert.Contains(t, gjson.Get(respGet, "output.0.content.0.text").String(), "<video controls src=")
	})

	t.Run("new_api upstream cascade adds /mulerouter prefix and honors model mapping", func(t *testing.T) {
		ch := harness.newChannel(constant.ChannelTypeNewAPI, "sk-cascade-key")
		ch.Models = "mr-berry"
		mapping := `{"mr-berry":"berry-1.0"}`
		ch.ModelMapping = &mapping
		ch.SetSetting(dto.ChannelSettings{TaskExtendPluginKeys: []string{"mulerouter"}})
		harness.install(t, ch)

		harness.upstream.respond = func(w http.ResponseWriter, _ int, _ capturedRequest) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"task_info":{"id":"up-casc-1","status":"pending"}}`)
		}
		harness.upstream.reset()
		before := spent(t)
		status, body := callJSON(t, http.MethodPost, "/v1/videos", `{"model":"mr-berry","prompt":"city","resolution":"480p","seconds":4}`)
		require.Equal(t, http.StatusOK, status, body)
		reqs := harness.upstream.captured()
		require.Len(t, reqs, 1)
		assert.Equal(t, "/mulerouter/vendors/carrothub/v1/berry-1.0/generation", reqs[0].URL)
		assert.Equal(t, quota(4*0.05), spent(t)-before)
	})

	t.Run("invalid requests are refused before reserving quota or calling upstream", func(t *testing.T) {
		installVendorChannel(t)
		for name, tc := range map[string]struct {
			path, body, errSubstr string
		}{
			"berry duration out of range": {
				path: "/v1/videos", body: `{"model":"berry-1.0","prompt":"cat","duration":31}`, errSubstr: "duration must be -1 or an integer from 2 to 30",
			},
			"berry keyframe and reference mix": {
				path:      "/vendors/carrothub/v1/berry-1.0/generation",
				body:      `{"prompt":"cat","first_frame":"https://cdn.example/1.png","reference_images":["https://cdn.example/2.png"]}`,
				errSubstr: "cannot mix keyframe fields",
			},
			"berry file and link mix": {
				path:      "/vendors/carrothub/v1/berry-1.0/generation",
				body:      `{"prompt":"cat","file":"https://cdn.example/a.pdf","link":"https://example.com"}`,
				errSubstr: "file and link are mutually exclusive",
			},
			"lora scale out of bounds": {
				path:      "/vendors/carrothub/v1/w2.2-i2v-lora/generation",
				body:      `{"prompt":"dance","image":"https://cdn.example/1.png","loras":[{"url":"https://example.com/a.safetensors","scale":5}]}`,
				errSubstr: "loras[0].scale must be between 0 and 4",
			},
			"lora count over 3": {
				path:      "/vendors/carrothub/v1/w2.2-i2v-lora/generation",
				body:      `{"prompt":"dance","image":"https://cdn.example/1.png","loras":[{"url":"u1"},{"url":"u2"},{"url":"u3"},{"url":"u4"}]}`,
				errSubstr: "loras accepts at most 3 items",
			},
			"qwen edit over 2 reference images": {
				path:      "/vendors/carrothub/v1/qwen-image-edit-spicy/generation",
				body:      `{"image":"https://cdn.example/m.png","prompt":"edit","reference_images":["u1","u2","u3"]}`,
				errSubstr: "reference_images accepts at most 2 items",
			},
			"openai_image n > 1": {
				path:      "/v1/images/generations",
				body:      `{"model":"z-image-spicy","prompt":"cat","n":2}`,
				errSubstr: "n must be 1",
			},
			"flashvsr missing resolution": {
				path:      "/v1/videos",
				body:      `{"model":"flashvsr","video":"https://cdn.example/in.mp4"}`,
				errSubstr: "resolution is required",
			},
			"flashvsr missing video": {
				path:      "/vendors/mulerouter/v1/flashvsr/generation",
				body:      `{"resolution":"720p"}`,
				errSubstr: "requires video",
			},
		} {
			t.Run(name, func(t *testing.T) {
				harness.upstream.reset()
				before := spent(t)
				status, body := callJSON(t, http.MethodPost, tc.path, tc.body)
				assert.Equal(t, http.StatusBadRequest, status, body)
				assert.Contains(t, body, tc.errSubstr)
				assert.Empty(t, harness.upstream.captured())
				assert.Zero(t, spent(t)-before)
			})
		}
	})
}
