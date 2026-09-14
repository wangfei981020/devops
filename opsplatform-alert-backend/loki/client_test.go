package loki

import (
	"strings"
	"testing"
	"time"

	"opsplatform-alert-backend/timezone"
)

func TestToHitsPreservesStreamLabels(t *testing.T) {
	body := []byte(`{
  "status":"success",
  "data":{"resultType":"streams","result":[
    {"stream":{"namespace":"g32-uat","container":"api","pod":"api-7d9f"},
     "values":[["1700000000000000000","payment timeout"]]}
  ]}
}`)

	result, err := parseLokiResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hits := result.ToHits()
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}

	// The flattened labels stay: templates already reference them by name.
	if hits[0]["namespace"] != "g32-uat" {
		t.Errorf("flattened label lost: %v", hits[0]["namespace"])
	}

	// The label SET is what a context query needs to rebuild the selector.
	labels, ok := hits[0]["__stream_labels"].(map[string]string)
	if !ok {
		t.Fatalf("__stream_labels missing or wrong type: %T", hits[0]["__stream_labels"])
	}
	if len(labels) != 3 || labels["container"] != "api" {
		t.Errorf("labels = %v, want the 3 stream labels", labels)
	}
	if _, present := labels["message"]; present {
		t.Error("__stream_labels must carry only stream labels, not the log line")
	}
}

// The timestamp lands in an alert card beside a log line that carries the
// source system's own clock, which is routinely UTC. Rendering it without an
// offset makes one instant look like two, hours apart.
func TestToHitsTimestampCarriesItsZone(t *testing.T) {
	r := &QueryResult{Streams: []Stream{{
		Labels:  map[string]string{"container": "atmosphere-client-backend"},
		Entries: []map[string]interface{}{{"timestamp": "1788935856638000000", "line": "boom"}},
	}}}

	hits := r.ToHits()
	if len(hits) != 1 {
		t.Fatalf("ToHits() returned %d hits, want 1", len(hits))
	}

	got, _ := hits[0]["timestamp"].(string)
	want := timezone.FormatWithZone(time.Unix(0, 1788935856638000000))
	if got != want {
		t.Errorf("ToHits() timestamp = %q, want %q", got, want)
	}
	if !strings.Contains(got, "(") {
		t.Errorf("ToHits() timestamp %q carries no zone", got)
	}
	if hits[0]["@timestamp"] != hits[0]["timestamp"] {
		t.Errorf("ToHits() @timestamp %v and timestamp %v disagree", hits[0]["@timestamp"], hits[0]["timestamp"])
	}
}
