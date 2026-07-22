package release

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v0.1.1", "v0.2.0", -1, true},     // older < newer
		{"v0.2.0", "v0.1.1", 1, true},      // newer > older
		{"0.1.1", "v0.1.1", 0, true},       // 'v' optional, equal
		{"v0.2.0-rc1", "v0.2.0", -1, true}, // pre-release precedes the final (SU7)
		{"v0.2.0", "v0.2.0-rc1", 1, true},  // ...and the final is newer than its rc
		{"v0.2.0-rc1", "v0.2.0-rc1", 0, true}, // same pre-release, equal
		{"v0.2.5+ci", "v0.2.5", 0, true},   // build metadata does not affect precedence
		{"v1.0", "v1.0.0", 0, true},        // trailing zeros
		{"v0.10.0", "v0.9.0", 1, true},     // numeric, not lexical
		{"dev", "v0.1.1", 0, false},        // un-released build not comparable
		{"v0.1.1", "", 0, false},           // empty not comparable
	}
	for _, c := range cases {
		got, ok := Compare(c.a, c.b)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("Compare(%q,%q) = (%d,%v), want (%d,%v)", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}
