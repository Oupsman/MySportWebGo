package vars

import "testing"

func TestParseSize(t *testing.T) {
	for text, want := range map[string]int64{"1g": 1 << 30, "1G": 1 << 30, "500MB": 500 << 20, "1.5g": 3 << 29,
		"2t": 2 << 40, "1024": 1024, "64k": 64 << 10, "none": 0, "0": 0, " 2 GB ": 2 << 30} {
		got, err := ParseSize(text)
		if err != nil || got != want {
			t.Errorf("%q: %d %v, want %d", text, got, err, want)
		}
	}
	for _, text := range []string{"", "lots", "-1g", "1x", "g"} {
		if _, err := ParseSize(text); err == nil {
			t.Errorf("%q accepted", text)
		}
	}
}
