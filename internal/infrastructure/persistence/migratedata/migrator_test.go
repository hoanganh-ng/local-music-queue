package migratedata

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBool0or1ToBool(t *testing.T) {
	cases := []struct {
		in   int64
		want bool
	}{
		{0, false},
		{1, true},
		{2, false}, // SQLite schema uses INTEGER NOT NULL DEFAULT 0; treat anything non-1 as false
		{-1, false},
	}
	for _, c := range cases {
		if got := bool0or1ToBool(c.in); got != c.want {
			t.Errorf("bool0or1ToBool(%d) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestRemapID(t *testing.T) {
	m := map[int64]int64{1: 100, 2: 200, 42: 9999}
	if got, ok := RemapID(m, 1); !ok || got != 100 {
		t.Errorf("RemapID(m, 1) = (%d, %v); want (100, true)", got, ok)
	}
	if got, ok := RemapID(m, 99); ok || got != 0 {
		t.Errorf("RemapID(m, 99) = (%d, %v); want (0, false)", got, ok)
	}
	if got, ok := RemapID(nil, 1); ok || got != 0 {
		t.Errorf("RemapID(nil, 1) = (%d, %v); want (0, false)", got, ok)
	}
}

func TestReport_TextFormat(t *testing.T) {
	r := &Report{
		StartedAt:  mustParseTime("2026-01-02T15:04:05Z"),
		FinishedAt: mustParseTime("2026-01-02T15:04:06Z"),
		RedactedDSN: "postgres://user:***@host:5432/db",
		Notes:     []string{"all good"},
		Tables: []TableReport{
			{Table: "users", SourceCount: 3, TargetCount: 3, SourceMinID: 1, SourceMaxID: 3, TargetMinID: 10, TargetMaxID: 12},
		},
		QueueState: QueueStateBytes{Source: 42, Target: 42},
	}
	var buf bytes.Buffer
	if err := r.WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Migration report", "users", "42"} {
		if !strings.Contains(out, want) {
			t.Errorf("WriteText output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestReport_JSONFormat(t *testing.T) {
	r := &Report{
		StartedAt:  mustParseTime("2026-01-02T15:04:05Z"),
		FinishedAt: mustParseTime("2026-01-02T15:04:06Z"),
		RedactedDSN: "***",
		Tables: []TableReport{
			{Table: "queue_state", SourceCount: 1, TargetCount: 1, SourceMinID: 1, SourceMaxID: 1, TargetMinID: 1, TargetMaxID: 1},
		},
		QueueState: QueueStateBytes{Source: 7, Target: 7},
	}
	var buf bytes.Buffer
	if err := r.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var back Report
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Tables) != 1 || back.Tables[0].Table != "queue_state" {
		t.Errorf("round-trip tables: %+v", back.Tables)
	}
	if back.QueueState.Source != 7 || back.QueueState.Target != 7 {
		t.Errorf("round-trip queue_state bytes: %+v", back.QueueState)
	}
}

func TestReport_AllTablesMatch(t *testing.T) {
	r := &Report{
		Tables: []TableReport{
			{Table: "a", SourceCount: 1, TargetCount: 1},
			{Table: "b", SourceCount: 2, TargetCount: 2},
		},
	}
	if !r.AllTablesMatch() {
		t.Error("expected all-match")
	}
	r.Tables[1].TargetCount = 3
	if r.AllTablesMatch() {
		t.Error("expected mismatch on b")
	}
}

func TestReport_MergeSourceStats(t *testing.T) {
	post := []TableReport{
		{Table: "users", TargetCount: 3, TargetMinID: 100, TargetMaxID: 102},
		{Table: "queue_state", TargetCount: 1, TargetMinID: 1, TargetMaxID: 1},
	}
	probe := []TableReport{
		{Table: "users", SourceCount: 3, SourceMinID: 1, SourceMaxID: 3},
		{Table: "queue_state", SourceCount: 1, SourceMinID: 1, SourceMaxID: 1},
	}
	r := &Report{Tables: post}
	r.MergeSourceStats(probe)
	if r.Tables[0].SourceCount != 3 || r.Tables[0].SourceMinID != 1 || r.Tables[0].SourceMaxID != 3 {
		t.Errorf("merge failed for users: %+v", r.Tables[0])
	}
	if r.Tables[1].SourceCount != 1 {
		t.Errorf("merge failed for queue_state: %+v", r.Tables[1])
	}
}

func mustParseTime(s string) (out time.Time) {
	out, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return
}