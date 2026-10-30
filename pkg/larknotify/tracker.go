package larknotify

import (
	"fmt"
	"sync"
	"time"
)

// Config says when failures turn into an alert.
type Config struct {
	Window    time.Duration // failures are counted within this long
	Threshold int           // this many failures in the window raise an alert
	Mute      time.Duration // after an alert, stay quiet for this long
}

func (c Config) enabled() bool { return c.Window > 0 && c.Threshold > 0 }

// Caller is who made the failed request.
type Caller struct {
	UserID    int
	Username  string
	TokenName string
	Group     string
}

// Channel is the upstream channel that failed.
type Channel struct {
	ID   int
	Name string
	Type string // readable channel type, e.g. "OpenAI"
}

// Sample is one failed attempt.
type Sample struct {
	Channel    Channel
	Model      string
	Caller     Caller
	StatusCode int
	ErrorCode  string
	Message    string
	RequestID  string
	At         time.Time
}

// Alert is what gets reported once a channel and model have failed often enough.
type Alert struct {
	Channel      Channel
	Model        string
	Count        int
	Window       time.Duration
	Mute         time.Duration
	FirstAt      time.Time
	LastAt       time.Time
	Callers      []Caller // distinct callers, newest first, at most MaxCallers
	TotalCallers int
	Latest       Sample
}

const (
	MaxCallers       = 5
	maxSamplesPerKey = 200
	maxKeys          = 5000
)

type entry struct {
	samples    []Sample
	mutedUntil time.Time
}

// Tracker counts failures per channel and model.
type Tracker struct {
	mu      sync.Mutex
	config  func() Config
	now     func() time.Time
	entries map[string]*entry
	lastGC  time.Time
}

// NewTracker reads its settings through config on every call, so changes made
// in the admin page apply without a restart.
func NewTracker(config func() Config) *Tracker {
	return &Tracker{config: config, now: time.Now, entries: map[string]*entry{}}
}

// Record adds one failure. It returns an Alert, and true, only when this
// failure brings the count in the window to the threshold and the channel and
// model are not muted. Raising an alert starts the mute and restarts the count.
func (t *Tracker) Record(s Sample) (Alert, bool) {
	cfg := t.config()
	if !cfg.enabled() {
		return Alert{}, false
	}
	if s.At.IsZero() {
		s.At = t.now()
	}
	now := s.At
	key := fmt.Sprintf("%d\x00%s", s.Channel.ID, s.Model)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.gc(now, cfg)
	e := t.entries[key]
	if e == nil {
		if len(t.entries) >= maxKeys {
			return Alert{}, false
		}
		e = &entry{}
		t.entries[key] = e
	}
	cutoff := now.Add(-cfg.Window)
	kept := e.samples[:0]
	for _, x := range e.samples {
		if x.At.After(cutoff) {
			kept = append(kept, x)
		}
	}
	e.samples = append(kept, s)
	if len(e.samples) > maxSamplesPerKey {
		e.samples = append([]Sample(nil), e.samples[len(e.samples)-maxSamplesPerKey:]...)
	}
	if now.Before(e.mutedUntil) || len(e.samples) < cfg.Threshold {
		return Alert{}, false
	}
	a := buildAlert(e.samples, cfg)
	e.samples = nil
	e.mutedUntil = now.Add(cfg.Mute)
	return a, true
}

// gc forgets channels and models that have been quiet for a whole window.
func (t *Tracker) gc(now time.Time, cfg Config) {
	if now.Sub(t.lastGC) < time.Minute {
		return
	}
	t.lastGC = now
	cutoff := now.Add(-cfg.Window)
	for k, e := range t.entries {
		if now.Before(e.mutedUntil) {
			continue
		}
		if n := len(e.samples); n == 0 || !e.samples[n-1].At.After(cutoff) {
			delete(t.entries, k)
		}
	}
}

func buildAlert(samples []Sample, cfg Config) Alert {
	last := samples[len(samples)-1]
	a := Alert{
		Channel: last.Channel,
		Model:   last.Model,
		Count:   len(samples),
		Window:  cfg.Window,
		Mute:    cfg.Mute,
		FirstAt: samples[0].At,
		LastAt:  last.At,
		Latest:  last,
	}
	type id struct {
		user  int
		token string
	}
	seen := map[id]bool{}
	for i := len(samples) - 1; i >= 0; i-- {
		c := samples[i].Caller
		k := id{c.UserID, c.TokenName}
		if seen[k] {
			continue
		}
		seen[k] = true
		a.TotalCallers++
		if len(a.Callers) < MaxCallers {
			a.Callers = append(a.Callers, c)
		}
	}
	return a
}
