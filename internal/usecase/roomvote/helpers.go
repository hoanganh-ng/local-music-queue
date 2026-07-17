package roomvote

import "strings"

// splitSessionKey returns the (slug, songID) pair encoded in a session
// key of the form "{type}:{slug}:{songID}" where type is "skip" or
// "prioritize". The shared ExpireSessions sweep uses this to recover
// the slug for any session type without a second expiry loop. Malformed
// keys (missing the song segment) degrade gracefully to
// (best-effort-slug, "") so the sweep never panics.
func splitSessionKey(key string) (slug, songID string) {
	rest := key
	if strings.HasPrefix(rest, "prioritize:") {
		rest = strings.TrimPrefix(rest, "prioritize:")
	} else {
		rest = strings.TrimPrefix(rest, "skip:")
	}
	idx := strings.IndexByte(rest, ':')
	if idx < 0 {
		return rest, ""
	}
	return rest[:idx], rest[idx+1:]
}