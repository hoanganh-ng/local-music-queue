package entity

import (
	"testing"
	"time"
)

func TestPlayerLease_ArchiveReason_IsValid(t *testing.T) {
	for _, r := range []PlayerLeaseArchiveReason{
		PlayerLeaseExpired, PlayerLeaseHostLeft, PlayerLeaseExplicit,
	} {
		if !r.IsValid() {
			t.Errorf("expected %s valid", r)
		}
	}
	if PlayerLeaseArchiveReason("nope").IsValid() {
		t.Error("expected unknown reason invalid")
	}
}

func TestPlayerLease_IsWithinGrace(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	l := &PlayerLease{ExpiresAt: now.Add(-10 * time.Second)}
	if !l.IsWithinGrace(now, 30*time.Second) {
		t.Error("expected -10s within 30s grace")
	}
	// expires_at = now-10s; grace = 30s; at now+21s we are past expires_at+grace.
	if l.IsWithinGrace(now.Add(21*time.Second), 30*time.Second) {
		t.Error("expected +21s outside 30s grace (expires_at+30s reached)")
	}
}

func TestPlayerLease_IsExpired(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	ended := now.Add(-1 * time.Second)
	l := &PlayerLease{ExpiresAt: now.Add(-60 * time.Second), EndedAt: &ended}
	if !l.IsExpired(now) {
		t.Error("expected ended lease to be expired")
	}
}