package sessions

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "store.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY, cwd TEXT, repository TEXT, branch TEXT,
			summary TEXT, created_at TEXT, updated_at TEXT, host_type TEXT
		);
		INSERT INTO sessions(id, cwd, repository, summary, updated_at) VALUES
			('uuid-recent',  '/tmp/a', 'gh/a', 'recent one',  datetime('now')),
			('uuid-old',     '/tmp/b', 'gh/b', 'two weeks+',  datetime('now', '-20 days')),
			('uuid-mid',     '/tmp/c', 'gh/c', 'mid',         datetime('now', '-3 days'));
	`)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRecent_FiltersByAgeAndOrders(t *testing.T) {
	path := newTestDB(t)
	got, err := LoadRecent(path, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 sessions (within 14 days), got %d", len(got))
	}
	if got[0].ID != "uuid-recent" || got[1].ID != "uuid-mid" {
		t.Fatalf("want recent then mid, got %s,%s", got[0].ID, got[1].ID)
	}
	if got[0].CWD != "/tmp/a" || got[0].Repository != "gh/a" || got[0].Summary != "recent one" {
		t.Fatalf("fields not loaded: %+v", got[0])
	}
}
