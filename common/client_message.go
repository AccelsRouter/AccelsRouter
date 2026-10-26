package common

import (
	"regexp"
	"strings"
)

// Fork: what an API client receives in an error message is the upstream's
// reason without the upstream's identity. Administrators keep the full text in
// the error log, which is recorded before the client response is built.
var (
	// A request id appended by another New API (or a vendor) to its message,
	// such as " (request id: 2026…)" or "（请求 ID：…）". Ours is appended after
	// this runs.
	clientMessageRequestIdPattern = regexp.MustCompile(`(?i)\s*[(（]\s*(?:request[ _-]?id|请求 ?id|請求 ?id)\s*[:：]?\s*[^)）]*[)）]`)
	// "request ID req_…" inside a vendor sentence.
	clientMessageInlineRequestIdPattern = regexp.MustCompile(`(?i)[\s,;]*\(?\brequest[ _-]?id[:：]?\s*req_[A-Za-z0-9]+\)?`)
	// The routing group of the gateway that produced the message: ours (for
	// example a reseller's private group) or an upstream New API's.
	clientMessageGroupPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\s+under group\s+[^\s:：,，;；()（）]+`),
		regexp.MustCompile(`(?:分组|分組)\s*[^\s:：,，;；()（）]+\s*下\s*`),
	}
	// Internal stage markers of the channel selection messages.
	clientMessageStagePattern = regexp.MustCompile(`\s*[(（](?:distributor|retry)[)）]`)
	// A removal can leave a space before punctuation ("model m :").
	clientMessagePunctuationPattern = regexp.MustCompile(`\s+([:：,，;；)）])`)
)

// ClientFacingMessage strips vendor and routing identifiers from an error
// message before it is returned to an API client: upstream request ids, the
// routing group name and the internal stage markers. The error reason itself
// is kept.
func ClientFacingMessage(message string) string {
	cleaned := clientMessageRequestIdPattern.ReplaceAllString(message, "")
	cleaned = clientMessageInlineRequestIdPattern.ReplaceAllString(cleaned, "")
	for _, pattern := range clientMessageGroupPatterns {
		cleaned = pattern.ReplaceAllString(cleaned, " ")
	}
	cleaned = clientMessageStagePattern.ReplaceAllString(cleaned, "")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	cleaned = clientMessagePunctuationPattern.ReplaceAllString(cleaned, "$1")
	if cleaned == "" {
		return strings.TrimSpace(message)
	}
	return cleaned
}
