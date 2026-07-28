package main

// R14c runtime flag. The single approved server argument is
//
//	--room-cutover-authoritative=true|false
//
// parsed here before database composition and before any listener opens.
// The parsed result travels through the narrow immutable setupOptions
// value; there is deliberately NO mutable package-level mode state and
// the Go server NEVER reads VITE_ROOM_CUTOVER_AUTHORITATIVE (that build
// argument belongs exclusively to the R14d frontend bundle).

import (
	"fmt"
	"strings"
)

// setupOptions is the narrow immutable setup option produced by argument
// parsing in main. It is passed by value into setupApp /
// setupAppWithActivityObserver and read exactly once during composition.
type setupOptions struct {
	// roomCutoverAuthoritative selects the R14c runtime mode:
	//   false — pre-cutover/rollback pair: real legacy global handlers
	//           and global /ws stay registered, the explicit no-op
	//           room-activity writer is selected, and defensive startup
	//           migrations keep their existing behavior.
	//   true  — authoritative room runtime: the fail-closed startup
	//           guard must pass, the real PostgreSQL room-activity
	//           writer is selected, and every approved legacy global
	//           route plus the global /ws HTTP phase is served by the
	//           repository-free 410 Gone tombstone.
	roomCutoverAuthoritative bool
}

// roomCutoverFlagName is the canonical double-dash spelling; the
// single-dash Go spelling is accepted as well.
const roomCutoverFlagName = "--room-cutover-authoritative"

// parseRoomCutoverFlag parses the process arguments (os.Args[1:]).
//
// Rules (fail-closed — any surprise is a startup error, never a silent
// mode selection):
//   - omitted                → false
//   - explicit "=false"      → false
//   - explicit "=true"       → true
//   - bare flag (no value)   → error (an explicit value is required)
//   - invalid value          → error
//   - unknown flag           → error
//   - positional argument    → error
//   - repeated flag          → error (ambiguous operator intent)
func parseRoomCutoverFlag(args []string) (setupOptions, error) {
	opts := setupOptions{}
	seen := false
	for _, arg := range args {
		name, value, hasValue := strings.Cut(arg, "=")
		if name != roomCutoverFlagName && name != "-room-cutover-authoritative" {
			if strings.HasPrefix(arg, "-") {
				return setupOptions{}, fmt.Errorf("unknown flag %q (the only supported flag is %s=true|false)", arg, roomCutoverFlagName)
			}
			return setupOptions{}, fmt.Errorf("unexpected positional argument %q", arg)
		}
		if !hasValue {
			return setupOptions{}, fmt.Errorf("%s requires an explicit value: %s=true|false", roomCutoverFlagName, roomCutoverFlagName)
		}
		if seen {
			return setupOptions{}, fmt.Errorf("%s specified more than once", roomCutoverFlagName)
		}
		seen = true
		switch value {
		case "true":
			opts.roomCutoverAuthoritative = true
		case "false":
			opts.roomCutoverAuthoritative = false
		default:
			return setupOptions{}, fmt.Errorf("invalid value %q for %s (want true or false)", value, roomCutoverFlagName)
		}
	}
	return opts, nil
}

// retiredGlobalContractPatterns is the exact approved R14c retirement
// inventory: the 15 legacy global REST method+path pairs plus the
// global /ws HTTP phase. It is consumed ONLY by the central
// all-or-nothing registration decision in setupAppWithActivityObserver;
// there is no per-route runtime configuration. Auth, priority-balance,
// YouTube-search, room, and invite routes are NOT in this list and
// remain live in both modes.
var retiredGlobalContractPatterns = []string{
	"GET /api/queue",
	"POST /api/queue/add",
	"POST /api/queue/skip",
	"POST /api/queue/status",
	"POST /api/queue/sync",
	"POST /api/queue/ended",
	"POST /api/queue/prev",
	"POST /api/queue/remove",
	"POST /api/queue/clear",
	"POST /api/queue/volume",
	"POST /api/queue/prioritize",
	"POST /api/vote/skip",
	"POST /api/vote/prioritize",
	"POST /api/autoqueue/toggle",
	"GET /api/autoqueue/status",
	"/ws",
}
