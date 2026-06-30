package roomvote

import "strings"

// splitSessionKey returns the (slug, songID) pair encoded in a session
// key of the form "skip:{slug}:{songID}". Malformed keys (no second
// colon) yield ("key-without-skip-prefix", "") so callers never panic.
func splitSessionKey(key string) (slug, songID string) {
	rest := strings.TrimPrefix(key, "skip:")
	idx := strings.IndexByte(rest, ':')
	if idx < 0 {
		return rest, ""
	}
	return rest[:idx], rest[idx+1:]
}