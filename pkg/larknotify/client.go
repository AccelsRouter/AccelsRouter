package larknotify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Target is one bot: its webhook URL (which is a secret) and, when the bot has
// signature verification on, the signing secret.
type Target struct {
	WebhookURL string
	Secret     string
}

// Configured reports whether there is somewhere to send to.
func (t Target) Configured() bool { return strings.TrimSpace(t.WebhookURL) != "" }

// Sender posts messages to a Target. Marshal and Unmarshal are required: the
// project does all JSON through the wrappers of its common package, so they are
// handed in rather than imported here.
type Sender struct {
	HTTP      *http.Client     // defaults to a client with an 8 second timeout
	Now       func() time.Time // defaults to time.Now; set in tests
	Marshal   func(any) ([]byte, error)
	Unmarshal func([]byte, any) error
}

// Send posts a Card.
func (s Sender) Send(ctx context.Context, t Target, card Card) error {
	return s.SendRaw(ctx, t, card.Payload())
}

// SendRaw posts an already built card object (the "card" part of an
// interactive message), signing it when the target has a secret.
func (s Sender) SendRaw(ctx context.Context, t Target, card map[string]any) error {
	hook := strings.TrimSpace(t.WebhookURL)
	if hook == "" {
		return errors.New("lark webhook is not configured")
	}
	if s.Marshal == nil || s.Unmarshal == nil {
		return errors.New("lark sender has no JSON codec")
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	msg := map[string]any{"msg_type": "interactive", "card": card}
	if secret := strings.TrimSpace(t.Secret); secret != "" {
		ts := now().Unix()
		msg["timestamp"] = strconv.FormatInt(ts, 10)
		msg["sign"] = Sign(secret, ts)
	}
	body, err := s.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encode lark message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook, bytes.NewReader(body))
	if err != nil {
		return errors.New("lark webhook address is not a valid URL")
	}
	req.Header.Set("Content-Type", "application/json")
	hc := s.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		// The transport error names the URL, which holds a secret: it must not reach logs or API responses.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return errors.New(strings.ReplaceAll(err.Error(), hook, "<webhook>"))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("lark returned HTTP %d: %s", resp.StatusCode, Truncate(strings.TrimSpace(string(raw)), 300))
	}
	// Lark answers 200 even when it refuses a message (wrong signature,
	// keyword mismatch, IP not allowed); the verdict is in the body.
	var verdict struct {
		Code       *int   `json:"code"`
		Msg        string `json:"msg"`
		StatusCode *int   `json:"StatusCode"`
		StatusMsg  string `json:"StatusMessage"`
	}
	if s.Unmarshal(raw, &verdict) == nil {
		switch {
		case verdict.Code != nil && *verdict.Code != 0:
			return fmt.Errorf("lark rejected the message: code %d, %s", *verdict.Code, verdict.Msg)
		case verdict.StatusCode != nil && *verdict.StatusCode != 0:
			return fmt.Errorf("lark rejected the message: code %d, %s", *verdict.StatusCode, verdict.StatusMsg)
		}
	}
	return nil
}
