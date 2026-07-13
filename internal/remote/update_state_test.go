package remote

import (
	"testing"
	"time"
)

func withHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestNeedsRefresh(t *testing.T) {
	now := time.Now().UTC()
	state := UpdateState{LastCheckedAt: now.Add(-23 * time.Hour)}
	if NeedsRefresh(state, now, 24*time.Hour, false) {
		t.Fatal("expected no refresh inside ttl")
	}
	if !NeedsRefresh(state, now, 24*time.Hour, true) {
		t.Fatal("expected forced refresh")
	}
}

func TestNeedsRefresh_PastTTL(t *testing.T) {
	now := time.Now().UTC()
	state := UpdateState{LastCheckedAt: now.Add(-25 * time.Hour)}
	if !NeedsRefresh(state, now, 24*time.Hour, false) {
		t.Fatal("expected refresh past ttl")
	}
}

func TestNeedsRefresh_ZeroStateForcesRefresh(t *testing.T) {
	now := time.Now().UTC()
	if !NeedsRefresh(UpdateState{}, now, 24*time.Hour, false) {
		t.Fatal("expected refresh when no prior state recorded")
	}
}

func TestUpdateStatePersistence_RoundTrip(t *testing.T) {
	withHome(t)
	want := UpdateState{
		LastCheckedAt: time.Now().UTC().Round(time.Millisecond),
		Runtime:       "linux-amd64",
		Version:       "v1.2.3",
		AssetName:     "tsession_linux-amd64.tar.gz",
	}
	if err := SaveUpdateState("devbox", want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadUpdateState("devbox")
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastCheckedAt.Equal(want.LastCheckedAt) {
		t.Errorf("last_checked_at: want %v, got %v", want.LastCheckedAt, got.LastCheckedAt)
	}
	if got.Runtime != want.Runtime || got.Version != want.Version || got.AssetName != want.AssetName {
		t.Errorf("state = %+v, want %+v", got, want)
	}
}

func TestUpdateStatePersistence_MissingReturnsZeroValue(t *testing.T) {
	withHome(t)
	got, err := LoadUpdateState("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastCheckedAt.IsZero() {
		t.Fatalf("expected zero-value state, got %+v", got)
	}
}
