package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	termduty "termduty"
	"termduty/internal/domain"
	"termduty/internal/store/shard"

	_ "modernc.org/sqlite"
)

// OpenDB opens the embedded SQLite database at dbPath with write-ahead logging
// and immediate transactions so concurrent writers serialise cleanly under load.
func OpenDB(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)&_txlock=immediate", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate applies every embedded migration whose version is greater than the
// version recorded in schema_meta. Each migration runs in its own transaction
// and bumps the recorded version, so a crash mid-way leaves the database at the
// last fully applied version.
func Migrate(ctx context.Context, db *sql.DB, log *slog.Logger) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	current, err := schemaVersion(ctx, db)
	if err != nil {
		return err
	}
	entries, err := fs.ReadDir(termduty.Migrations, "migrations")
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		if version <= current {
			continue
		}
		script, err := termduty.Migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if err := applyMigration(ctx, db, version, string(script)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		log.Info("migration applied", "version", version, "file", name)
	}
	return nil
}

func schemaVersion(ctx context.Context, db *sql.DB) (int64, error) {
	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM schema_meta WHERE key = 'version'`).Scan(&value)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(value, 10, 64)
}

func migrationVersion(name string) (int64, error) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid migration filename %q", name)
	}
	return strconv.ParseInt(parts[0], 10, 64)
}

func applyMigration(ctx context.Context, db *sql.DB, version int64, script string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, stmt := range splitStatements(script) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE schema_meta SET value = ?, updated_at = ? WHERE key = 'version'`,
		strconv.FormatInt(version, 10), timestamp(domain.RealClock{}.Now())); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func splitStatements(script string) []string {
	var out []string
	var depth int
	var last int
	inSingle := false
	for i := 0; i < len(script); i++ {
		c := script[i]
		switch c {
		case '\'':
			inSingle = !inSingle
		case '(':
			if !inSingle {
				depth++
			}
		case ')':
			if !inSingle && depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 && !inSingle {
				out = append(out, script[last:i])
				last = i + 1
			}
		}
	}
	if last < len(script) {
		out = append(out, script[last:])
	}
	return out
}

// New opens the database, runs migrations, and prepares the shard store.
func New(ctx context.Context, dbPath, shardDir string, clock domain.Clock, log *slog.Logger) (*Store, error) {
	db, err := OpenDB(dbPath)
	if err != nil {
		return nil, err
	}
	if err := Migrate(ctx, db, log); err != nil {
		db.Close()
		return nil, err
	}
	shards, err := shard.New(shardDir)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, shards: shards, clock: clock, log: log}, nil
}

func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// Version returns the applied schema version, for diagnostics and readiness.
func (s *Store) Version(ctx context.Context) (int64, error) {
	return schemaVersion(ctx, s.db)
}
