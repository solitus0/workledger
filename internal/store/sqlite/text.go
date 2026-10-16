package sqlite

import (
	"database/sql/driver"
	"strings"

	sqlitedriver "modernc.org/sqlite"
)

func init() {
	// SQLite's built-in lower only folds ASCII. Match the Go lowercase mapping
	// used by the local catalog while keeping filtering ahead of SQL pagination.
	sqlitedriver.MustRegisterDeterministicScalarFunction("workledger_lower", 1, func(_ *sqlitedriver.FunctionContext, args []driver.Value) (driver.Value, error) {
		if value, ok := args[0].(string); ok {
			return strings.ToLower(value), nil
		}
		return nil, nil
	})
}
