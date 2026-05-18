package cache

import (
	"os"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

func withHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestWriteThenReadRoundTrip(t *testing.T) {
	withHome(t)
	want := File{
		UpdatedAt: time.Now().UTC().Round(time.Millisecond),
		Interval:  10 * time.Second,
		Sessions: []sessions.Session{
			{ID: "a", CWD: "/x", Summary: "hi", State: sessions.StateWorking},
		},
	}
	if err := Write(want); err != nil {
		t.Fatal(err)
	}
	got, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("updated_at: want %v, got %v", want.UpdatedAt, got.UpdatedAt)
	}
	if got.Interval != want.Interval {
		t.Errorf("interval: want %v, got %v", want.Interval, got.Interval)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].ID != "a" || got.Sessions[0].State != sessions.StateWorking {
		t.Errorf("sessions: got %+v", got.Sessions)
	}
}

func TestRead_MissingReturnsNotExist(t *testing.T) {
	withHome(t)
	_, err := Read()
	if !IsNotExist(err) {
		t.Errorf("want fs.ErrNotExist, got %v", err)
	}
}

func TestFresh(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name      string
		updatedAt time.Time
		tol       time.Duration
		want      bool
	}{
		{"recent", now.Add(-5 * time.Second), 20 * time.Second, true},
		{"stale", now.Add(-1 * time.Minute), 20 * time.Second, false},
		{"zero", time.Time{}, 20 * time.Second, false},
	}
	for _, c := range cases {
		f := &File{UpdatedAt: c.updatedAt}
		if got := f.Fresh(now, c.tol); got != c.want {
			t.Errorf("%s: want %v got %v", c.name, c.want, got)
		}
	}
}

func TestNilFresh(t *testing.T) {
	var f *File
	if f.Fresh(time.Now(), time.Minute) {
		t.Error("nil cache should not be fresh")
	}
}

func TestDirCreated(t *testing.T) {
	withHome(t)
	d, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d); err != nil {
		t.Errorf("dir not created: %v", err)
	}
}
