package router

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"

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
