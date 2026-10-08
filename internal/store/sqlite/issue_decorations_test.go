package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestBootstrapAddsDecorationsWithoutLosingExistingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO issue_metadata VALUES('APP-1',3600,'jira-cloud','main','2026-01-01T00:00:00Z')`,
		`INSERT INTO worklogs(id,issue_key,started_at_utc,duration_seconds,description,created_at,updated_at) VALUES('old','APP-1','2026-01-01T00:00:00Z',900,'work','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
		`DROP TABLE issue_decorations`,
	} {
		if _, err := store.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	if opened, err := OpenExistingReadOnly(path); err == nil {
		opened.Close()
		t.Fatal("read-only open silently accepted incomplete schema")
	}
	store, _, err = Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, table := range []string{"worklogs", "issue_metadata"} {
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s rows: %d %v", table, count, err)
		}
	}
	var before, after int
	if err := store.DB().QueryRow(`SELECT revision FROM domain_change_state WHERE id = 1`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(context.Background(), `INSERT INTO issue_decorations VALUES('APP-1','Local title',7200,'2026-02-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT revision FROM domain_change_state WHERE id = 1`).Scan(&after); err != nil || after <= before {
		t.Fatalf("metadata commit did not advance tracker: %d -> %d %v", before, after, err)
	}
}
