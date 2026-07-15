package db

import "testing"

func TestServerSupportsFilteredExpectation(t *testing.T) {
	cases := map[string]bool{
		"2.11.0":      true,
		"2.11.3":      true,
		"2.12.0":      true,
		"3.0.0":       true,
		"2.10.14":     false,
		"2.9.22":      false,
		"1.4.1":       false,
		"":            false,
		"garbage":     false,
		"2":           false,
		"2.x.0":       false,
		"10.0.0":      true,
		"2.11.0-beta": true,
	}
	for version, want := range cases {
		if got := serverSupportsFilteredExpectation(version); got != want {
			t.Errorf("serverSupportsFilteredExpectation(%q) = %v, want %v", version, got, want)
		}
	}
}
