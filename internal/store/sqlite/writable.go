package sqlite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type WritableError struct {
	SQLitePath string
	ParentDir  string
	Operation  string
	Detail     string
}

func (e *WritableError) Error() string {
	switch e.Detail {
	case "parent directory is not writable":
		return fmt.Sprintf("cannot write local ledger at %s: parent directory %s is not writable", e.SQLitePath, e.ParentDir)
	case "sqlite file is not writable":
		return fmt.Sprintf("cannot write local ledger at %s: SQLite file is not writable", e.SQLitePath)
	default:
		return fmt.Sprintf("cannot write local ledger at %s: %s", e.SQLitePath, e.Detail)
	}
}

func CheckWritable(sqlitePath, operation string) error {
	parentDir := filepath.Dir(sqlitePath)

	info, err := os.Stat(parentDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return writableError(sqlitePath, parentDir, operation, fmt.Sprintf("cannot inspect parent directory: %v", err))
	}
	if !info.IsDir() {
		return writableError(sqlitePath, parentDir, operation, "parent path is not a directory")
	}

	if sqliteInfo, err := os.Stat(sqlitePath); err == nil {
		// Never open the database outside SQLite. On POSIX, closing an unrelated
		// descriptor for the database can release locks held by SQLite connections
		// elsewhere in this process.
		if sqliteInfo.Mode().Perm()&0o222 == 0 {
			return writableError(sqlitePath, parentDir, operation, "sqlite file is not writable")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return writableError(sqlitePath, parentDir, operation, fmt.Sprintf("cannot inspect SQLite file: %v", err))
	}

	probe, err := os.CreateTemp(parentDir, ".workledger-write-check-*")
	if err != nil {
		return writableError(sqlitePath, parentDir, operation, "parent directory is not writable")
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return nil
}

func writableError(sqlitePath, parentDir, operation, detail string) error {
	return &WritableError{
		SQLitePath: sqlitePath,
		ParentDir:  parentDir,
		Operation:  operation,
		Detail:     detail,
	}
}
