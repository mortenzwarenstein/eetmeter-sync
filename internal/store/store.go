// Package store is the PostgreSQL persistence for recipe sync: the account rows,
// the cross-account recipe links with their last-synced baselines, the run
// history, and pending conflict resolutions. It holds queries only — the sync
// decisions live in package syncengine.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned by the single-row getters when nothing matches.
var ErrNotFound = errors.New("store: not found")

// Link statuses.
const (
	StatusActive   = "active"
	StatusConflict = "conflict"
	StatusRetired  = "retired"
)

// Store is a handle to the database. Safe for concurrent use.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects, verifies the connection, and returns a Store.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases all connections.
func (s *Store) Close() { s.pool.Close() }

// Ping checks the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// --- accounts -------------------------------------------------------------------

// Account mirrors one row of the account table. The password is never stored.
type Account struct {
	ID    string
	Label string
	Email string
}

// UpsertAccount inserts or updates one account row by id.
func (s *Store) UpsertAccount(ctx context.Context, a Account) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO account (id, label, email)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET label = EXCLUDED.label, email = EXCLUDED.email, updated_at = now()
	`, a.ID, a.Label, a.Email)
	if err != nil {
		return fmt.Errorf("store: upsert account %s: %w", a.ID, err)
	}
	return nil
}

// --- recipe links -------------------------------------------------------------

// Link is one row of recipe_link: a recipe name mapped across both accounts,
// with the fingerprint of each side as of the last successful sync. While
// Status is "conflict" the two *ConflictSnapshot fields hold the JSON of each
// side's recipe at flag time, for the review endpoint; they are NULL otherwise.
type Link struct {
	ID                 int64
	Name               string
	ARecipeID          string // "" means the recipe does not exist in account A
	BRecipeID          string
	ABaselineHash      []byte
	BBaselineHash      []byte
	Status             string
	LastSyncedAt       *time.Time
	ConflictDetectedAt *time.Time
	Note               string
	AConflictSnapshot  []byte
	BConflictSnapshot  []byte
}

const linkColumns = `
	id, name, coalesce(a_recipe_id, ''), coalesce(b_recipe_id, ''),
	a_baseline_hash, b_baseline_hash, status, last_synced_at,
	conflict_detected_at, note, a_conflict_snapshot, b_conflict_snapshot`

func scanLink(row pgx.Row) (Link, error) {
	var l Link
	err := row.Scan(&l.ID, &l.Name, &l.ARecipeID, &l.BRecipeID,
		&l.ABaselineHash, &l.BBaselineHash, &l.Status, &l.LastSyncedAt,
		&l.ConflictDetectedAt, &l.Note, &l.AConflictSnapshot, &l.BConflictSnapshot)
	return l, err
}

// ListLinks returns every link, ordered by name.
func (s *Store) ListLinks(ctx context.Context) ([]Link, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+linkColumns+` FROM recipe_link ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: list links: %w", err)
	}
	defer rows.Close()

	var out []Link
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetLink returns one link by name.
func (s *Store) GetLink(ctx context.Context, name string) (Link, error) {
	l, err := scanLink(s.pool.QueryRow(ctx, `SELECT `+linkColumns+` FROM recipe_link WHERE name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	if err != nil {
		return Link{}, fmt.Errorf("store: get link %q: %w", name, err)
	}
	return l, nil
}

// UpsertLink inserts or updates a link by name and returns it with its id.
// Empty ARecipeID/BRecipeID are stored as NULL; a "" status defaults to active.
func (s *Store) UpsertLink(ctx context.Context, l Link) (Link, error) {
	if l.Status == "" {
		l.Status = StatusActive
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO recipe_link
			(name, a_recipe_id, b_recipe_id, a_baseline_hash, b_baseline_hash,
			 status, last_synced_at, conflict_detected_at, note,
			 a_conflict_snapshot, b_conflict_snapshot)
		VALUES ($1, nullif($2,''), nullif($3,''), $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (name) DO UPDATE SET
			a_recipe_id = nullif(excluded.a_recipe_id, ''),
			b_recipe_id = nullif(excluded.b_recipe_id, ''),
			a_baseline_hash = excluded.a_baseline_hash,
			b_baseline_hash = excluded.b_baseline_hash,
			status = excluded.status,
			last_synced_at = excluded.last_synced_at,
			conflict_detected_at = excluded.conflict_detected_at,
			note = excluded.note,
			a_conflict_snapshot = excluded.a_conflict_snapshot,
			b_conflict_snapshot = excluded.b_conflict_snapshot,
			updated_at = now()
		RETURNING id
	`, l.Name, l.ARecipeID, l.BRecipeID, l.ABaselineHash, l.BBaselineHash,
		l.Status, l.LastSyncedAt, l.ConflictDetectedAt, l.Note,
		l.AConflictSnapshot, l.BConflictSnapshot).Scan(&l.ID)
	if err != nil {
		return Link{}, fmt.Errorf("store: upsert link %q: %w", l.Name, err)
	}
	return l, nil
}

// SetLinkStatus changes only the status and note of a link, e.g. retiring it.
func (s *Store) SetLinkStatus(ctx context.Context, id int64, status, note string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE recipe_link SET status = $2, note = $3, updated_at = now() WHERE id = $1
	`, id, status, note)
	if err != nil {
		return fmt.Errorf("store: set link %d status: %w", id, err)
	}
	return nil
}

// ConflictRow is one unresolved conflict for the review endpoint. The snapshots
// are raw JSON of an eetmeter.Recipe captured when the conflict was flagged.
type ConflictRow struct {
	LinkID     int64
	Name       string
	DetectedAt *time.Time
	ARecipeID  string
	BRecipeID  string
	ASnapshot  []byte
	BSnapshot  []byte
	Pending    string // "", "a", or "b"
}

// ListConflicts returns every link currently in conflict, with any pending
// resolution.
func (s *Store) ListConflicts(ctx context.Context) ([]ConflictRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.id, l.name, l.conflict_detected_at,
		       coalesce(l.a_recipe_id, ''), coalesce(l.b_recipe_id, ''),
		       l.a_conflict_snapshot, l.b_conflict_snapshot,
		       coalesce(cr.winner, '')
		FROM recipe_link l
		LEFT JOIN conflict_resolution cr ON cr.link_id = l.id
		WHERE l.status = 'conflict'
		ORDER BY l.name
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list conflicts: %w", err)
	}
	defer rows.Close()

	var out []ConflictRow
	for rows.Next() {
		var c ConflictRow
		if err := rows.Scan(&c.LinkID, &c.Name, &c.DetectedAt, &c.ARecipeID, &c.BRecipeID,
			&c.ASnapshot, &c.BSnapshot, &c.Pending); err != nil {
			return nil, fmt.Errorf("store: scan conflict: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- conflict resolutions ---------------------------------------------------------

// ListResolutions returns the pending resolutions as link id -> winner ("a"/"b").
func (s *Store) ListResolutions(ctx context.Context) (map[int64]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT link_id, winner FROM conflict_resolution`)
	if err != nil {
		return nil, fmt.Errorf("store: list resolutions: %w", err)
	}
	defer rows.Close()

	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var winner string
		if err := rows.Scan(&id, &winner); err != nil {
			return nil, fmt.Errorf("store: scan resolution: %w", err)
		}
		out[id] = winner
	}
	return out, rows.Err()
}

// UpsertResolution records (or replaces) the pending resolution for a link. It
// returns ErrNotFound if the link is not currently in conflict.
func (s *Store) UpsertResolution(ctx context.Context, linkID int64, winner string) error {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO conflict_resolution (link_id, winner)
		SELECT $1, $2 FROM recipe_link WHERE id = $1 AND status = 'conflict'
		ON CONFLICT (link_id) DO UPDATE SET winner = excluded.winner, requested_at = now()
	`, linkID, winner)
	if err != nil {
		return fmt.Errorf("store: upsert resolution for link %d: %w", linkID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ApplyResolution atomically writes the post-resolution link state (active, no
// conflict, no snapshots) and deletes the pending resolution row.
func (s *Store) ApplyResolution(ctx context.Context, l Link) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin apply resolution: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit

	if _, err := tx.Exec(ctx, `
		UPDATE recipe_link SET
			a_recipe_id = nullif($2,''), b_recipe_id = nullif($3,''),
			a_baseline_hash = $4, b_baseline_hash = $5,
			status = 'active', last_synced_at = $6, conflict_detected_at = NULL,
			note = '', a_conflict_snapshot = NULL, b_conflict_snapshot = NULL,
			updated_at = now()
		WHERE id = $1
	`, l.ID, l.ARecipeID, l.BRecipeID, l.ABaselineHash, l.BBaselineHash, l.LastSyncedAt); err != nil {
		return fmt.Errorf("store: apply resolution link %d: %w", l.ID, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM conflict_resolution WHERE link_id = $1`, l.ID); err != nil {
		return fmt.Errorf("store: clear resolution link %d: %w", l.ID, err)
	}
	return tx.Commit(ctx)
}

// --- runs -------------------------------------------------------------------

// Run mirrors one row of sync_run.
type Run struct {
	ID         string
	Trigger    string
	StartedAt  time.Time
	FinishedAt *time.Time
	Status     string
	Error      string
	Summary    []byte // raw jsonb
}

// StartRun inserts a 'running' row. It fails if another run is already running.
func (s *Store) StartRun(ctx context.Context, id, trigger string, startedAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sync_run (id, trigger, started_at, status, summary)
		VALUES ($1, $2, $3, 'running', '{}'::jsonb)
	`, id, trigger, startedAt)
	if err != nil {
		return fmt.Errorf("store: start run: %w", err)
	}
	return nil
}

// SaveRunSummary updates the summary of a still-running run (progress).
func (s *Store) SaveRunSummary(ctx context.Context, id string, summary []byte) error {
	if _, err := s.pool.Exec(ctx, `UPDATE sync_run SET summary = $2 WHERE id = $1`, id, summary); err != nil {
		return fmt.Errorf("store: save run summary: %w", err)
	}
	return nil
}

// FinishRun sets the terminal status, finish time, final summary, and an
// optional error message.
func (s *Store) FinishRun(ctx context.Context, id, status, errMsg string, summary []byte, finishedAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sync_run SET status = $2, error = nullif($3,''), summary = $4, finished_at = $5 WHERE id = $1
	`, id, status, errMsg, summary, finishedAt)
	if err != nil {
		return fmt.Errorf("store: finish run: %w", err)
	}
	return nil
}

// LatestRun returns the most recently started run, or ErrNotFound.
func (s *Store) LatestRun(ctx context.Context) (Run, error) {
	var r Run
	err := s.pool.QueryRow(ctx, `
		SELECT id, trigger, started_at, finished_at, status, coalesce(error, ''), summary
		FROM sync_run ORDER BY started_at DESC LIMIT 1
	`).Scan(&r.ID, &r.Trigger, &r.StartedAt, &r.FinishedAt, &r.Status, &r.Error, &r.Summary)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("store: latest run: %w", err)
	}
	return r, nil
}

// ClearStaleRunningRuns marks any 'running' rows left by a crash as failed.
// Called once at startup before the first new run.
func (s *Store) ClearStaleRunningRuns(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sync_run SET status = 'failed', error = 'interrupted (process restarted)', finished_at = now()
		WHERE status = 'running'
	`)
	if err != nil {
		return fmt.Errorf("store: clear stale running runs: %w", err)
	}
	return nil
}
