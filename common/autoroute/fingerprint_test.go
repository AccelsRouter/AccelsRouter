package autoroute

import "testing"

func fp(t *testing.T, path, body string) string {
	t.Helper()
	return ConversationFingerprint(path, []byte(body))
}

func TestFingerprintIsStableAcrossTurns(t *testing.T) {
	turn1 := `{"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"你好"}]}`
	turn2 := `{"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"你好"},{"role":"assistant","content":"你好！"},{"role":"user","content":"继续"}]}`
	turn3 := `{"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"你好"},{"role":"assistant","content":"你好！"},{"role":"user","content":"继续"},{"role":"assistant","content":"..."},{"role":"user","content":"请详细分析一下这个分布式系统"}]}`
	a, b, c := fp(t, "/v1/chat/completions", turn1), fp(t, "/v1/chat/completions", turn2), fp(t, "/v1/chat/completions", turn3)
	if a == "" || a != b || b != c {
		t.Fatalf("fingerprint must be the same on every turn: %q %q %q", a, b, c)
	}
}

func TestFingerprintSeparatesConversations(t *testing.T) {
	base := fp(t, "/v1/chat/completions", `{"messages":[{"role":"system","content":"S"},{"role":"user","content":"hello"}]}`)
	for name, body := range map[string]string{
		"another opening":       `{"messages":[{"role":"system","content":"S"},{"role":"user","content":"hello there"}]}`,
		"another system prompt": `{"messages":[{"role":"system","content":"T"},{"role":"user","content":"hello"}]}`,
		"no system prompt":      `{"messages":[{"role":"user","content":"hello"}]}`,
	} {
		if got := fp(t, "/v1/chat/completions", body); got == base || got == "" {
			t.Errorf("%s must be a different conversation, got %q vs %q", name, got, base)
		}
	}
	// the developer role counts as the system prompt
	dev := fp(t, "/v1/chat/completions", `{"messages":[{"role":"developer","content":"S"},{"role":"user","content":"hello"}]}`)
	if dev != base {
		t.Error("a developer message is read as the system prompt")
	}
}

func TestFingerprintPerFormat(t *testing.T) {
	// Claude: the system prompt is top-level, a string or blocks; tool results never count as the opening
	claude1 := fp(t, "/v1/messages", `{"system":[{"type":"text","text":"Be brief"}],"messages":[{"role":"user","content":"refactor auth"}]}`)
	claude2 := fp(t, "/v1/messages", `{"system":"Be brief","messages":[{"role":"user","content":"refactor auth"},{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"x","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}]}`)
	if claude1 == "" || claude1 != claude2 {
		t.Errorf("claude: %q vs %q", claude1, claude2)
	}
	// Responses: instructions plus the first user item, string or parts
	r1 := fp(t, "/v1/responses", `{"instructions":"X","input":"hello"}`)
	r2 := fp(t, "/v1/responses", `{"instructions":"X","input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]},{"type":"function_call_output","call_id":"1","output":"..."}]}`)
	if r1 == "" || r1 != r2 {
		t.Errorf("responses: %q vs %q", r1, r2)
	}
	// nothing to identify
	for name, body := range map[string]string{
		"no user text":       `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`,
		"only a system turn": `{"messages":[{"role":"system","content":"S"}]}`,
		"not json":           `{nope`,
		"empty":              ``,
	} {
		if got := fp(t, "/v1/chat/completions", body); got != "" {
			t.Errorf("%s: want no fingerprint, got %q", name, got)
		}
	}
	if got := fp(t, "/v1/completions", `{"prompt":"hello"}`); got != "" {
		t.Errorf("a legacy completion has no conversation: %q", got)
	}
}

func TestExtractRequestMatchesExtractInput(t *testing.T) {
	body := `{"tools":[{"type":"function"}],"messages":[{"role":"system","content":"S"},{"role":"user","content":"你好"}]}`
	in, format := ExtractInput("/v1/chat/completions", []byte(body))
	e := ExtractRequest("/v1/chat/completions", []byte(body))
	if e.Input != in || e.Format != format || e.Fingerprint == "" {
		t.Fatalf("ExtractRequest %+v vs ExtractInput %+v %v", e, in, format)
	}
}
