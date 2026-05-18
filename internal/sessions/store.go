package sessions

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

func LoadRecent(dbPath string, maxAge time.Duration) ([]Session, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer db.Close()

	cutoff := time.Now().Add(-maxAge).UTC().Format("2006-01-02 15:04:05")
	rows, err := db.Query(`
		SELECT id, COALESCE(cwd,''), COALESCE(repository,''),
		       COALESCE(summary,''), updated_at
		FROM sessions
		WHERE updated_at >= ?
		ORDER BY updated_at DESC
	`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var s Session
		var ts string
		if err := rows.Scan(&s.ID, &s.CWD, &s.Repository, &s.Summary, &ts); err != nil {
			return nil, err
		}
		s.UpdatedAt = parseSqliteTime(ts)
		out = append(out, s)
	}
	return out, rows.Err()
}

func parseSqliteTime(s string) time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
