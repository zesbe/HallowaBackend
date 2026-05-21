package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Open opens (and migrates) the whatsmeow SQLite store at the given path.
// Creates parent dir if needed.
func Open(ctx context.Context, dbPath string) (*sqlstore.Container, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir store dir: %w", err)
	}
	dsn := "file:" + dbPath + "?_foreign_keys=on&_pragma=journal_mode(WAL)"
	dbLog := waLog.Stdout("Database", "INFO", true)
	c, err := sqlstore.New(ctx, "sqlite3", dsn, dbLog)
	if err != nil {
		return nil, fmt.Errorf("open whatsmeow store: %w", err)
	}
	// sanity ping
	if db, ok := dbHandleFromContainer(c); ok {
		if err := db.PingContext(ctx); err != nil {
			return nil, fmt.Errorf("ping store: %w", err)
		}
	}
	return c, nil
}

// dbHandleFromContainer is a soft accessor; whatsmeow exposes its own DB internally.
// We don't strictly need it, but keep this hook in case we want to run migrations later.
func dbHandleFromContainer(_ *sqlstore.Container) (*sql.DB, bool) {
	return nil, false
}
