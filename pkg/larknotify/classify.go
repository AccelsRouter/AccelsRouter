package larknotify

// IsUpstreamFailure reports whether a failed relay attempt says something about
// the upstream channel itself, as opposed to the caller's request or account.
// Only the first kind should count towards an availability alert.
//
// status is the HTTP status the upstream answered with (0 if it never did),
// code is the gateway's error code, channelErr is the gateway's own verdict
// that the error belongs to the channel.
func IsUpstreamFailure(status int, code string, channelErr bool) bool {
	if callerSideCodes[code] {
		return false
	}
	if channelErr {
		return true
	}
	switch code {
	case "bad_response_status_code":
		return upstreamStatus(status)
	case "do_request_failed", "bad_response", "bad_response_body", "empty_response",
		"read_response_body_failed", "aws_invoke_error", "model_not_found":
		return true
	}
	return upstreamStatus(status)
}

// upstreamStatus: server errors, timeouts, rate limiting and rejected or
// unknown credentials or models. A plain 400 is left out on purpose: it is
// usually the caller's request.
func upstreamStatus(status int) bool {
	switch {
	case status >= 500:
		return true
	case status == 401, status == 403, status == 404, status == 408, status == 429:
		return true
	}
	return false
}

// callerSideCodes are gateway error codes that blame the request, the caller's
// account or the gateway's own bookkeeping, never the upstream.
var callerSideCodes = map[string]bool{
	"invalid_request":                true,
	"sensitive_words_detected":       true,
	"violation_fee.grok.csam":        true,
	"count_token_failed":             true,
	"model_price_error":              true,
	"invalid_api_type":               true,
	"json_marshal_failed":            true,
	"gen_relay_info_failed":          true,
	"read_request_body_failed":       true,
	"convert_request_failed":         true,
	"access_denied":                  true,
	"bad_request_body":               true,
	"prompt_blocked":                 true,
	"query_data_error":               true,
	"update_data_error":              true,
	"insufficient_user_quota":        true,
	"pre_consume_token_quota_failed": true,
}
