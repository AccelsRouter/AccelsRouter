package autoroute

import (
	"strings"
	"testing"
)

func TestExtractInput(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		body       string
		wantText   string
		wantFormat Format
		wantTools  bool
		wantImages bool
	}{
		{
			name: "openai chat, string content",
			path: "/v1/chat/completions", wantFormat: FormatMessages,
			body:     `{"model":"auto","messages":[{"role":"system","content":"You are an agent. Write code."},{"role":"user","content":"你好"}]}`,
			wantText: "你好",
		},
		{
			name: "openai chat, parts and an image",
			path: "/v1/chat/completions", wantFormat: FormatMessages, wantImages: true,
			body:     `{"messages":[{"role":"user","content":[{"type":"text","text":"这张图里是什么"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`,
			wantText: "这张图里是什么",
		},
		{
			name: "openai chat, tool result in a tool message is skipped",
			path: "/v1/chat/completions", wantFormat: FormatMessages, wantTools: true,
			body:     `{"tools":[{"type":"function","function":{"name":"read_file"}}],"messages":[{"role":"user","content":"重构 auth 模块"},{"role":"assistant","tool_calls":[{"id":"1"}]},{"role":"tool","content":"package auth func main() {}"}]}`,
			wantText: "重构 auth 模块",
		},
		{
			name: "claude agent loop: last user message holds only a tool_result",
			path: "/v1/messages", wantFormat: FormatMessages,
			body: `{"system":"x","messages":[
				{"role":"user","content":"Refactor the auth module and add unit tests"},
				{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"read_file","input":{}}]},
				{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"package auth\nfunc Login() {}\nclass Session {}"}]}]}`,
			wantText: "Refactor the auth module and add unit tests",
		},
		{
			name: "claude: text block next to a tool_result block",
			path: "/v1/messages", wantFormat: FormatMessages,
			body:     `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"},{"type":"text","text":"now run the tests"}]}]}`,
			wantText: "now run the tests",
		},
		{
			name: "claude: image only, no text anywhere",
			path: "/v1/messages", wantFormat: FormatMessages, wantImages: true,
			body:     `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"AAAA"}}]}]}`,
			wantText: "",
		},
		{
			name: "responses: string input",
			path: "/v1/responses", wantFormat: FormatResponses,
			body:     `{"model":"auto","instructions":"be brief","input":"证明根号2是无理数"}`,
			wantText: "证明根号2是无理数",
		},
		{
			name: "responses: input array whose content is an array of input_text (the gap in OmniRoute)",
			path: "/v1/responses", wantFormat: FormatResponses,
			body:     `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"实现一个分布式缓存"}]}]}`,
			wantText: "实现一个分布式缓存",
		},
		{
			name: "responses: function_call_output after the user turn is skipped",
			path: "/v1/responses", wantFormat: FormatResponses, wantTools: true,
			body:     `{"tools":[{"type":"function","name":"shell"}],"input":[{"role":"user","content":"run the build"},{"type":"function_call","name":"shell"},{"type":"function_call_output","call_id":"1","output":"BUILD OK class function def"}]}`,
			wantText: "run the build",
		},
		{
			name: "responses body sent to the chat completions path",
			path: "/v1/chat/completions", wantFormat: FormatResponses,
			body:     `{"input":"hello"}`,
			wantText: "hello",
		},
		{
			name: "legacy completions: prompt string",
			path: "/v1/completions", wantFormat: FormatCompletions,
			body:     `{"prompt":"Translate to French: hello"}`,
			wantText: "Translate to French: hello",
		},
		{
			name: "legacy completions: prompt array",
			path: "/v1/completions", wantFormat: FormatCompletions,
			body:     `{"prompt":["a","b"]}`,
			wantText: "a\nb",
		},
		{
			name: "playground path",
			path: "/pg/chat/completions", wantFormat: FormatMessages,
			body:     `{"messages":[{"role":"user","content":"hi"}]}`,
			wantText: "hi",
		},
		{
			name: "unknown path, body decides",
			path: "/some/other", wantFormat: FormatMessages,
			body:     `{"messages":[{"role":"user","content":"hi there"}]}`,
			wantText: "hi there",
		},
		{
			name: "part without a type is read as text",
			path: "/v1/chat/completions", wantFormat: FormatMessages,
			body:     `{"messages":[{"role":"user","content":[{"text":"no type here"}]}]}`,
			wantText: "no type here",
		},
		{
			name: "legacy functions count as tools",
			path: "/v1/chat/completions", wantFormat: FormatMessages, wantTools: true,
			body:     `{"functions":[{"name":"f"}],"messages":[{"role":"user","content":"x"}]}`,
			wantText: "x",
		},
		{
			name: "empty tools array is not tools",
			path: "/v1/chat/completions", wantFormat: FormatMessages,
			body:     `{"tools":[],"messages":[{"role":"user","content":"x"}]}`,
			wantText: "x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, format := ExtractInput(tc.path, []byte(tc.body))
			if format != tc.wantFormat {
				t.Errorf("format = %q, want %q", format, tc.wantFormat)
			}
			if in.Text != tc.wantText {
				t.Errorf("text = %q, want %q", in.Text, tc.wantText)
			}
			if in.HasTools != tc.wantTools {
				t.Errorf("HasTools = %v, want %v", in.HasTools, tc.wantTools)
			}
			if in.HasImages != tc.wantImages {
				t.Errorf("HasImages = %v, want %v", in.HasImages, tc.wantImages)
			}
		})
	}
}

func TestExtractInputUnreadableBodies(t *testing.T) {
	for name, body := range map[string][]byte{
		"empty":       nil,
		"not json":    []byte("{nope"),
		"json array":  []byte(`[1,2]`),
		"too large":   []byte(`{"messages":[{"role":"user","content":"` + strings.Repeat("x", MaxBodyBytes) + `"}]}`),
		"no messages": []byte(`{"model":"auto"}`),
	} {
		in, _ := ExtractInput("/v1/chat/completions", body)
		if in.Text != "" || in.HasTools || in.HasImages {
			t.Errorf("%s: want an empty Input, got %+v", name, in)
		}
	}
}

// The tier of an agent loop must not change from turn to turn: every request
// of the loop, tool results included, classifies on what the person asked.
func TestAgentLoopKeepsTheTier(t *testing.T) {
	c := mustNew(t, DefaultConfig())
	ask := "请详细分析一下这个分布式系统的架构设计，一步一步推理，权衡各方案的利弊"
	first := `{"messages":[{"role":"user","content":"` + ask + `"}]}`
	later := `{"messages":[{"role":"user","content":"` + ask + `"},{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"read_file","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"def f(): pass"}]}]}`
	in1, _ := ExtractInput("/v1/messages", []byte(first))
	in2, _ := ExtractInput("/v1/messages", []byte(later))
	r1, r2 := c.Classify(in1), c.Classify(in2)
	if r1.Tier != TierReasoning || r2.Tier != r1.Tier {
		t.Fatalf("tier changed across the loop: first=%s later=%s", r1.Tier, r2.Tier)
	}
}

func TestEmptyTextUsesTheDefaultTier(t *testing.T) {
	c := mustNew(t, DefaultConfig())
	if r := c.Classify(Input{}); r.Tier != TierMedium || r.Cause != CauseNoText {
		t.Errorf("empty input: want medium/no_text, got %s/%s", r.Tier, r.Cause)
	}
	if r := c.Classify(Input{Text: "   \n"}); r.Cause != CauseNoText {
		t.Errorf("blank input: want no_text, got %s", r.Cause)
	}
	cfg := DefaultConfig()
	cfg.NoSignalTier = TierSimple
	c2 := mustNew(t, cfg)
	if r := c2.Classify(Input{HasImages: true}); r.Tier != TierMedium || r.Cause != CauseMinTier {
		t.Errorf("image only, default simple: want the image floor (medium/min_tier), got %s/%s", r.Tier, r.Cause)
	}
}
