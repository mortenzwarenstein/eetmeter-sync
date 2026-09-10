package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every embedded migration not yet recorded in schema_migrations,
// each inside its own transaction, in ascending numeric order. It is safe to call
// on every startup.
func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    integer PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return fmt.Errorf("store: ensure schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("store: read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version, err := versionOf(name)
		if err != nil {
			return err
		}

		var applied bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version,
		).Scan(&applied); err != nil {
			return fmt.Errorf("store: check migration %d: %w", version, err)
		}
		if applied {
			continue
		}

		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", name, err)
		}

		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("store: begin migration %d: %w", version, err)
		}
		for _, stmt := range splitStatements(string(body)) {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("store: migration %d failed on statement %q: %w", version, first80(stmt), err)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("store: record migration %d: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("store: commit migration %d: %w", version, err)
		}
	}
	return nil
}

// versionOf parses the leading integer of a "NNNN_name.sql" filename.
func versionOf(name string) (int, error) {
	i := strings.IndexByte(name, '_')
	if i <= 0 {
		return 0, fmt.Errorf("store: migration %q must be named <version>_<name>.sql", name)
	}
	v, err := strconv.Atoi(name[:i])
	if err != nil {
		return 0, fmt.Errorf("store: migration %q has a non-numeric version: %w", name, err)
	}
	return v, nil
}

// splitStatements strips line comments from the whole file first, then breaks it
// into statements on semicolons. Migrations must therefore not use ';' or '--'
// for any purpose other than statement termination and comments (see the note at
// the top of 0001_init.sql).
func splitStatements(sql string) []string {
	sql = stripSQLComments(sql)
	var out []string
	for _, part := range strings.Split(sql, ";") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func stripSQLComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func first80(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
