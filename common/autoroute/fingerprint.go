package autoroute

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// ConversationFingerprint identifies the conversation a request belongs to,
// for clients that do not say which one it is.
//
// Chat clients resend the whole history with every request, so everything a
// conversation sends begins the same way: the same system prompt and the same
// first user message. A hash of those two is therefore the same on every turn
// of a conversation and different between conversations. It is empty when the
// request has no user message with text, or is a legacy completion (a prompt
// has no conversation). The system prompt is read from where each format
// keeps it: the "system" field of a Claude request, "instructions" of a
// Responses request, a system or developer message of an OpenAI one.
//
// A client that rewrites the start of the history between turns, for example
// by dropping old messages to fit a context window or by putting a changing
// timestamp in the system prompt, gets a different fingerprint, which simply
// means it is treated as a new conversation.
func ConversationFingerprint(path string, body []byte) string {
	return ExtractRequest(path, body).Fingerprint
}

func fingerprintOf(format Format, p *requestProbe) string {
	var system, firstUser string
	switch format {
	case FormatMessages:
		system, firstUser = openingOfMessages(p.Messages, p.System)
	case FormatResponses:
		system, firstUser = openingOfResponses(p.Input, p.Instructions)
	}
	if firstUser == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("s\\x1f" + system + "\\x1eu\\x1f" + firstUser))
	return hex.EncodeToString(sum[:16])
}

func isSystemRole(role string) bool { return role == "system" || role == "developer" }

func openingOfMessages(rawMessages, rawSystem json.RawMessage) (system, firstUser string) {
	parts := []string{}
	if t, _ := contentText(rawSystem); t != "" {
		parts = append(parts, t)
	}
	var messages []chatMessage
	if json.Unmarshal(bytes.TrimSpace(rawMessages), &messages) != nil {
		return strings.Join(parts, "\\n"), ""
	}
	systemSeen := false
	for _, m := range messages {
		text, _ := contentText(m.Content)
		switch {
		case isSystemRole(m.Role) && !systemSeen && text != "":
			parts = append(parts, text)
			systemSeen = true
		case m.Role == "user" && text != "":
			return strings.Join(parts, "\\n"), text
		}
	}
	return strings.Join(parts, "\\n"), ""
}

func openingOfResponses(rawInput, rawInstructions json.RawMessage) (system, firstUser string) {
	parts := []string{}
	if t, _ := contentText(rawInstructions); t != "" {
		parts = append(parts, t)
	}
	raw := bytes.TrimSpace(rawInput)
	if len(raw) == 0 {
		return strings.Join(parts, "\\n"), ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return strings.Join(parts, "\\n"), strings.TrimSpace(s)
		}
		return strings.Join(parts, "\\n"), ""
	}
	var items []responsesItem
	if json.Unmarshal(raw, &items) != nil {
		return strings.Join(parts, "\\n"), ""
	}
	systemSeen := false
	for _, it := range items {
		text, _ := contentText(it.Content)
		switch {
		case isSystemRole(it.Role) && !systemSeen && text != "":
			parts = append(parts, text)
			systemSeen = true
		case (it.Role == "user" || (it.Role == "" && it.Type == "message")) && text != "":
			return strings.Join(parts, "\\n"), text
		}
	}
	return strings.Join(parts, "\\n"), ""
}
