package sqlite

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestExistingStoreRequiresExplicitMinuteGuardBootstrap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`DROP TRIGGER trg_worklogs_minutes_insert`); err != nil {
		t.Fatal(err)
	}
	store.Close()
	for _, open := range []func(string) (*Store, error){OpenExisting, OpenExistingReadOnly} {
		if opened, err := open(path); err == nil {
			opened.Close()
			t.Fatal("opened store without minute guard")
		} else {
			var mismatch *OpenExistingError
			if !errors.As(err, &mismatch) || mismatch.Kind != OpenExistingErrorSchemaMismatch {
				t.Fatal(err)
			}
		}
	}
	store, _, err = Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
}

func TestMinuteStorageGuards(t *testing.T) {
	store, _, err := Bootstrap(filepath.Join(t.TempDir(), "minutes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	insert := `INSERT INTO worklogs(id, issue_key, started_at_utc, duration_seconds, description, created_at, updated_at) VALUES('log','APP-1',?,?,'test','2026-05-01T00:00:00Z','2026-05-01T00:00:00Z')`
	for _, tc := range []struct {
		start   string
		seconds int
	}{
		{"2026-05-01T08:00:00Z", 901}, {"2026-05-01T08:00:01Z", 900},
		{"2026-05-01T08:00:00.001Z", 900}, {"invalid", 900}, {"2026-05-01T08:00:00Z", 0},
	} {
		if _, err := store.DB().Exec(insert, tc.start, tc.seconds); err == nil {
			t.Fatalf("accepted %s/%d", tc.start, tc.seconds)
		}
	}
	if _, err := store.DB().Exec(insert, "2026-05-01T08:00:00Z", 900); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE worklogs SET duration_seconds=901 WHERE id='log'`); err == nil {
		t.Fatal("accepted update with seconds")
	}
	if _, err := store.DB().Exec(`UPDATE worklogs SET started_at_utc='2026-05-01T08:00:30Z' WHERE id='log'`); err == nil {
		t.Fatal("accepted second-precision start")
	}
	preset := `INSERT INTO worklog_presets(id,name,issue_key,start_time,duration_seconds,description,created_at,updated_at) VALUES('preset','test','APP-1','08:00',?,'test','2026-05-01T00:00:00Z','2026-05-01T00:00:00Z')`
	if _, err := store.DB().Exec(preset, 901); err == nil {
		t.Fatal("accepted non-minute preset")
	}
	if _, err := store.DB().Exec(preset, 900); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE worklog_presets SET duration_seconds=901 WHERE id='preset'`); err == nil {
		t.Fatal("accepted non-minute preset update")
	}
}

func TestBootstrapPreservesExistingSecondPrecisionRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE worklogs(id TEXT PRIMARY KEY,issue_key TEXT NOT NULL,started_at_utc TEXT NOT NULL,duration_seconds INTEGER NOT NULL,description TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,revision INTEGER NOT NULL DEFAULT 1);
	INSERT INTO worklogs VALUES('existing','APP-1','2026-05-01T08:00:46Z',931,'historical','2026-05-01T00:00:00Z','2026-05-01T00:00:00Z',1)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var start string
	var duration int
	if err := store.DB().QueryRow(`SELECT started_at_utc,duration_seconds FROM worklogs WHERE id='existing'`).Scan(&start, &duration); err != nil {
		t.Fatal(err)
	}
	if start != "2026-05-01T08:00:46Z" || duration != 931 {
		t.Fatalf("startup converted data: %s/%d", start, duration)
	}
}
