package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type localStorageError struct {
	SQLitePath string
	ParentDir  string
	Operation  string
	Detail     string
}

func (e *localStorageError) Error() string {
	switch e.Detail {
	case "parent directory is not writable":
		return fmt.Sprintf("cannot write local ledger at %s: parent directory %s is not writable", e.SQLitePath, e.ParentDir)
	case "sqlite file is not writable":
		return fmt.Sprintf("cannot write local ledger at %s: SQLite file is not writable", e.SQLitePath)
	default:
		return fmt.Sprintf("cannot write local ledger at %s: %s", e.SQLitePath, e.Detail)
	}
}

func checkLocalStorageWritable(sqlitePath, operation string) error {
	parentDir := filepath.Dir(sqlitePath)

	info, err := os.Stat(parentDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return &localStorageError{
			SQLitePath: sqlitePath,
			ParentDir:  parentDir,
			Operation:  operation,
			Detail:     fmt.Sprintf("cannot inspect parent directory: %v", err),
		}
	}
	if !info.IsDir() {
		return &localStorageError{
			SQLitePath: sqlitePath,
			ParentDir:  parentDir,
			Operation:  operation,
			Detail:     "parent path is not a directory",
		}
	}

	if _, err := os.Stat(sqlitePath); err == nil {
		file, openErr := os.OpenFile(sqlitePath, os.O_WRONLY|os.O_APPEND, 0)
		if openErr != nil {
			return &localStorageError{
				SQLitePath: sqlitePath,
				ParentDir:  parentDir,
				Operation:  operation,
				Detail:     "sqlite file is not writable",
			}
		}
		_ = file.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return &localStorageError{
			SQLitePath: sqlitePath,
			ParentDir:  parentDir,
			Operation:  operation,
			Detail:     fmt.Sprintf("cannot inspect SQLite file: %v", err),
		}
	}

	probe, err := os.CreateTemp(parentDir, ".workledger-write-check-*")
	if err != nil {
		return &localStorageError{
			SQLitePath: sqlitePath,
			ParentDir:  parentDir,
			Operation:  operation,
			Detail:     "parent directory is not writable",
		}
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())

	return nil
}
