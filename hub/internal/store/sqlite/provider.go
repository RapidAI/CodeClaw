package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Config struct {
	DSN               string
	WAL               bool
	BusyTimeoutMS     int
	MaxReadOpenConns  int
	MaxReadIdleConns  int
	MaxWriteOpenConns int
	MaxWriteIdleConns int
}

type Provider struct {
	Write *sql.DB
	Read  *sql.DB
}

func NewProvider(cfg Config) (*Provider, error) {
	if err := ensureParentDir(cfg.DSN); err != nil {
		return nil, err
	}

	writeDB, err := sql.Open("sqlite", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}

	readDB, err := sql.Open("sqlite", cfg.DSN)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open read db: %w", err)
	}

	writeDB.SetMaxOpenConns(cfg.MaxWriteOpenConns)
	writeDB.SetMaxIdleConns(cfg.MaxWriteIdleConns)
	writeDB.SetConnMaxLifetime(30 * time.Minute)

	readDB.SetMaxOpenConns(cfg.MaxReadOpenConns)
	readDB.SetMaxIdleConns(cfg.MaxReadIdleConns)
	readDB.SetConnMaxLifetime(30 * time.Minute)

	if err := applyPragmas(writeDB, cfg); err != nil {
		return nil, err
	}
	if err := applyPragmas(readDB, cfg); err != nil {
		return nil, err
	}

	return &Provider{
		Write: writeDB,
		Read:  readDB,
	}, nil
}

func applyPragmas(db *sql.DB, cfg Config) error {
	stmts := []string{
		"PRAGMA foreign_keys = ON;",
		fmt.Sprintf("PRAGMA busy_timeout = %d;", cfg.BusyTimeoutMS),
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA temp_store = MEMORY;",
	}
	if cfg.WAL {
		stmts = append(stmts, "PRAGMA journal_mode = WAL;")
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("apply pragma %q: %w", stmt, err)
		}
	}
	return nil
}

func ensureParentDir(dsn string) error {
	if dsn == "" || dsn == ":memory:" {
		return nil
	}

	parent := filepath.Dir(dsn)
	if parent == "." || parent == "" {
		return nil
	}

	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create sqlite data dir: %w", err)
	}
	return nil
}
