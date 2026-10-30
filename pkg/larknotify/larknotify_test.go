package larknotify

import (
	"context"
	"encoding/json" // test fixtures only: production code uses the injected codec
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSignMatchesKnownVector(t *testing.T) {
	// computed independently with Python's hmac
	if got := Sign("xxx", 1599360473); got != "JDBzM0/Lgm4EoMB1Z0UHv+cKRCWFryI7HKNFin4AIVU=" {
		t.Fatalf("signature = %q", got)
	}
}

func TestCardPayload(t *testing.T) {
	p := Card{
		Title: "t", Color: "red",
		Fields: []Field{{"渠道", "#3 openai"}, {"最近错误", strings.Repeat("x", 800)}},
		Note:   "note",
	}.Payload()
	raw, _ := json.Marshal(p)
	s := string(raw)
	for _, want := range []string{`"template":"red"`, `"content":"t"`, `**渠道**\n#3 openai`, `"is_short":true`, `"is_short":false`, `"tag":"note"`} {
		if !strings.Contains(s, want) {
			t.Errorf("payload lacks %s: %s", want, s)
		}
	}
	if strings.Contains(s, strings.Repeat("x", 700)) {
		t.Error("a long value must be truncated")
	}
	if got := (Card{Title: "t"}).Payload()["header"].(map[string]any)["template"]; got != "blue" {
		t.Errorf("default colour = %v", got)
	}
	if n := len((Card{Title: "t"}).Payload()["elements"].([]any)); n != 0 {
		t.Errorf("a card without fields or note has %d elements", n)
	}
}

func TestTruncateIsRuneSafe(t *testing.T) {
	if got := Truncate("你好世界", 2); got != "你好…" {
		t.Errorf("got %q", got)
	}
	if got := Truncate("abc", 3); got != "abc" {
		t.Errorf("got %q", got)
	}
}

func codecSender() Sender { return Sender{Marshal: json.Marshal, Unmarshal: json.Unmarshal} }

func TestSendNeedsACodec(t *testing.T) {
	if err := (Sender{}).Send(context.Background(), Target{WebhookURL: "http://127.0.0.1:1"}, Card{}); err == nil || !strings.Contains(err.Error(), "codec") {
		t.Errorf("err = %v", err)
	}
}

func serve(t *testing.T, status int, body string, got *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if got != nil {
			_ = json.Unmarshal(raw, got)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestSendSignsAndChecksTheVerdict(t *testing.T) {
	fixed := time.Unix(1599360473, 0)
	s := Sender{Now: func() time.Time { return fixed }, Marshal: json.Marshal, Unmarshal: json.Unmarshal}

	var got map[string]any
	ok := serve(t, 200, `{"code":0,"msg":"success"}`, &got)
	defer ok.Close()
	if err := s.Send(context.Background(), Target{ok.URL, "xxx"}, Card{Title: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got["msg_type"] != "interactive" || got["timestamp"] != "1599360473" || got["sign"] != Sign("xxx", 1599360473) {
		t.Errorf("message = %v", got)
	}

	// without a secret there is no signature
	got = nil
	if err := s.Send(context.Background(), Target{WebhookURL: ok.URL}, Card{Title: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, has := got["sign"]; has {
		t.Error("an unsigned target must not send a signature")
	}

	for name, c := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"lark refuses with 200": {200, `{"code":19021,"msg":"sign match fail"}`, "19021"},
		"old style refusal":     {200, `{"StatusCode":9499,"StatusMessage":"bad"}`, "9499"},
		"http error":            {500, `boom`, "HTTP 500"},
	} {
		srv := serve(t, c.status, c.body, nil)
		err := s.Send(context.Background(), Target{WebhookURL: srv.URL}, Card{Title: "x"})
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, c.want)
		}
	}
	// an answer that is not JSON at all is accepted when the status is fine
	srv := serve(t, 200, `ok`, nil)
	defer srv.Close()
	if err := s.Send(context.Background(), Target{WebhookURL: srv.URL}, Card{Title: "x"}); err != nil {
		t.Errorf("plain 200 should pass: %v", err)
	}
}

func TestSendNeverLeaksTheWebhookAddress(t *testing.T) {
	srv := serve(t, 200, `{}`, nil)
	url := srv.URL + "/open-apis/bot/v2/hook/SECRET-TOKEN"
	srv.Close() // now the connection is refused and the transport error names the URL
	err := codecSender().Send(context.Background(), Target{WebhookURL: url}, Card{Title: "x"})
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Errorf("error leaks the webhook: %v", err)
	}
	if err := codecSender().Send(context.Background(), Target{}, Card{}); err == nil {
		t.Error("an empty target must be an error")
	}
	if err := codecSender().Send(context.Background(), Target{WebhookURL: "://bad"}, Card{}); err == nil || strings.Contains(err.Error(), "://bad") {
		t.Errorf("a malformed address must be rejected without echoing it: %v", err)
	}
}

func TestIsUpstreamFailure(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		code   string
		chErr  bool
		want   bool
	}{
		{"upstream 500", 500, "bad_response_status_code", false, true},
		{"upstream 502", 502, "bad_response_status_code", false, true},
		{"upstream 429", 429, "bad_response_status_code", false, true},
		{"upstream 401, key rejected", 401, "bad_response_status_code", false, true},
		{"upstream 400 is the caller's request", 400, "bad_response_status_code", false, false},
		{"upstream 422", 422, "bad_response_status_code", false, false},
		{"network failure", 0, "do_request_failed", false, true},
		{"empty answer", 200, "empty_response", false, true},
		{"channel error", 0, "channel:invalid_key", true, true},
		{"caller's invalid request, even with 500", 500, "invalid_request", false, false},
		{"caller out of quota", 403, "insufficient_user_quota", false, false},
		{"sensitive words", 400, "sensitive_words_detected", false, false},
		{"flagged channel error but the caller's fault", 400, "bad_request_body", true, false},
		{"unknown code, server error", 503, "something_new", false, true},
		{"unknown code, 400", 400, "something_new", false, false},
	} {
		if got := IsUpstreamFailure(c.status, c.code, c.chErr); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func sample(ch int, model string, user int, at time.Time) Sample {
	return Sample{
		Channel: Channel{ID: ch, Name: "ch", Type: "OpenAI"}, Model: model,
		Caller:     Caller{UserID: user, Username: "u", TokenName: "k"},
		StatusCode: 502, ErrorCode: "bad_response_status_code", Message: "boom", At: at,
	}
}

func newTracker() *Tracker {
	return NewTracker(func() Config { return Config{Window: 5 * time.Minute, Threshold: 3, Mute: 10 * time.Minute} })
}

func TestThreeFailuresInFiveMinutesThenQuietForTen(t *testing.T) {
	tr := newTracker()
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	min := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }

	for i, m := range []int{0, 1} {
		if _, fire := tr.Record(sample(1, "gpt", 7, min(m))); fire {
			t.Fatalf("failure %d must not alert yet", i+1)
		}
	}
	a, fire := tr.Record(sample(1, "gpt", 8, min(2)))
	if !fire {
		t.Fatal("the third failure within five minutes must alert")
	}
	if a.Count != 3 || a.Channel.ID != 1 || a.Model != "gpt" || a.TotalCallers != 2 || len(a.Callers) != 2 {
		t.Errorf("alert = %+v", a)
	}
	if a.Callers[0].UserID != 8 {
		t.Errorf("callers must be newest first: %+v", a.Callers)
	}

	// muted for ten minutes, however many failures follow
	for _, m := range []int{3, 4, 5, 6, 7, 8, 9, 11} {
		if _, fire := tr.Record(sample(1, "gpt", 7, min(m))); fire {
			t.Fatalf("minute %d is inside the mute", m)
		}
	}
	// the mute ended at minute 12, and the failures of the last five minutes still count
	if _, fire := tr.Record(sample(1, "gpt", 7, min(12))); !fire {
		t.Error("a channel that keeps failing must alert again once the mute is over")
	}
}

func TestFailuresOutsideTheWindowDoNotAdd(t *testing.T) {
	tr := newTracker()
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, m := range []int{0, 3, 6, 9, 12} { // never three within five minutes
		if _, fire := tr.Record(sample(1, "gpt", 7, t0.Add(time.Duration(m)*time.Minute))); fire {
			t.Fatalf("minute %d alerted although the failures are spread out", m)
		}
	}
}

func TestChannelsAndModelsAreCountedSeparately(t *testing.T) {
	tr := newTracker()
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		tr.Record(sample(1, "gpt", 7, t0))
		tr.Record(sample(2, "gpt", 7, t0))
		tr.Record(sample(1, "claude", 7, t0))
	}
	if _, fire := tr.Record(sample(1, "gpt", 7, t0)); !fire {
		t.Error("channel 1 + gpt has three failures")
	}
	if _, fire := tr.Record(sample(2, "claude", 7, t0)); fire {
		t.Error("channel 2 + claude has one failure")
	}
}

func TestDisabledOrBadConfigNeverAlerts(t *testing.T) {
	for _, cfg := range []Config{{}, {Window: time.Minute}, {Threshold: 3}, {Window: -time.Minute, Threshold: 3}} {
		cfg := cfg
		tr := NewTracker(func() Config { return cfg })
		for i := 0; i < 10; i++ {
			if _, fire := tr.Record(sample(1, "gpt", 7, time.Now())); fire {
				t.Fatalf("config %+v must never alert", cfg)
			}
		}
	}
}

func TestSettingsAreReadOnEveryCall(t *testing.T) {
	th := 5
	tr := NewTracker(func() Config { return Config{Window: time.Minute, Threshold: th, Mute: time.Minute} })
	now := time.Now()
	for i := 0; i < 3; i++ {
		tr.Record(sample(1, "gpt", 7, now))
	}
	th = 3 // changed in the admin page
	if _, fire := tr.Record(sample(1, "gpt", 7, now)); !fire {
		t.Error("a lowered threshold must apply to the next failure")
	}
}

func TestCallersAreDistinctAndCapped(t *testing.T) {
	tr := NewTracker(func() Config { return Config{Window: time.Hour, Threshold: 20, Mute: time.Hour} })
	now := time.Now()
	var a Alert
	var fire bool
	for i := 0; i < 20; i++ {
		a, fire = tr.Record(sample(1, "gpt", i%8, now.Add(time.Duration(i)*time.Second)))
	}
	if !fire || a.Count != 20 {
		t.Fatalf("alert = %+v fire=%v", a, fire)
	}
	if a.TotalCallers != 8 || len(a.Callers) != MaxCallers {
		t.Errorf("TotalCallers=%d shown=%d", a.TotalCallers, len(a.Callers))
	}
}

func TestIdleEntriesAreForgotten(t *testing.T) {
	tr := newTracker()
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for ch := 1; ch <= 50; ch++ {
		tr.Record(sample(ch, "gpt", 7, t0))
	}
	tr.Record(sample(999, "gpt", 7, t0.Add(30*time.Minute)))
	tr.mu.Lock()
	n := len(tr.entries)
	tr.mu.Unlock()
	if n != 1 {
		t.Errorf("%d entries kept after half an hour of silence", n)
	}
}

// fakeTr renders a key with its params in a fixed order, so a test can see which message was asked for.
func fakeTr(key string, params map[string]any) string {
	if len(params) == 0 {
		return key
	}
	names := make([]string, 0, len(params))
	for k := range params {
		names = append(names, k)
	}
	slices.Sort(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, fmt.Sprintf("%s=%v", k, params[k]))
	}
	return key + "[" + strings.Join(parts, ",") + "]"
}

func TestAlertCardNamesChannelModelAndCallers(t *testing.T) {
	a := Alert{
		Channel: Channel{ID: 12, Name: "sectob-gpt", Type: "OpenAI"},
		Model:   "gpt-6-sol", Count: 3, Window: 5 * time.Minute, Mute: 10 * time.Minute,
		Callers: []Caller{
			{UserID: 8, Username: "alice", TokenName: "prod", Group: "default"},
			{UserID: 0, Username: "", TokenName: "k2"},
		},
		TotalCallers: 4,
		LastAt:       time.Date(2026, 10, 28, 12, 3, 0, 0, time.UTC),
		Latest:       Sample{StatusCode: 502, ErrorCode: "bad_response_status_code", Message: strings.Repeat("e", 500), RequestID: "req-1"},
	}
	card := AlertCard(a, fakeTr)
	raw, _ := json.Marshal(card.Payload())
	s := string(raw)
	for _, want := range []string{
		MsgAlertTitle, "#12 sectob-gpt (OpenAI)", "gpt-6-sol",
		MsgAlertFailuresValue + "[Count=3,Minutes=5]",
		"502 · bad_response_status_code",
		"alice (ID 8) · " + MsgAlertCallerToken + "[Name=prod] · " + MsgAlertCallerGroup + "[Name=default]",
		MsgAlertUnknownUser + " · " + MsgAlertCallerToken + "[Name=k2]",
		MsgAlertMoreCallers + "[Count=2]",
		"req-1", MsgAlertNote + "[Minutes=10,Time=2026-10-28 12:03:00 UTC]", `"template":"red"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("card lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, strings.Repeat("e", 400)) {
		t.Error("the error text must be shortened")
	}

	// empty values leave their field out
	bare := AlertCard(Alert{Channel: Channel{ID: 1, Name: "c"}, Count: 3, Window: time.Minute, Mute: time.Minute}, fakeTr)
	for _, f := range bare.Fields {
		if strings.TrimSpace(f.Value) == "" {
			t.Errorf("field %q is empty", f.Name)
		}
	}
}

func TestTestCard(t *testing.T) {
	c := TestCard(MsgTestPurposeAlert, fakeTr)
	if c.Title != MsgTestTitle || len(c.Fields) != 2 || c.Fields[0].Value != MsgTestPurposeAlert {
		t.Errorf("card = %+v", c)
	}
}

func usageCfg() UsageConfig {
	return UsageConfig{Multiplier: 3, BaselineDays: 7, MinSpend: 10}
}

func kinds(events []UsageEvent) []UsageKind {
	var k []UsageKind
	for _, e := range events {
		k = append(k, e.Kind)
	}
	return k
}

var utcDay = time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)

func TestUsageSpikeAgainstTheUsersOwnAverage(t *testing.T) {
	cfg := UsageConfig{Multiplier: 3, BaselineDays: 7, MinSpend: 1} // average below is 14 / 7 = 2 a day
	for _, c := range []struct {
		name  string
		today float64
		want  bool
	}{
		{"exactly three times the average is not a spike", 6, false},
		{"just above three times", 6.01, true},
		{"below the minimum, however large the multiple", 0.9, false},
		{"normal day", 2, false},
	} {
		got := EvaluateUsage(UserSpend{UserID: 1, Today: c.today, Month: 50, Baseline: 14}, cfg, utcDay)
		if (len(got) == 1 && got[0].Kind == UsageAnomaly) != c.want {
			t.Errorf("%s: events = %v, want spike %v", c.name, kinds(got), c.want)
		}
	}
	got := EvaluateUsage(UserSpend{UserID: 1, Today: 7, Month: 50, Baseline: 14}, cfg, utcDay)
	if len(got) != 1 || got[0].Average != 2 || got[0].NoHistory {
		t.Errorf("spike = %+v", got)
	}
}

func TestUsageNewUserSpikesAsSoonAsTheyReachTheMinimum(t *testing.T) {
	got := EvaluateUsage(UserSpend{UserID: 9, Today: 10, Month: 10}, usageCfg(), utcDay)
	if len(got) != 1 || got[0].Kind != UsageAnomaly || !got[0].NoHistory {
		t.Errorf("events = %+v", got)
	}
	if got := EvaluateUsage(UserSpend{UserID: 9, Today: 9.99, Month: 9.99}, usageCfg(), utcDay); len(got) != 0 {
		t.Errorf("below the minimum must not alert: %+v", got)
	}
}

func TestUsageDaysWithoutSpendCountInTheAverage(t *testing.T) {
	// $7 over seven days is $1 a day, not $7 a day: $10 is ten times that
	got := EvaluateUsage(UserSpend{UserID: 2, Today: 10, Month: 17, Baseline: 7}, usageCfg(), utcDay)
	if len(got) != 1 || got[0].Average != 1 {
		t.Errorf("events = %+v", got)
	}
}

func TestUsageLimitsAreOffByDefaultAndOnWhenSet(t *testing.T) {
	u := UserSpend{UserID: 3, Today: 120, Month: 1600, Baseline: 700} // average 100: 120 is no spike
	if got := EvaluateUsage(u, usageCfg(), utcDay); len(got) != 0 {
		t.Errorf("no limits set, no spike: %v", kinds(got))
	}
	cfg := usageCfg()
	cfg.DailyThreshold, cfg.MonthlyThreshold = 100, 1500
	got := EvaluateUsage(u, cfg, utcDay)
	if !slices.Equal(kinds(got), []UsageKind{UsageDaily, UsageMonthly}) {
		t.Fatalf("events = %v", kinds(got))
	}
	if got[0].Key != "usage_alert:daily:3:2026-10-28" || got[1].Key != "usage_alert:monthly:3:2026-10" {
		t.Errorf("keys = %q %q", got[0].Key, got[1].Key)
	}
	if got[0].TTL != 24*time.Hour || got[1].TTL != 32*24*time.Hour {
		t.Errorf("ttl = %v %v", got[0].TTL, got[1].TTL)
	}
	// the limits are reached, not just exceeded
	if got := EvaluateUsage(UserSpend{UserID: 3, Today: 100, Month: 100}, UsageConfig{Multiplier: 3, BaselineDays: 7, MinSpend: 1000, DailyThreshold: 100}, utcDay); len(got) != 1 {
		t.Errorf("a spend equal to the limit must alert: %v", kinds(got))
	}
}

func TestUsageKeysChangeWithTheDayAndAreStablePerUser(t *testing.T) {
	u := UserSpend{UserID: 4, Today: 50, Month: 50}
	a := EvaluateUsage(u, usageCfg(), utcDay)
	b := EvaluateUsage(u, usageCfg(), utcDay.Add(5*time.Hour)) // the same day later on is the same key
	c := EvaluateUsage(u, usageCfg(), utcDay.AddDate(0, 0, 1))
	if a[0].Key != b[0].Key || a[0].Key == c[0].Key {
		t.Errorf("keys: %q %q %q", a[0].Key, b[0].Key, c[0].Key)
	}
	if got := EvaluateUsage(UserSpend{UserID: 5, Today: 50, Month: 0}, usageCfg(), utcDay); len(got) != 0 {
		t.Error("a user with no spend this month is not evaluated")
	}
	if got := EvaluateUsage(u, UsageConfig{Multiplier: 3, MinSpend: 10}, utcDay); len(got) != 1 {
		t.Error("a missing baseline length must not divide by zero")
	}
}

func TestUsageCard(t *testing.T) {
	cfg := usageCfg()
	events := []UsageEvent{
		{Kind: UsageMonthly, User: UserSpend{UserID: 3, Username: "carol", Month: 1600}, Threshold: 1500},
		{Kind: UsageAnomaly, User: UserSpend{UserID: 1, Username: "alice", Today: 23.4}, Average: 2.1},
		{Kind: UsageAnomaly, User: UserSpend{UserID: 2, Username: "", Today: 55}, NoHistory: true},
		{Kind: UsageDaily, User: UserSpend{UserID: 3, Username: "carol", Today: 120}, Threshold: 100},
	}
	card := UsageCard(events, time.Date(2026, 10, 28, 13, 0, 0, 0, time.UTC), cfg, fakeTr)
	if card.Title != MsgUsageTitle || card.Color != "orange" || len(card.Fields) != 4 {
		t.Fatalf("card = %+v", card)
	}
	wantNames := []string{
		MsgUsageKindAnomaly + " · " + MsgAlertUnknownUser + " (ID 2)", // the bigger spender first
		MsgUsageKindAnomaly + " · alice (ID 1)",
		MsgUsageKindDaily + " · carol (ID 3)",
		MsgUsageKindMonthly + " · carol (ID 3)",
	}
	for i, f := range card.Fields {
		if f.Name != wantNames[i] {
			t.Errorf("field %d = %q, want %q", i, f.Name, wantNames[i])
		}
	}
	wantValues := []string{
		MsgUsageNewUserValue + "[Days=7,MinSpend=10.00,Today=55.00]",
		MsgUsageAnomalyValue + "[Average=2.10,Days=7,MinSpend=10.00,Multiplier=3,Today=23.40]",
		MsgUsageDailyValue + "[Spend=120.00,Threshold=100.00]",
		MsgUsageMonthlyValue + "[Spend=1600.00,Threshold=1500.00]",
	}
	for i, f := range card.Fields {
		if f.Value != wantValues[i] {
			t.Errorf("value %d = %q, want %q", i, f.Value, wantValues[i])
		}
	}
	if card.Note != MsgUsageNote+"[Time=2026-10-28 13:00:00,Zone=UTC]" {
		t.Errorf("note = %q", card.Note)
	}

	// a long list is cut and counted
	var many []UsageEvent
	for i := 1; i <= MaxUsageEvents+5; i++ {
		many = append(many, UsageEvent{Kind: UsageAnomaly, User: UserSpend{UserID: i, Username: "u", Today: float64(i)}})
	}
	big := UsageCard(many, time.Now(), cfg, fakeTr)
	if len(big.Fields) != MaxUsageEvents+1 || big.Fields[MaxUsageEvents].Value != MsgUsageMore+"[Count=5]" {
		t.Errorf("fields = %d, last = %+v", len(big.Fields), big.Fields[len(big.Fields)-1])
	}
}

func TestUsageDaysFollowTheTimeZoneOfToday(t *testing.T) {
	plus8 := time.FixedZone("x", 8*3600)
	today := time.Date(2026, 10, 28, 0, 0, 0, 0, plus8) // 2026-10-27 16:00 UTC
	got := EvaluateUsage(UserSpend{UserID: 6, Today: 50, Month: 50}, usageCfg(), today)
	if len(got) != 1 || got[0].Key != "usage_alert:anomaly:6:2026-10-28" {
		t.Errorf("the key must carry the local date: %+v", got)
	}
	cfg := usageCfg()
	cfg.MonthlyThreshold = 10
	for _, e := range EvaluateUsage(UserSpend{UserID: 6, Today: 50, Month: 50}, cfg, time.Date(2026, 11, 1, 0, 0, 0, 0, plus8)) {
		if e.Kind == UsageMonthly && e.Key != "usage_alert:monthly:6:2026-11" {
			t.Errorf("monthly key = %q", e.Key)
		}
	}
	for _, c := range []struct {
		zone *time.Location
		want string
	}{{plus8, "UTC+8"}, {time.FixedZone("y", -5*3600), "UTC-5"}, {time.UTC, "UTC"}} {
		card := UsageCard(nil, time.Date(2026, 10, 28, 13, 0, 0, 0, c.zone), usageCfg(), fakeTr)
		if !strings.Contains(card.Note, "Zone="+c.want+"]") || !strings.Contains(card.Note, "Time=2026-10-28 13:00:00") {
			t.Errorf("note = %q, want zone %s and the local time", card.Note, c.want)
		}
	}
}
