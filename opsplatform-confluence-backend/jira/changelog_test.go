package jira

import (
	"testing"
	"time"
)

func TestParseStatusTransitions_OnlyStatusFieldIncluded(t *testing.T) {
	issue := IssueWithChangelog{}
	issue.Changelog.Histories = []struct {
		Created string `json:"created"`
		Items   []struct {
			Field      string `json:"field"`
			FromString string `json:"fromString"`
			ToString   string `json:"toString"`
		} `json:"items"`
	}{
		{
			Created: "2026-05-13T14:30:00.000+0800",
			Items: []struct {
				Field      string `json:"field"`
				FromString string `json:"fromString"`
				ToString   string `json:"toString"`
			}{
				{Field: "status", FromString: "研发HOD审批", ToString: "运维组长审批"},
				{Field: "assignee", FromString: "alice", ToString: "bob"},
			},
		},
	}
	since := time.Date(2026, 5, 13, 14, 0, 0, 0, time.FixedZone("CST", 8*3600))
	transitions := ParseStatusTransitions(issue, since)
	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(transitions))
	}
	if transitions[0].From != "研发HOD审批" || transitions[0].To != "运维组长审批" {
		t.Fatalf("wrong transition: %+v", transitions[0])
	}
}

func TestParseStatusTransitions_FiltersOldEntries(t *testing.T) {
	issue := IssueWithChangelog{}
	issue.Changelog.Histories = []struct {
		Created string `json:"created"`
		Items   []struct {
			Field      string `json:"field"`
			FromString string `json:"fromString"`
			ToString   string `json:"toString"`
		} `json:"items"`
	}{
		{
			Created: "2026-05-13T10:00:00.000+0800",
			Items: []struct {
				Field      string `json:"field"`
				FromString string `json:"fromString"`
				ToString   string `json:"toString"`
			}{{Field: "status", FromString: "A", ToString: "B"}},
		},
		{
			Created: "2026-05-13T14:30:00.000+0800",
			Items: []struct {
				Field      string `json:"field"`
				FromString string `json:"fromString"`
				ToString   string `json:"toString"`
			}{{Field: "status", FromString: "B", ToString: "C"}},
		},
	}
	since := time.Date(2026, 5, 13, 14, 0, 0, 0, time.FixedZone("CST", 8*3600))
	transitions := ParseStatusTransitions(issue, since)
	if len(transitions) != 1 {
		t.Fatalf("expected 1 transition after filter, got %d", len(transitions))
	}
	if transitions[0].To != "C" {
		t.Fatalf("expected To=C, got %s", transitions[0].To)
	}
}

func TestParseStatusTransitions_MultipleInOneWindow(t *testing.T) {
	issue := IssueWithChangelog{}
	issue.Changelog.Histories = []struct {
		Created string `json:"created"`
		Items   []struct {
			Field      string `json:"field"`
			FromString string `json:"fromString"`
			ToString   string `json:"toString"`
		} `json:"items"`
	}{
		{
			Created: "2026-05-13T14:30:00.000+0800",
			Items: []struct {
				Field      string `json:"field"`
				FromString string `json:"fromString"`
				ToString   string `json:"toString"`
			}{{Field: "status", FromString: "A", ToString: "B"}},
		},
		{
			Created: "2026-05-13T14:30:30.000+0800",
			Items: []struct {
				Field      string `json:"field"`
				FromString string `json:"fromString"`
				ToString   string `json:"toString"`
			}{{Field: "status", FromString: "B", ToString: "C"}},
		},
	}
	since := time.Date(2026, 5, 13, 14, 0, 0, 0, time.FixedZone("CST", 8*3600))
	transitions := ParseStatusTransitions(issue, since)
	if len(transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(transitions))
	}
	if transitions[0].To != "B" || transitions[1].To != "C" {
		t.Fatalf("wrong order: %+v", transitions)
	}
}
