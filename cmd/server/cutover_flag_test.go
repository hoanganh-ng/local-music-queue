package main

// R14c flag-parsing contract (pure unit tests, no database):
// omitted → false, explicit =false → false, explicit =true → true, and
// every surprising input (bare flag, invalid value, unknown flag,
// positional argument, duplicate flag) is a startup error — never a
// silent mode selection.

import (
	"strings"
	"testing"
)

func TestParseRoomCutoverFlag_Valid(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"omitted defaults to false", nil, false},
		{"empty slice defaults to false", []string{}, false},
		{"explicit false (double dash)", []string{"--room-cutover-authoritative=false"}, false},
		{"explicit true (double dash)", []string{"--room-cutover-authoritative=true"}, true},
		{"explicit false (single dash)", []string{"-room-cutover-authoritative=false"}, false},
		{"explicit true (single dash)", []string{"-room-cutover-authoritative=true"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseRoomCutoverFlag(tc.args)
			if err != nil {
				t.Fatalf("parseRoomCutoverFlag(%v): unexpected error: %v", tc.args, err)
			}
			if opts.roomCutoverAuthoritative != tc.want {
				t.Errorf("parseRoomCutoverFlag(%v) = %t, want %t", tc.args, opts.roomCutoverAuthoritative, tc.want)
			}
		})
	}
}

func TestParseRoomCutoverFlag_Errors(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantSub string
	}{
		{"bare flag without value", []string{"--room-cutover-authoritative"}, "explicit value"},
		{"bare single-dash flag", []string{"-room-cutover-authoritative"}, "explicit value"},
		{"invalid value", []string{"--room-cutover-authoritative=yes"}, "invalid value"},
		{"empty value", []string{"--room-cutover-authoritative="}, "invalid value"},
		{"uppercase value", []string{"--room-cutover-authoritative=TRUE"}, "invalid value"},
		{"unknown flag", []string{"--verbose"}, "unknown flag"},
		{"unknown flag with value", []string{"--mode=true"}, "unknown flag"},
		{"positional argument", []string{"serve"}, "positional argument"},
		{"duplicate flag", []string{"--room-cutover-authoritative=true", "--room-cutover-authoritative=true"}, "more than once"},
		{"conflicting duplicate", []string{"--room-cutover-authoritative=true", "--room-cutover-authoritative=false"}, "more than once"},
		{"valid then positional", []string{"--room-cutover-authoritative=true", "extra"}, "positional argument"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseRoomCutoverFlag(tc.args)
			if err == nil {
				t.Fatalf("parseRoomCutoverFlag(%v): expected error, got %+v", tc.args, opts)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("parseRoomCutoverFlag(%v) error %q does not contain %q", tc.args, err.Error(), tc.wantSub)
			}
			if opts.roomCutoverAuthoritative {
				t.Errorf("parseRoomCutoverFlag(%v): error path must return the zero (false) option", tc.args)
			}
		})
	}
}

// TestRetiredGlobalContractPatterns_Inventory pins the exact approved
// retirement inventory: the 15 legacy global REST method+path pairs plus
// the global /ws HTTP phase — nothing more, nothing less. Any drift in
// this list is a contract change and must fail review.
func TestRetiredGlobalContractPatterns_Inventory(t *testing.T) {
	want := map[string]bool{
		"GET /api/queue":             true,
		"POST /api/queue/add":        true,
		"POST /api/queue/skip":       true,
		"POST /api/queue/status":     true,
		"POST /api/queue/sync":       true,
		"POST /api/queue/ended":      true,
		"POST /api/queue/prev":       true,
		"POST /api/queue/remove":     true,
		"POST /api/queue/clear":      true,
		"POST /api/queue/volume":     true,
		"POST /api/queue/prioritize": true,
		"POST /api/vote/skip":        true,
		"POST /api/vote/prioritize":  true,
		"POST /api/autoqueue/toggle": true,
		"GET /api/autoqueue/status":  true,
		"/ws":                        true,
	}
	if len(retiredGlobalContractPatterns) != len(want) {
		t.Fatalf("retirement inventory has %d entries, want %d", len(retiredGlobalContractPatterns), len(want))
	}
	seen := map[string]bool{}
	for _, p := range retiredGlobalContractPatterns {
		if !want[p] {
			t.Errorf("unexpected retired pattern %q", p)
		}
		if seen[p] {
			t.Errorf("duplicate retired pattern %q", p)
		}
		seen[p] = true
	}
	for p := range want {
		if !seen[p] {
			t.Errorf("missing retired pattern %q", p)
		}
	}
}
