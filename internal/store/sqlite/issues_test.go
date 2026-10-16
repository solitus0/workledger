package sqlite

import (
	"path/filepath"
	"testing"
)

func TestBootstrapAddsCachedTitleWithoutChangingIssueData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "issues.db")
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO issues(issue_key,title,epic,max_estimate_seconds,created_at,updated_at) VALUES('APP-1','Manual title','October',7200,'2026-01-01T00:00:00Z','2026-02-01T00:00:00Z')`,
		`INSERT INTO issue_metadata(issue_key,title,max_estimate_seconds,source_adapter_family,source_adapter_instance,refreshed_at) VALUES('APP-1','Discarded title',3600,'jira-cloud','main','2026-03-01T00:00:00Z')`,
		`ALTER TABLE issue_metadata DROP COLUMN title`,
	} {
		if _, err := store.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	store, status, err := Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if status != StatusRepaired {
		t.Fatalf("status = %s", status)
	}
	var title, epic, created, updated, family, instance, refreshed string
	var manual, cached int64
	var missingTitle bool
	if err := store.DB().QueryRow(`SELECT d.title,d.epic,d.max_estimate_seconds,d.created_at,d.updated_at,c.title IS NULL,c.max_estimate_seconds,c.source_adapter_family,c.source_adapter_instance,c.refreshed_at FROM issues d JOIN issue_metadata c USING(issue_key) WHERE issue_key='APP-1'`).Scan(&title, &epic, &manual, &created, &updated, &missingTitle, &cached, &family, &instance, &refreshed); err != nil {
		t.Fatal(err)
	}
	if title != "Manual title" || epic != "October" || manual != 7200 || created != "2026-01-01T00:00:00Z" || updated != "2026-02-01T00:00:00Z" || !missingTitle || cached != 3600 || family != "jira-cloud" || instance != "main" || refreshed != "2026-03-01T00:00:00Z" {
		t.Fatal("upgrade changed existing fields")
	}
}

func TestBootstrapReplacesDecorationsAndBackfillsIssueIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklogs.db")
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`DROP TRIGGER trg_worklogs_issue_insert`,
		`DROP TRIGGER trg_worklogs_issue_update`,
		`DROP TRIGGER trg_metadata_issue_insert`,
		`DROP TABLE issues`,
		`CREATE TABLE issue_decorations(issue_key TEXT PRIMARY KEY, title TEXT NULL, max_estimate_seconds INTEGER NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO issue_decorations VALUES('APP-1','Local title',7200,'2026-02-01T00:00:00Z')`,
		`INSERT INTO issue_metadata(issue_key,max_estimate_seconds,source_adapter_family,source_adapter_instance,refreshed_at) VALUES('APP-1',3600,'jira-cloud','main','2026-01-01T00:00:00Z')`,
		`INSERT INTO issue_metadata(issue_key,max_estimate_seconds,source_adapter_family,source_adapter_instance,refreshed_at) VALUES('APP-2',3600,'jira-cloud','main','2026-01-01T00:00:00Z')`,
		`INSERT INTO worklogs(id,issue_key,started_at_utc,duration_seconds,description,created_at,updated_at) VALUES('old','APP-3','2026-01-01T00:00:00Z',900,'work','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
	} {
		if _, err := store.DB().Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	store.Close()
	if opened, err := OpenExistingReadOnly(path); err == nil {
		opened.Close()
		t.Fatal("read-only open silently accepted incomplete schema")
	}
	store, status, err := Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if status != StatusRepaired {
		t.Fatalf("bootstrap status = %s", status)
	}
	for table, want := range map[string]int{"worklogs": 1, "issue_metadata": 2, "issues": 3} {
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s rows: %d %v", table, count, err)
		}
	}
	var title string
	var estimate int64
	if err := store.DB().QueryRow(`SELECT title,max_estimate_seconds FROM issues WHERE issue_key='APP-1'`).Scan(&title, &estimate); err != nil || title != "Local title" || estimate != 7200 {
		t.Fatalf("manual metadata lost: %s %d %v", title, estimate, err)
	}
	var oldTableCount int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_schema WHERE name='issue_decorations'`).Scan(&oldTableCount); err != nil || oldTableCount != 0 {
		t.Fatalf("obsolete table remains: %d %v", oldTableCount, err)
	}
	var before, after int
	if err := store.DB().QueryRow(`SELECT revision FROM domain_change_state WHERE id = 1`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE issues SET epic='Document project' WHERE issue_key='APP-1'`); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT revision FROM domain_change_state WHERE id = 1`).Scan(&after); err != nil || after <= before {
		t.Fatalf("issue commit did not advance tracker: %d -> %d %v", before, after, err)
	}
}

func TestWorklogIssueIdentityTriggersAreAtomicAndDurable(t *testing.T) {
	store, _, err := Bootstrap(filepath.Join(t.TempDir(), "worklogs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tx, err := store.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	const insert = `INSERT INTO worklogs(id,issue_key,started_at_utc,duration_seconds,description,created_at,updated_at) VALUES('new','APP-1','2026-01-01T00:00:00Z',900,'work','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`
	if _, err := tx.Exec(insert); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM issues`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback created issue: %d %v", count, err)
	}
	for _, statement := range []string{insert, `UPDATE worklogs SET issue_key='APP-2' WHERE id='new'`, `DELETE FROM worklogs WHERE id='new'`} {
		if _, err := store.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM issues WHERE title IS NULL AND epic IS NULL AND max_estimate_seconds IS NULL`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("issue identities not retained: %d %v", count, err)
	}
}

func TestIssueIdentityGuardsAreRequiredAndInitializationRepairsThem(t *testing.T) {
	for _, trigger := range []string{"trg_worklogs_issue_insert", "trg_worklogs_issue_update", "trg_metadata_issue_insert"} {
		t.Run(trigger, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "worklogs.db")
			store, _, err := Bootstrap(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB().Exec(`DROP TRIGGER ` + trigger); err != nil {
				t.Fatal(err)
			}
			store.Close()
			if opened, err := OpenExistingReadOnly(path); err == nil {
				opened.Close()
				t.Fatal("ordinary read accepted missing issue identity guard")
			}
			store, _, err = Bootstrap(path)
			if err != nil {
				t.Fatal(err)
			}
			store.Close()
			opened, err := OpenExistingReadOnly(path)
			if err != nil {
				t.Fatalf("explicit initialization did not repair guard: %v", err)
			}
			opened.Close()
		})
	}
}

func TestIssueCreationMigrationRetainsEarliestEvidenceAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "issues.db")
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO issues(issue_key,title,epic,max_estimate_seconds,updated_at) VALUES('APP-1','Title','October',7200,'2026-03-01T00:00:00Z'),('APP-2',NULL,NULL,NULL,'2026-02-01T00:00:00Z')`,
		`INSERT INTO worklogs(id,issue_key,started_at_utc,duration_seconds,description,created_at,updated_at) VALUES('first','APP-1','2025-12-01T09:00:00Z',900,'First','2026-01-01T00:00:00Z','2026-03-01T00:00:00Z'),('second','APP-1','2026-02-01T09:00:00Z',900,'Second','2026-02-01T00:00:00Z','2026-03-01T00:00:00Z')`,
		`INSERT INTO issue_metadata(issue_key,max_estimate_seconds,source_adapter_family,source_adapter_instance,refreshed_at) VALUES('APP-1',3600,'jira-cloud','main','2026-01-10T00:00:00Z')`,
		`INSERT INTO trashed_worklogs(id,storage_scope,issue_key,started_at_utc,duration_seconds,description,trashed_at,reason_code,reason_detail,plan_direction,source_created_at) VALUES('archived','local','APP-1','2025-12-01T00:00:00Z',900,'Archived','2026-01-01T00:00:00Z','user_deleted','','local','2025-12-15T00:00:00Z')`,
		`DROP TRIGGER trg_worklogs_issue_insert`, `DROP TRIGGER trg_worklogs_issue_update`, `DROP TRIGGER trg_metadata_issue_insert`,
		`ALTER TABLE issues DROP COLUMN created_at`,
	}
	for _, statement := range statements {
		if _, err := store.DB().Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	store.Close()
	if opened, err := OpenExistingReadOnly(path); err == nil {
		opened.Close()
		t.Fatal("outdated catalog accepted before initialization")
	}
	store, status, err := Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusRepaired {
		t.Fatal("creation migration did not report repair")
	}
	var created, title, epic string
	var estimate int
	if err := store.DB().QueryRow(`SELECT created_at,title,epic,max_estimate_seconds FROM issues WHERE issue_key='APP-1'`).Scan(&created, &title, &epic, &estimate); err != nil || created != "2025-12-15T00:00:00Z" || title != "Title" || epic != "October" || estimate != 7200 {
		t.Fatalf("migration changed metadata or ignored archived creation: %s %s %s %d %v", created, title, epic, estimate, err)
	}
	if err := store.DB().QueryRow(`SELECT created_at FROM issues WHERE issue_key='APP-2'`).Scan(&created); err != nil || created != "2026-02-01T00:00:00Z" {
		t.Fatalf("metadata-only backfill: %s %v", created, err)
	}
	for _, statement := range []string{`UPDATE issues SET updated_at='2026-10-01T00:00:00Z'`, `DELETE FROM worklogs`, `DELETE FROM trashed_worklogs`, `DELETE FROM issue_metadata`} {
		if _, err := store.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	store, _, err = Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.DB().QueryRow(`SELECT created_at FROM issues WHERE issue_key='APP-1'`).Scan(&created); err != nil || created != "2025-12-15T00:00:00Z" {
		t.Fatalf("reinitialization recomputed immutable creation: %s %v", created, err)
	}
	if _, err := store.DB().Exec(`INSERT INTO worklogs(id,issue_key,started_at_utc,duration_seconds,description,created_at,updated_at) VALUES('new','APP-4','2026-10-01T00:00:00Z',900,'New','2026-10-02T00:00:00Z','2026-10-02T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow(`SELECT created_at FROM issues WHERE issue_key='APP-4'`).Scan(&created); err != nil || created != "2026-10-02T00:00:00Z" {
		t.Fatalf("migrated trigger omitted creation: %s %v", created, err)
	}
}

func TestCatalogCreationUsesArchivedEvidenceWithoutAddingTrashOnlyIssues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "issues.db")
	store, _, err := Bootstrap(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`DROP TRIGGER trg_worklogs_issue_insert`, `DROP TRIGGER trg_worklogs_issue_update`, `DROP TRIGGER trg_metadata_issue_insert`,
		`DROP TABLE issues`,
		`INSERT INTO worklogs(id,issue_key,started_at_utc,duration_seconds,description,created_at,updated_at) VALUES('current','APP-1','2026-05-01T08:00:00Z',900,'Current','2026-05-01T00:00:00Z','2026-05-01T00:00:00Z')`,
		`INSERT INTO issue_metadata(issue_key,max_estimate_seconds,source_adapter_family,source_adapter_instance,refreshed_at) VALUES('APP-2',3600,'jira-cloud','main','2026-04-01T00:00:00Z')`,
		`INSERT INTO trashed_worklogs(id,storage_scope,issue_key,started_at_utc,duration_seconds,description,trashed_at,reason_code,reason_detail,plan_direction,source_created_at) VALUES
		('active-history','local','APP-1','2025-01-01T00:00:00Z',900,'Archived','2026-01-01T00:00:00Z','user_deleted','','local','2025-01-01T00:00:00Z'),
		('cache-history','local','APP-2','2025-02-01T00:00:00Z',900,'Archived','2026-01-01T00:00:00Z','user_deleted','','local','2025-02-01T00:00:00Z'),
		('trash-only','local','APP-3','2025-01-01T00:00:00Z',900,'Archived','2026-01-01T00:00:00Z','user_deleted','','local','2025-01-01T00:00:00Z'),
		('remote-history','remote','APP-1','2024-01-01T00:00:00Z',900,'Remote audit','2026-01-01T00:00:00Z','remote_deleted','','push','2024-01-01T00:00:00Z')`,
	} {
		if _, err := store.DB().Exec(statement); err != nil {
			store.Close()
			t.Fatalf("%s: %v", statement, err)
		}
	}
	store.Close()
	for iteration := 0; iteration < 2; iteration++ {
		store, _, err = Bootstrap(path)
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{"APP-1": "2025-01-01T00:00:00Z", "APP-2": "2025-02-01T00:00:00Z"} {
			var created string
			if err := store.DB().QueryRow(`SELECT created_at FROM issues WHERE issue_key=?`, key).Scan(&created); err != nil || created != want {
				store.Close()
				t.Fatalf("%s creation = %s, want %s: %v", key, created, want, err)
			}
		}
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(*) FROM issues`).Scan(&count); err != nil || count != 2 {
			store.Close()
			t.Fatalf("trash established issue identity: %d issues, %v", count, err)
		}
		if _, err := store.DB().Exec(`DELETE FROM trashed_worklogs`); err != nil {
			store.Close()
			t.Fatal(err)
		}
		store.Close()
	}
}
