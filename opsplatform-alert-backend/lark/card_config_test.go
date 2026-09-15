package lark

import (
	"encoding/json"
	"testing"

	"opsplatform-alert-backend/models"
)

func TestCardCarriesWideScreenMode(t *testing.T) {
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
	if cfg["wide_screen_mode"] != true {
		t.Errorf("wide_screen_mode is not true: %v", cfg["wide_screen_mode"])
	}
	// The rest of the card must be untouched.
	if _, ok := card["header"]; !ok {
		t.Error("header disappeared")
	}
	if _, ok := card["elements"]; !ok {
		t.Error("elements disappeared")
	}
}
