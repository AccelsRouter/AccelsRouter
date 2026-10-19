package autoroute

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Format is the shape of a request body, as far as reading the user's text
// is concerned.
type Format string

const (
	FormatUnknown     Format = ""
	FormatMessages    Format = "messages"    // OpenAI chat completions and Claude /v1/messages
	FormatResponses   Format = "responses"   // OpenAI Responses
	FormatCompletions Format = "completions" // legacy OpenAI completions
)

// MaxBodyBytes bounds what ExtractInput will read. A larger body is treated
// as unreadable (no text), which classifies as the default tier, instead of
// parsing megabytes of inlined images on every request.
const MaxBodyBytes = 16 << 20

type requestProbe struct {
	Messages  json.RawMessage `json:"messages"`
	Input     json.RawMessage `json:"input"`
	Prompt    json.RawMessage `json:"prompt"`
	Tools     json.RawMessage `json:"tools"`
	Functions json.RawMessage `json:"functions"`
}

// DetectFormat names the body's shape. The URL path decides when it can;
// the body's fields are the fallback. Guessing from incidental fields (a
// max_tokens, say) misreads converted requests, so the body is consulted
// only for what the path leaves open.
func DetectFormat(path string, body []byte) Format {
	var probe requestProbe
	if len(body) == 0 || len(body) > MaxBodyBytes || json.Unmarshal(body, &probe) != nil {
		return FormatUnknown
	}
	return detectFormat(path, &probe)
}

func detectFormat(path string, p *requestProbe) Format {
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "/responses"):
		return FormatResponses
	case strings.HasSuffix(strings.TrimRight(lower, "/"), "/messages"):
		return FormatMessages
	case strings.Contains(lower, "/chat/completions"):
		if present(p.Input) && !present(p.Messages) {
			return FormatResponses
		}
		return FormatMessages
	case strings.HasSuffix(strings.TrimRight(lower, "/"), "/completions"):
		return FormatCompletions
	}
	switch {
	case present(p.Messages):
		return FormatMessages
	case present(p.Input):
		return FormatResponses
	case present(p.Prompt):
		return FormatCompletions
	}
	return FormatUnknown
}

// ExtractInput reads what the classifier needs from a request body: the text
// to classify, whether the request carries tools, and whether it carries
// images.
//
// The text is the latest user message that has any, which is not always the
// last user message. In an agent loop (Claude Code and the like) a Claude
// request ends with a user message that only holds tool results, and the
// request's difficulty is set by what the person asked earlier, not by the
// tool output. So user messages without text are skipped, and the text of a
// tool result is never read: a file or a command's output would hit a pile of
// code keywords and classify the file, not the request. The system prompt is
// not read either.
func ExtractInput(path string, body []byte) (Input, Format) {
	var probe requestProbe
	if len(body) == 0 || len(body) > MaxBodyBytes || json.Unmarshal(body, &probe) != nil {
		return Input{}, FormatUnknown
	}
	format := detectFormat(path, &probe)
	in := Input{HasTools: nonEmptyArray(probe.Tools) || nonEmptyArray(probe.Functions)}
	switch format {
	case FormatMessages:
		in.Text, in.HasImages = fromMessages(probe.Messages)
	case FormatResponses:
		in.Text, in.HasImages = fromResponsesInput(probe.Input)
	case FormatCompletions:
		in.Text = fromPrompt(probe.Prompt)
	}
	return in, format
}

type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// contentText returns the text of a message's content, which is a string or
// a list of parts, and whether any part is an image. Only text parts count
// (a part with no type is read as text, as some clients omit it).
func contentText(raw json.RawMessage) (text string, hasImage bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false
	}
	switch raw[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return strings.TrimSpace(s), false
		}
	case '[':
		var parts []contentPart
		if json.Unmarshal(raw, &parts) != nil {
			return "", false
		}
		var texts []string
		for _, p := range parts {
			switch p.Type {
			case "", "text", "input_text":
				if t := strings.TrimSpace(p.Text); t != "" {
					texts = append(texts, t)
				}
			case "image", "image_url", "input_image":
				hasImage = true
			}
		}
		return strings.Join(texts, "\n"), hasImage
	}
	return "", false
}

type chatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func fromMessages(raw json.RawMessage) (text string, hasImages bool) {
	var messages []chatMessage
	if json.Unmarshal(bytes.TrimSpace(raw), &messages) != nil {
		return "", false
	}
	for i := len(messages) - 1; i >= 0; i-- {
		t, img := contentText(messages[i].Content)
		hasImages = hasImages || img
		if text == "" && messages[i].Role == "user" && t != "" {
			text = t
		}
	}
	return text, hasImages
}

type responsesItem struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func fromResponsesInput(raw json.RawMessage) (text string, hasImages bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return strings.TrimSpace(s), false
		}
		return "", false
	}
	var items []responsesItem
	if json.Unmarshal(raw, &items) != nil {
		return "", false
	}
	for i := len(items) - 1; i >= 0; i-- {
		t, img := contentText(items[i].Content)
		hasImages = hasImages || img
		isUser := items[i].Role == "user" || (items[i].Role == "" && items[i].Type == "message")
		if text == "" && isUser && t != "" {
			text = t
		}
	}
	return text, hasImages
}

func fromPrompt(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	switch raw[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return strings.TrimSpace(s)
		}
	case '[':
		var list []string
		if json.Unmarshal(raw, &list) == nil {
			return strings.TrimSpace(strings.Join(list, "\n"))
		}
	}
	return ""
}

func present(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && !bytes.Equal(t, []byte("null"))
}

func nonEmptyArray(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 2 && t[0] == '['
}
