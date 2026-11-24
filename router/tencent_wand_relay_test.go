package router

import (
	"context"
	"io"
	"maps"
	"net/http"
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
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Fork: the built-in tencent-wand plugin serves Tencent's WAND Vega video
// models on /v1/videos through a New API channel. It declares the usage facts
// the upstream gateway prices the models with, so that gateway's expressions
// can be saved here; it bills each request from the body it sends upstream;
// and it shares /v1/videos with sora without taking sora's models.
func TestTencentWandPluginServesVegaThroughANewAPIChannel(t *testing.T) {
	const (
		key    = "sk-upstream-gateway"
		lite10 = "wand-vega-video-1.0-lite"
		lite15 = "wand-vega-video-1.5-lite"
		// The upstream's price of 1.5-lite, in US dollars.
		lite15Expr = `u("resolution") == "480P" ? tier("480P", u("input_video_seconds") * 0.039705882353 + u("seconds") * 0.039705882353 + u("tokens") * 0 / 1000000) : u("resolution") == "720P" ? tier("720P", u("input_video_seconds") * 0.079411764706 + u("seconds") * 0.079411764706 + u("tokens") * 0 / 1000000) : u("resolution") == "1080P" ? tier("1080P", u("input_video_seconds") * 0.158823529412 + u("seconds") * 0.158823529412 + u("tokens") * 0 / 1000000) : u("resolution") == "2K" ? tier("2K", u("input_video_seconds") * 0.158823529412 + u("seconds") * 0.190588235294 + u("tokens") * 0 / 1000000) : tier("4K", u("input_video_seconds") * 0.158823529412 + u("seconds") * 0.228676470588 + u("tokens") * 0 / 1000000)`
		// A price of the same form for 1.0-lite, which also bills input images.
		lite10Expr = `u("resolution") == "480P" ? tier("480P", u("input_video_seconds") * 0.03 + u("seconds") * 0.03 + u("images") * 0.01 + u("tokens") * 0 / 1000000) : u("resolution") == "768P" ? tier("768P", u("input_video_seconds") * 0.04 + u("seconds") * 0.04 + u("images") * 0.01 + u("tokens") * 0 / 1000000) : u("resolution") == "1080P" ? tier("1080P", u("input_video_seconds") * 0.05 + u("seconds") * 0.05 + u("images") * 0.01 + u("tokens") * 0 / 1000000) : u("resolution") == "2K" ? tier("2K", u("input_video_seconds") * 0.05 + u("seconds") * 0.06 + u("images") * 0.01 + u("tokens") * 0 / 1000000) : tier("4K", u("input_video_seconds") * 0.05 + u("seconds") * 0.07 + u("images") * 0.01 + u("tokens") * 0 / 1000000)`
		video      = "not really a video"
	)
	expressions := map[string]string{lite10: lite10Expr, lite15: lite15Expr}

	generation := jsplugin.DefaultRegistry.Generation()
	plugin, found := generation.Get("tencent-wand")
	require.True(t, found, "the built-in plugin is not served")
	sora, found := generation.Get("sora")
	require.True(t, found)

	// The two models are this plugin's alone, with the usage facts the
	// upstream meters, and the price sync's check of an upstream expression
	// (customBillingExprError) passes for them.
	for name, fields := range map[string][]string{
		lite10: {"tokens", "seconds", "input_video_seconds", "images", "resolution"},
		lite15: {"tokens", "seconds", "input_video_seconds", "resolution"},
	} {
		assert.Equal(t, []*jsplugin.LoadedPlugin{plugin}, generation.PluginsByModel(name), name)
		binding, ok := generation.LookupEndpoint(http.MethodPost, "/v1/videos", name)
		require.True(t, ok, name)
		assert.Same(t, plugin, binding.Plugin, name)
		schema, _ := plugin.Meta.UsageForModel(name)
		assert.ElementsMatch(t, fields, slices.Collect(maps.Keys(schema)), name)
		assert.NoError(t, billing_setting.SmokeTestTaskExpr(expressions[name], schema), name)
	}
	lite10Schema, _ := plugin.Meta.UsageForModel(lite10)
	lite15Schema, _ := plugin.Meta.UsageForModel(lite15)
	assert.Equal(t, []string{"480P", "768P", "1080P", "2K", "4K"}, lite10Schema["resolution"].Enum)
	assert.Equal(t, []string{"480P", "720P", "1080P", "2K", "4K"}, lite15Schema["resolution"].Enum)
	// Sora's models stay sora's on the path both plugins serve.
	for _, name := range sora.Meta.Models {
		assert.Equal(t, []*jsplugin.LoadedPlugin{sora}, generation.PluginsByModel(name), name)
		candidates := generation.LookupEndpointCandidates(http.MethodPost, "/v1/videos", name)
		require.Len(t, candidates, 1, name)
		assert.Same(t, sora, candidates[0].Plugin, name)
	}

	// The fake upstream is a loopback address, which the gateway would not
	// fetch a video from.
	fetchSetting := system_setting.GetFetchSetting()
	previousFetchSetting := *fetchSetting
	fetchSetting.AllowPrivateIp, fetchSetting.AllowedPorts = true, []string{"1-65535"}
	t.Cleanup(func() {
		*fetchSetting = previousFetchSetting
		service.InitHttpClient()
	})
	harness := newRelayHarness(t)
	previousAdaptor := service.GetTaskAdaptorFunc
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor { return relay.GetTaskAdaptor(platform) }
	t.Cleanup(func() { service.GetTaskAdaptorFunc = previousAdaptor })
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(option, value string) error {
		if option == "billing_setting.billing_mode" || option == "billing_setting.billing_expr" {
			saved[option] = value
		}
		return nil
	}))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	modeJSON, err := common.Marshal(map[string]string{lite10: "tiered_expr", lite15: "tiered_expr"})
	require.NoError(t, err)
	expressionJSON, err := common.Marshal(expressions)
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": string(modeJSON), "billing_setting.billing_expr": string(expressionJSON)}))

	// The channel also binds sora, which this gateway offers as a second
	// candidate for every video model on /v1/videos; the Vega models still
	// execute on this plugin. An alias the channel maps to a Vega model is
	// sent, and billed, as that model.
	channel := harness.newChannel(constant.ChannelTypeNewAPI, key)
	channel.Models = lite10 + "," + lite15 + ",vega-fast"
	mapping := `{"vega-fast":"` + lite15 + `"}`
	channel.ModelMapping = &mapping
	channel.SetSetting(dto.ChannelSettings{TaskExtendPluginKeys: []string{"sora", "tencent-wand"}})
	harness.install(t, channel)
	var queryReply atomic.Pointer[string]
	harness.upstream.respond = func(w http.ResponseWriter, _ int, request capturedRequest) {
		switch {
		case request.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"task_up1","object":"video","model":"`+gjson.Get(request.Body, "model").String()+`","status":"queued","progress":0,"created_at":1795491701}`)
		case strings.HasSuffix(request.URL, "/content"):
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = io.WriteString(w, video)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, *queryReply.Load())
		}
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
	poll := func(t *testing.T, reply string) model.Task {
		queryReply.Store(&reply)
		var task model.Task
		require.NoError(t, model.DB.Order("id DESC").First(&task).Error)
		upstreamID := task.GetUpstreamTaskID()
		service.DispatchPlatformUpdate(context.Background(), task.Platform, map[int][]string{channel.Id: {upstreamID}}, map[string]*model.Task{upstreamID: &task})
		require.NoError(t, model.DB.Order("id DESC").First(&task).Error)
		return task
	}
	images := func(count int) string {
		return strings.Repeat(`,{"type":"input_image","image_url":"https://cdn.example/a.png"}`, count)
	}
	input := func(count int) string {
		return `[{"role":"user","content":[{"type":"input_text","text":"a cat"}` + images(count) + `]}]`
	}

	for name, tc := range map[string]struct {
		body string
		// sent is the body the upstream receives: the documented fields only,
		// with the resolution and the duration that are billed.
		sent  string
		facts string
		quota int
	}{
		"1.0-lite with two images": {
			body:  `{"model":"` + lite10 + `","input":` + input(2) + `,"resolution":"480p","ratio":"16:9","seconds":60}`,
			sent:  `{"model":"` + lite10 + `","input":` + input(2) + `,"resolution":"480P","duration":5,"ratio":"16:9"}`,
			facts: `{"tokens":148500,"seconds":5,"input_video_seconds":0,"images":0,"resolution":"480P"}`,
			// 5 s at 0.03.
			quota: 75000,
		},
		"1.0-lite with a sixth image": {
			body:  `{"model":"` + lite10 + `","input":` + input(6) + `,"resolution":"480P","duration":5}`,
			sent:  `{"model":"` + lite10 + `","input":` + input(6) + `,"resolution":"480P","duration":5}`,
			facts: `{"tokens":163500,"seconds":5,"input_video_seconds":0,"images":1,"resolution":"480P"}`,
			// 5 s at 0.03 and one image at 0.01.
			quota: 80000,
		},
		"1.5-lite in 4K": {
			body:  `{"model":"` + lite15 + `","input":"a cat","resolution":"4K","duration":5}`,
			sent:  `{"model":"` + lite15 + `","input":"a cat","resolution":"4K","duration":5}`,
			facts: `{"tokens":777500,"seconds":5,"input_video_seconds":0,"resolution":"4K"}`,
			// 5 s at 0.228676470588.
			quota: 571691,
		},
		"a channel alias of 1.5-lite": {
			body:  `{"model":"vega-fast","input":"a cat","resolution":"2k","duration":4}`,
			sent:  `{"model":"` + lite15 + `","input":"a cat","resolution":"2K","duration":4}`,
			facts: `{"tokens":518400,"seconds":4,"input_video_seconds":0,"resolution":"2K"}`,
			// 4 s at 0.190588235294, the price of the model the alias maps to.
			quota: 381176,
		},
		"1.5-lite by default": {
			body:  `{"model":"` + lite15 + `","input":"a cat"}`,
			sent:  `{"model":"` + lite15 + `","input":"a cat","resolution":"720P","duration":5}`,
			facts: `{"tokens":270000,"seconds":5,"input_video_seconds":0,"resolution":"720P"}`,
			// 5 s at 0.079411764706.
			quota: 198529,
		},
	} {
		t.Run(name, func(t *testing.T) {
			harness.upstream.reset()

			outcome := harness.post(t, "/v1/videos", tc.body)

			require.Equal(t, http.StatusOK, outcome.status, outcome.body)
			requests := harness.upstream.captured()
			require.Len(t, requests, 1)
			assert.Equal(t, "/v1/videos", requests[0].URL)
			assert.Equal(t, "Bearer "+key, requests[0].Header.Get("Authorization"))
			assert.JSONEq(t, tc.sent, requests[0].Body)
			assert.JSONEq(t, tc.facts, gjson.Get(outcome.log.Other, "usage_facts").Raw)
			assert.Equal(t, tc.quota, outcome.quota, "the estimate is reserved")
			assert.Equal(t, "queued", gjson.Get(outcome.body, "status").String())
			assert.NotContains(t, outcome.body, "task_up1", "the client gets the gateway's task id")
		})
	}

	// The upstream's reply reports no usage, so a finished task costs what
	// was reserved and a failed one nothing.
	t.Run("polls to completion and serves the video", func(t *testing.T) {
		harness.upstream.reset()
		outcome := harness.post(t, "/v1/videos", `{"model":"`+lite15+`","input":"a cat","resolution":"4K","duration":5}`)
		require.Equal(t, http.StatusOK, outcome.status, outcome.body)
		id := gjson.Get(outcome.body, "id").String()

		task := poll(t, `{"id":"task_up1","object":"video","model":"`+lite15+`","status":"in_progress","progress":40,"created_at":1795491701}`)
		assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.Status)
		assert.Equal(t, constant.TaskPlatform("tencent-wand"), task.Platform)
		assert.Equal(t, id, task.TaskID)
		requests := harness.upstream.captured()
		require.Len(t, requests, 2)
		assert.Equal(t, http.MethodGet, requests[1].Method)
		assert.Equal(t, "/v1/videos/task_up1", requests[1].URL)
		assert.Equal(t, "Bearer "+key, requests[1].Header.Get("Authorization"))

		task = poll(t, `{"id":"task_up1","object":"video","model":"`+lite15+`","status":"completed","progress":100,"created_at":1795491701,"completed_at":1795491801}`)
		assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
		assert.Equal(t, 571691, task.Quota)

		status, body := get(t, "/v1/videos/"+id)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, id, gjson.Get(body, "id").String())
		assert.Equal(t, "completed", gjson.Get(body, "status").String())
		assert.Equal(t, lite15, gjson.Get(body, "model").String())

		status, body = get(t, "/v1/videos/"+id+"/content")
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, video, body)
		requests = harness.upstream.captured()
		require.Len(t, requests, 4)
		assert.Equal(t, "/v1/videos/task_up1/content", requests[3].URL)
		assert.Equal(t, "Bearer "+key, requests[3].Header.Get("Authorization"))

		outcome = harness.post(t, "/v1/videos", `{"model":"`+lite15+`","input":"a cat"}`)
		require.Equal(t, http.StatusOK, outcome.status, outcome.body)
		var user model.User
		require.NoError(t, model.DB.First(&user, harness.userID).Error)
		reserved := user.Quota
		task = poll(t, `{"id":"task_up1","object":"video","model":"`+lite15+`","status":"failed","progress":0,"created_at":1795491701,"error":{"message":"blocked"}}`)
		assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
		assert.Contains(t, task.FailReason, "blocked")
		require.NoError(t, model.DB.First(&user, harness.userID).Error)
		assert.Equal(t, 198529, user.Quota-reserved, "the reservation is returned")
	})

	// A request the plugin cannot bill is refused before anything is sent or
	// reserved.
	for name, tc := range map[string]struct{ body, message string }{
		"duration below the shortest": {
			body: `{"model":"` + lite10 + `","input":"a cat","duration":3}`, message: "duration must be an integer from 4 to 15",
		},
		"duration above the longest": {
			body: `{"model":"` + lite15 + `","input":"a cat","duration":16}`, message: "duration must be an integer from 4 to 15",
		},
		"resolution of the other model": {
			body: `{"model":"` + lite15 + `","input":"a cat","resolution":"768P"}`, message: "resolution must be one of 480P, 720P, 1080P, 2K, 4K",
		},
		"video input": {
			body:    `{"model":"` + lite15 + `","input":[{"role":"user","content":[{"type":"input_text","text":"a cat"},{"type":"input_video","video_url":"https://cdn.example/in.mp4"}]}]}`,
			message: `input content type \"input_video\" is not supported`,
		},
		"more images than the cap": {
			body: `{"model":"` + lite10 + `","input":` + input(33) + `}`, message: "input accepts at most 32 images",
		},
	} {
		t.Run(name, func(t *testing.T) {
			harness.upstream.reset()

			outcome := harness.post(t, "/v1/videos", tc.body)

			assert.Equal(t, http.StatusBadRequest, outcome.status, outcome.body)
			assert.Contains(t, outcome.body, tc.message)
			assert.Empty(t, harness.upstream.captured())
			assert.Zero(t, outcome.quota)
		})
	}
}
