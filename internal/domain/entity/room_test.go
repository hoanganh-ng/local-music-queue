package entity

import "testing"

func TestRoom_IsValidSlug(t *testing.T) {
	cases := []struct {
		slug string
		want bool
	}{
		{"a", true},
		{"ab", true},
		{"my-room", true},
		{"my-cool-room-2", true},
		{"abc123", true},
		{"A", false},        // uppercase
		{"-leading", false}, // leading dash
		{"trailing-", false},
		{"_under", false},
		{"", false},
		{"a-b-c-d-e-f-g-h-i-j-k-l-m-n-o-p-q-r-s-t-u-v-w-x-y-z-1-2-3-4-5", false}, // too long
		{"aa", true},
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