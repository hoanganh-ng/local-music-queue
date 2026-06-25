package entity

import "testing"

func TestRoom_IsValidSlug(t *testing.T) {
	cases := []struct {
		slug string
		want bool
	}{
		{"abc", true},        // min 3 chars (positive boundary)
		{"my-room", true},
		{"my-cool-room-2", true},
		{"abc123", true},
		{"a", false},        // too short (1 char)
		{"ab", false},       // too short (2 chars)
		{"aa", false},       // too short (2 chars)
		{"A", false},        // uppercase
		{"-leading", false}, // leading dash
		{"trailing-", false},
		{"_under", false},
		{"", false},
		// 40-char: first+last=2, middle=38 (max) — valid
		{"ab" + "cdefghijklmnopqrstuvwxyz0123456789-0" + "cd", true},
		// 41-char: first+last=2, middle=39 (over max 38) — invalid
		{"ab" + "cdefghijklmnopqrstuvwxyz0123456789-01" + "cd", false},
		{"a-b-c-d-e-f-g-h-i-j-k-l-m-n-o-p-q-r-s-t-u-v-w-x-y-z-1-2-3-4-5", false}, // too long (52 chars)
	}
	for _, c := range cases {
		if got := IsValidSlug(c.slug); got != c.want {
			t.Errorf("IsValidSlug(%q) = %v, want %v", c.slug, got, c.want)
		}
	}
}

func TestRoom_IsReservedSlug(t *testing.T) {
	for _, s := range []string{"api", "admin", "static", "ws"} {
		if !IsReservedSlug(s) {
			t.Errorf("expected %q to be reserved", s)
		}
	}
	if IsReservedSlug("my-room") {
		t.Error("my-room must not be reserved")
	}
}