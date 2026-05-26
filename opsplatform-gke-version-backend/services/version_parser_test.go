package services

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in      string
		major   int
		minor   int
		patch   int
		build   int64
		wantErr bool
	}{
		{"1.33.10-gke.1115000", 1, 33, 10, 1115000, false},
		{"1.33.2-gke.1111000", 1, 33, 2, 1111000, false},
		{"1.30.5-gke.1234567", 1, 30, 5, 1234567, false},
		{"invalid", 0, 0, 0, 0, true},
		{"1.33.10", 0, 0, 0, 0, true},
		{"1.33-gke.111", 0, 0, 0, 0, true},
	}
	for _, c := range cases {
		v, err := ParseVersion(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expect error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.in, err)
			continue
		}
		if v.Major != c.major || v.Minor != c.minor || v.Patch != c.patch || v.Build != c.build {
			t.Errorf("%s: got %+v", c.in, v)
		}
	}
}

func TestArithmeticEncode(t *testing.T) {
	v, _ := ParseVersion("1.33.10-gke.1115000")
	got := v.ArithmeticEncode()
	want := 13310.1115000
	if got != want {
		t.Errorf("encode 1.33.10-gke.1115000: got %f want %f", got, want)
	}
}
