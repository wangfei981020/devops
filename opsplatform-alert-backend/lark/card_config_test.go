package lark

import (
	"encoding/json"
	"testing"

	"opsplatform-alert-backend/models"
)

// The card must carry width_mode, not the legacy wide_screen_mode: Lark's
// current reference documents the former and silently ignores the latter —
// measured on a real alert, visible width was 64 characters either way.
func TestCardCarriesWidthMode(t *testing.T) {
	s := &Sender{config: models.LarkConfig{}}
	payload := s.buildCard("标题", "正文", "S1", nil, false)
	b, _ := json.Marshal(payload)

	var got map[string]interface{}
	json.Unmarshal(b, &got)
	card, ok := got["card"].(map[string]interface{})
	if !ok {
		t.Fatalf("no card in payload: %s", b)
	}
	cfg, ok := card["config"].(map[string]interface{})
	if !ok {
		t.Fatalf("card has no config block:\n%s", b)
	}
	if cfg["width_mode"] != "fill" {
		t.Errorf("width_mode = %v, want \"fill\"", cfg["width_mode"])
	}
	if _, legacy := cfg["wide_screen_mode"]; legacy {
		t.Error("wide_screen_mode is the ignored legacy field; it must not come back")
	}
	// The rest of the card must be untouched.
	if _, ok := card["header"]; !ok {
		t.Error("header disappeared")
	}
	if _, ok := card["elements"]; !ok {
		t.Error("elements disappeared")
	}
}
