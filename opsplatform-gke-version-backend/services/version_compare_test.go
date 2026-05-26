package services

import (
	"math"
	"testing"
)

func TestIndexDiff(t *testing.T) {
	valid := []string{
		"1.33.10-gke.1115000",
		"1.33.10-gke.1067000",
		"1.33.9-gke.1166000",
		"1.33.9-gke.1117000",
		"1.33.2-gke.1111000",
	}
	cases := []struct {
		low, high string
		want      int
	}{
		{"1.33.2-gke.1111000", "1.33.10-gke.1115000", 4},
		{"1.33.10-gke.1115000", "1.33.10-gke.1115000", 0},
		{"1.33.9-gke.1117000", "1.33.10-gke.1115000", 3},
	}
	for _, c := range cases {
		got := IndexDiff(c.low, c.high, valid)
		if got != c.want {
			t.Errorf("IndexDiff(%s,%s): got %d want %d", c.low, c.high, got, c.want)
		}
	}
}

func TestArithmeticDiff(t *testing.T) {
	cases := []struct {
		low, high string
		want      float64
	}{
		{"1.33.2-gke.1111000", "1.33.10-gke.1115000", 8.0004},
		{"1.33.10-gke.1115000", "1.33.10-gke.1115000", 0.0},
	}
	for _, c := range cases {
		got, err := ArithmeticDiff(c.low, c.high)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if math.Abs(got-c.want) > 0.0001 {
			t.Errorf("ArithmeticDiff(%s,%s): got %f want %f", c.low, c.high, got, c.want)
		}
	}
}
