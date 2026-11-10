// Package larknotify sends cards to a Lark / Feishu custom bot and decides when
// a stream of upstream failures is worth a message. It depends on the standard
// library only, so it can be tested on its own.
package larknotify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
)

// Sign returns the signature a Lark custom bot expects when "signature
// verification" is turned on: HMAC-SHA256 keyed by "<timestamp>\n<secret>" over
// an empty message, base64 encoded.
func Sign(secret string, timestamp int64) string {
	key := strconv.FormatInt(timestamp, 10) + "\n" + secret
	return base64.StdEncoding.EncodeToString(hmac.New(sha256.New, []byte(key)).Sum(nil))
}
