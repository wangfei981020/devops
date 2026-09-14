package handlers

import "testing"

// The attention list is built entirely from the enabled rules, so an
// installation with none produces an empty list — and an empty list renders as
// "everything is fine". These cases pin the distinction that stops a fresh
// install being told its alerting is healthy when nothing is set up.
func TestSetupStateSeparatesHealthyFromUnconfigured(t *testing.T) {
	cases := []struct {
		name           string
		counts         map[string]int
		wantConfigured bool
		wantDone       int
	}{
		{
			name:           "fresh install has nothing",
			counts:         map[string]int{},
			wantConfigured: false,
			wantDone:       0,
		},
		{
			name:           "a source alone is not enough to alert",
			counts:         map[string]int{"es_active": 1},
			wantConfigured: false,
			wantDone:       1,
		},
		{
			name:           "a source and a channel still need a rule",
			counts:         map[string]int{"loki_active": 1, "lark_active": 2},
			wantConfigured: false,
			wantDone:       2,
		},
		{
			name: "rules exist but none is enabled",
			counts: map[string]int{
				"es_active": 1, "lark_active": 1, "rules_total": 4, "rules_enabled": 0,
			},
			wantConfigured: false,
			wantDone:       2,
		},
		{
			name: "all three present",
			counts: map[string]int{
				"loki_active": 1, "lark_active": 1, "rules_enabled": 1,
			},
			wantConfigured: true,
			wantDone:       3,
		},
	}

	for _, c := range cases {
		got := setupState(c.counts)
		if got["configured"] != c.wantConfigured {
			t.Errorf("%s: configured = %v, want %v", c.name, got["configured"], c.wantConfigured)
		}
		if got["done"] != c.wantDone {
			t.Errorf("%s: done = %v, want %d", c.name, got["done"], c.wantDone)
		}
	}
}

// A connection that exists but is disabled cannot be queried, so it must not
// count as a completed step — otherwise the checklist says the platform is
// ready while every run fails.
func TestSetupStateIgnoresDisabledResources(t *testing.T) {
	got := setupState(map[string]int{
		"es_connections": 3, "es_active": 0,
		"loki_connections": 1, "loki_active": 0,
		"lark_configs": 2, "lark_active": 0,
		"rules_total": 5, "rules_enabled": 0,
	})
	if got["configured"] != false {
		t.Error("resources that exist but are all disabled must not count as configured")
	}
	if got["done"] != 0 {
		t.Errorf("done = %v, want 0 — nothing usable exists", got["done"])
	}
}
