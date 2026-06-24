package migratedata

// bool0or1ToBool converts a SQLite INTEGER 0/1 representation of a boolean
// into a Go bool. SQLite has no native boolean type and the schema declares
// `enabled INTEGER NOT NULL DEFAULT 0`; values other than 1 are treated as
// false to match the existing `SQLiteAutoQueueRepository.GetConfig` behavior
// which uses `enabled == 1`.
func bool0or1ToBool(v int64) bool {
	return v == 1
}

// RemapID looks up a source (SQLite) user id in the identity map and returns
// the matching target (PostgreSQL) bigint id. The second return value is
// false when the source id is not present in the map; callers should treat
// that as a referential-integrity violation and either skip the row or fail
// loudly depending on their policy.
func RemapID(idMap map[int64]int64, sourceID int64) (int64, bool) {
	if idMap == nil {
		return 0, false
	}
	v, ok := idMap[sourceID]
	return v, ok
}