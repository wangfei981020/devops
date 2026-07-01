// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package models

import "testing"

func TestStreamSeriesLabel(t *testing.T) {
	tests := []struct {
		tableID string
		want    string
	}{
		{"L001", "L系列"},
		{"D001", "D系列"},
		{"LD99", "LD系列"},
		{"001", ""},
		{"台01", "台系列"},
	}
	for _, tt := range tests {
		t.Run(tt.tableID, func(t *testing.T) {
			if got := StreamSeriesLabel(tt.tableID); got != tt.want {
				t.Errorf("StreamSeriesLabel(%q) = %q, want %q", tt.tableID, got, tt.want)
			}
		})
	}
}
