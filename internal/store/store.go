// Package store implements the persistent memory engine for Postgram.
//
// It uses PostgreSQL to store and retrieve observations from AI coding sessions.
// This is the core of Postgram —
// everything else (HTTP server, MCP server, CLI, TUI) talks to this.
package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

var openDB = sql.Open

// ─── Types ───────────────────────────────────────────────────────────────────

type Session struct {
	ID              string  `json:"id"`
	ClientSessionID string  `json:"client_session_id,omitempty"`
	Project         string  `json:"project"`
	Directory       string  `json:"directory"`
	AuthIssuer      *string `json:"auth_issuer,omitempty"`
	AuthSubject     *string `json:"auth_subject,omitempty"`
	AuthUsername    *string `json:"auth_username,omitempty"`
	AuthEmail       *string `json:"auth_email,omitempty"`
	StartedAt       string  `json:"started_at"`
	EndedAt         *string `json:"ended_at,omitempty"`
	Summary         *string `json:"summary,omitempty"`
}

type SessionAuthor struct {
	Issuer   string `json:"issuer,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`
}

type CreateSessionParams struct {
	ClientSessionID string         `json:"client_session_id"`
	Project         string         `json:"project"`
	Directory       string         `json:"directory"`
	Author          *SessionAuthor `json:"author,omitempty"`
}

func (p CreateSessionParams) EffectiveID() string {
	clientID := strings.TrimSpace(p.ClientSessionID)
	if clientID == "" {
		clientID = "manual-save"
	}
	if p.Author == nil {
		return uuidFromStableText("client|" + clientID)
	}
	issuer := strings.TrimSpace(p.Author.Issuer)
	subject := strings.TrimSpace(p.Author.Subject)
	if issuer == "" || subject == "" {
		return uuidFromStableText("client|" + clientID)
	}
	return uuidFromStableText("auth|" + issuer + "|" + subject + "|" + clientID)
}

func (p CreateSessionParams) EffectiveSessionID() string {
	return p.EffectiveID()
}

func uuidFromStableText(value string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.TrimSpace(value))).String()
}

func (p CreateSessionParams) AuthorField(field string) string {
	if p.Author == nil {
		return ""
	}
	switch field {
	case "issuer":
		return p.Author.Issuer
	case "subject":
		return p.Author.Subject
	case "username":
		return p.Author.Username
	case "email":
		return p.Author.Email
	default:
		return ""
	}
}

type Observation struct {
	ID             string  `json:"id"`
	SyncID         string  `json:"sync_id"`
	SessionID      string  `json:"session_id"`
	Type           string  `json:"type"`
	Title          string  `json:"title"`
	Content        string  `json:"content"`
	ToolName       *string `json:"tool_name,omitempty"`
	Project        *string `json:"project,omitempty"`
	Scope          string  `json:"scope"`
	TopicKey       *string `json:"topic_key,omitempty"`
	RevisionCount  int     `json:"revision_count"`
	DuplicateCount int     `json:"duplicate_count"`
	LastSeenAt     *string `json:"last_seen_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	DeletedAt      *string `json:"deleted_at,omitempty"`
}

type SearchResult struct {
	Observation
	Rank float64 `json:"rank"`
}

type SessionSummary struct {
	ID               string  `json:"id"`
	ClientSessionID  string  `json:"client_session_id,omitempty"`
	Project          string  `json:"project"`
	AuthIssuer       *string `json:"auth_issuer,omitempty"`
	AuthSubject      *string `json:"auth_subject,omitempty"`
	AuthUsername     *string `json:"auth_username,omitempty"`
	AuthEmail        *string `json:"auth_email,omitempty"`
	StartedAt        string  `json:"started_at"`
	EndedAt          *string `json:"ended_at,omitempty"`
	Summary          *string `json:"summary,omitempty"`
	ObservationCount int     `json:"observation_count"`
}

type Stats struct {
	TotalSessions     int      `json:"total_sessions"`
	TotalObservations int      `json:"total_observations"`
	TotalPrompts      int      `json:"total_prompts"`
	Projects          []string `json:"projects"`
}

type TimelineEntry struct {
	ID             string  `json:"id"`
	SessionID      string  `json:"session_id"`
	Type           string  `json:"type"`
	Title          string  `json:"title"`
	Content        string  `json:"content"`
	ToolName       *string `json:"tool_name,omitempty"`
	Project        *string `json:"project,omitempty"`
	Scope          string  `json:"scope"`
	TopicKey       *string `json:"topic_key,omitempty"`
	RevisionCount  int     `json:"revision_count"`
	DuplicateCount int     `json:"duplicate_count"`
	LastSeenAt     *string `json:"last_seen_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	DeletedAt      *string `json:"deleted_at,omitempty"`
	IsFocus        bool    `json:"is_focus"` // true for the anchor observation
}

type TimelineResult struct {
	Focus        Observation     `json:"focus"`        // The anchor observation
	Before       []TimelineEntry `json:"before"`       // Observations before the focus (chronological)
	After        []TimelineEntry `json:"after"`        // Observations after the focus (chronological)
	SessionInfo  *Session        `json:"session_info"` // Session that contains the focus observation
	TotalInRange int             `json:"total_in_range"`
}

type SearchOptions struct {
	Type    string `json:"type,omitempty"`
	Project string `json:"project,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type AddObservationParams struct {
	SessionID string `json:"session_id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	ToolName  string `json:"tool_name,omitempty"`
	Project   string `json:"project,omitempty"`
	Scope     string `json:"scope,omitempty"`
	TopicKey  string `json:"topic_key,omitempty"`
}

type UpdateObservationParams struct {
	Type     *string `json:"type,omitempty"`
	Title    *string `json:"title,omitempty"`
	Content  *string `json:"content,omitempty"`
	Project  *string `json:"project,omitempty"`
	Scope    *string `json:"scope,omitempty"`
	TopicKey *string `json:"topic_key,omitempty"`
}

type Prompt struct {
	ID        string `json:"id"`
	SyncID    string `json:"sync_id"`
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Project   string `json:"project,omitempty"`
	CreatedAt string `json:"created_at"`
}

type AddPromptParams struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Project   string `json:"project,omitempty"`
}

var ErrSessionAuthorConflict = errors.New("session already belongs to a different authenticated user")

// ExportData is the full serializable dump of the postgram database.
type ExportData struct {
	Version      string        `json:"version"`
	ExportedAt   string        `json:"exported_at"`
	Sessions     []Session     `json:"sessions"`
	Observations []Observation `json:"observations"`
	Prompts      []Prompt      `json:"prompts"`
}

// ─── Config ──────────────────────────────────────────────────────────────────

type Config struct {
	Driver               string
	DatabaseURL          string
	DataDir              string
	MaxObservationLength int
	MaxContextResults    int
	MaxSearchResults     int
	DedupeWindow         time.Duration
}

func DefaultConfig() (Config, error) {
	return Config{
		Driver:               "postgres",
		DatabaseURL:          "",
		DataDir:              "",
		MaxObservationLength: 50000,
		MaxContextResults:    20,
		MaxSearchResults:     20,
		DedupeWindow:         15 * time.Minute,
	}, nil
}

// FallbackConfig returns a Config with the given DataDir and default values.
// Use this when DefaultConfig fails and you have resolved the home directory
// through alternative means.
func FallbackConfig(dataDir string) Config {
	return Config{
		Driver:               "postgres",
		DatabaseURL:          "",
		DataDir:              dataDir,
		MaxObservationLength: 50000,
		MaxContextResults:    20,
		MaxSearchResults:     20,
		DedupeWindow:         15 * time.Minute,
	}
}

// MaxObservationLength returns the configured maximum content length for observations.
func (s *Store) MaxObservationLength() int {
	return s.cfg.MaxObservationLength
}

// ─── Store ───────────────────────────────────────────────────────────────────

type Store struct {
	db     *sql.DB
	cfg    Config
	hooks  storeHooks
	driver string
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

type rowQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

type sqlRowScanner struct {
	rows *sql.Rows
}

func (r sqlRowScanner) Next() bool {
	return r.rows.Next()
}

func (r sqlRowScanner) Scan(dest ...any) error {
	return r.rows.Scan(dest...)
}

func (r sqlRowScanner) Err() error {
	return r.rows.Err()
}

func (r sqlRowScanner) Close() error {
	return r.rows.Close()
}

type storeHooks struct {
	exec     func(db execer, query string, args ...any) (sql.Result, error)
	query    func(db queryer, query string, args ...any) (*sql.Rows, error)
	queryRow func(db rowQueryer, query string, args ...any) *sql.Row
	queryIt  func(db queryer, query string, args ...any) (rowScanner, error)
	beginTx  func(db *sql.DB) (*sql.Tx, error)
	commit   func(tx *sql.Tx) error
}

func defaultStoreHooks() storeHooks {
	return storeHooks{
		exec: func(db execer, query string, args ...any) (sql.Result, error) {
			return db.Exec(query, args...)
		},
		query: func(db queryer, query string, args ...any) (*sql.Rows, error) {
			return db.Query(query, args...)
		},
		queryRow: func(db rowQueryer, query string, args ...any) *sql.Row {
			return db.QueryRow(query, args...)
		},
		queryIt: func(db queryer, query string, args ...any) (rowScanner, error) {
			rows, err := db.Query(query, args...)
			if err != nil {
				return nil, err
			}
			return sqlRowScanner{rows: rows}, nil
		},
		beginTx: func(db *sql.DB) (*sql.Tx, error) {
			return db.Begin()
		},
		commit: func(tx *sql.Tx) error {
			return tx.Commit()
		},
	}
}

func (s *Store) execHook(db execer, query string, args ...any) (sql.Result, error) {
	query = s.rebind(query)
	if s.hooks.exec != nil {
		return s.hooks.exec(db, query, args...)
	}
	return db.Exec(query, args...)
}

func (s *Store) queryHook(db queryer, query string, args ...any) (*sql.Rows, error) {
	query = s.rebind(query)
	if s.hooks.query != nil {
		return s.hooks.query(db, query, args...)
	}
	return db.Query(query, args...)
}

func (s *Store) queryRowHook(db rowQueryer, query string, args ...any) *sql.Row {
	query = s.rebind(query)
	if s.hooks.queryRow != nil {
		return s.hooks.queryRow(db, query, args...)
	}
	return db.QueryRow(query, args...)
}

func (s *Store) queryItHook(db queryer, query string, args ...any) (rowScanner, error) {
	if s.hooks.queryIt != nil {
		return s.hooks.queryIt(db, s.rebind(query), args...)
	}
	rows, err := s.queryHook(db, query, args...)
	if err != nil {
		return nil, err
	}
	return sqlRowScanner{rows: rows}, nil
}

func (s *Store) beginTxHook() (*sql.Tx, error) {
	if s.hooks.beginTx != nil {
		return s.hooks.beginTx(s.db)
	}
	return s.db.Begin()
}

func (s *Store) commitHook(tx *sql.Tx) error {
	if s.hooks.commit != nil {
		return s.hooks.commit(tx)
	}
	return tx.Commit()
}

func (s *Store) isPostgres() bool {
	return true
}

func (s *Store) rebind(query string) string {
	if !s.isPostgres() {
		return query
	}

	q := query
	var b strings.Builder
	b.Grow(len(q) + 8)
	index := 1
	for _, ch := range q {
		if ch == '?' {
			b.WriteString("$")
			b.WriteString(strconv.Itoa(index))
			index++
			continue
		}
		b.WriteRune(ch)
	}
	return b.String()
}

func New(cfg Config) (*Store, error) {
	driver := strings.TrimSpace(strings.ToLower(cfg.Driver))
	if driver == "" {
		driver = "postgres"
	}

	if driver != "postgres" && driver != "postgresql" {
		return nil, fmt.Errorf("postgram: sqlite support has been removed; use postgres and set POSTGRAM_DATABASE_URL")
	}
	driver = "postgres"
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return nil, fmt.Errorf("postgram: POSTGRAM_DATABASE_URL is required")
	}
	db, err := openDB("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgram: open postgres database: %w", err)
	}

	s := &Store{db: db, cfg: cfg, hooks: defaultStoreHooks(), driver: driver}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgram: migration: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// ─── Migrations ──────────────────────────────────────────────────────────────

func (s *Store) migrate() error {
	return s.migratePostgres()
}

func (s *Store) migratePostgres() error {
	schema := `
		CREATE EXTENSION IF NOT EXISTS pgcrypto;

		CREATE TABLE IF NOT EXISTS sessions (
			id         UUID PRIMARY KEY,
			client_session_id TEXT NOT NULL DEFAULT '',
			project    TEXT NOT NULL,
			directory  TEXT NOT NULL,
			auth_issuer TEXT,
			auth_subject TEXT,
			auth_username TEXT,
			auth_email TEXT,
			started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			ended_at   TEXT,
			summary    TEXT
		);

		CREATE TABLE IF NOT EXISTS observations (
			id              UUID PRIMARY KEY,
			sync_id         UUID,
			session_id      UUID NOT NULL,
			type            TEXT NOT NULL,
			title           TEXT NOT NULL,
			content         TEXT NOT NULL,
			tool_name       TEXT,
			project         TEXT,
			scope           TEXT NOT NULL DEFAULT 'project',
			topic_key       TEXT,
			normalized_hash TEXT,
			revision_count  INTEGER NOT NULL DEFAULT 1,
			duplicate_count INTEGER NOT NULL DEFAULT 1,
			last_seen_at    TEXT,
			created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			deleted_at      TEXT,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);

		CREATE TABLE IF NOT EXISTS user_prompts (
			id         UUID PRIMARY KEY,
			sync_id    UUID,
			session_id UUID NOT NULL,
			content    TEXT NOT NULL,
			project    TEXT,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);

		CREATE INDEX IF NOT EXISTS idx_obs_session  ON observations(session_id);
		CREATE INDEX IF NOT EXISTS idx_obs_type     ON observations(type);
		CREATE INDEX IF NOT EXISTS idx_obs_project  ON observations(project);
		CREATE INDEX IF NOT EXISTS idx_obs_created  ON observations(created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_obs_scope    ON observations(scope);
		CREATE INDEX IF NOT EXISTS idx_obs_sync_id  ON observations(sync_id);
		CREATE INDEX IF NOT EXISTS idx_obs_topic    ON observations(topic_key, project, scope, updated_at DESC);
		CREATE INDEX IF NOT EXISTS idx_obs_deleted  ON observations(deleted_at);
		CREATE INDEX IF NOT EXISTS idx_obs_dedupe   ON observations(normalized_hash, project, scope, type, title, created_at DESC);

		CREATE INDEX IF NOT EXISTS idx_prompts_session ON user_prompts(session_id);
		CREATE INDEX IF NOT EXISTS idx_prompts_project ON user_prompts(project);
		CREATE INDEX IF NOT EXISTS idx_prompts_created ON user_prompts(created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_prompts_sync_id ON user_prompts(sync_id);
	`
	if _, err := s.execHook(s.db, schema); err != nil {
		return err
	}

	if _, err := s.execHook(s.db, `UPDATE observations SET scope = 'project' WHERE scope IS NULL OR scope = ''`); err != nil {
		return err
	}
	if err := s.addColumnIfNotExists("sessions", "client_session_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfNotExists("sessions", "auth_issuer", "TEXT"); err != nil {
		return err
	}
	if err := s.addColumnIfNotExists("sessions", "auth_subject", "TEXT"); err != nil {
		return err
	}
	if err := s.addColumnIfNotExists("sessions", "auth_username", "TEXT"); err != nil {
		return err
	}
	if err := s.addColumnIfNotExists("sessions", "auth_email", "TEXT"); err != nil {
		return err
	}
	if _, err := s.execHook(s.db, `CREATE INDEX IF NOT EXISTS idx_sessions_author ON sessions(auth_issuer, auth_subject)`); err != nil {
		return err
	}
	if _, err := s.execHook(s.db, `UPDATE observations SET topic_key = NULL WHERE topic_key = ''`); err != nil {
		return err
	}
	if _, err := s.execHook(s.db, `UPDATE observations SET revision_count = 1 WHERE revision_count IS NULL OR revision_count < 1`); err != nil {
		return err
	}
	if _, err := s.execHook(s.db, `UPDATE observations SET duplicate_count = 1 WHERE duplicate_count IS NULL OR duplicate_count < 1`); err != nil {
		return err
	}
	if _, err := s.execHook(s.db, `UPDATE observations SET updated_at = created_at WHERE updated_at IS NULL OR updated_at = ''`); err != nil {
		return err
	}
	if _, err := s.execHook(s.db, `UPDATE observations SET sync_id = gen_random_uuid() WHERE sync_id IS NULL`); err != nil {
		return err
	}
	if _, err := s.execHook(s.db, `UPDATE user_prompts SET project = '' WHERE project IS NULL`); err != nil {
		return err
	}
	if _, err := s.execHook(s.db, `UPDATE user_prompts SET sync_id = gen_random_uuid() WHERE sync_id IS NULL`); err != nil {
		return err
	}

	return nil
}

// ─── Sessions ────────────────────────────────────────────────────────────────

func (s *Store) CreateSession(params CreateSessionParams) error {
	effectiveID := params.EffectiveID()
	return s.withTx(func(tx *sql.Tx) error {
		return s.createSessionTx(tx, effectiveID, params)
	})
}

func (s *Store) EndSession(id string, summary string) error {
	return s.withTx(func(tx *sql.Tx) error {
		id = normalizeSessionID(id)
		res, err := s.execHook(tx,
			`UPDATE sessions SET ended_at = CURRENT_TIMESTAMP, summary = ? WHERE id = ?`,
			nullableString(summary), id,
		)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}

		var endedAt string
		var project, directory, clientSessionID string
		var authIssuer, authSubject, authUsername, authEmail *string
		var storedSummary *string
		if err := s.queryRowHook(tx,
			`SELECT project, directory, client_session_id, auth_issuer, auth_subject, auth_username, auth_email, ended_at, summary FROM sessions WHERE id = ?`,
			id,
		).Scan(&project, &directory, &clientSessionID, &authIssuer, &authSubject, &authUsername, &authEmail, &endedAt, &storedSummary); err != nil {
			return err
		}

		_ = clientSessionID
		_ = project
		_ = directory
		_ = authIssuer
		_ = authSubject
		_ = authUsername
		_ = authEmail
		_ = endedAt
		_ = storedSummary
		return nil
	})
}

func (s *Store) GetSession(id string) (*Session, error) {
	row := s.queryRowHook(s.db,
		`SELECT id::text, client_session_id, project, directory, auth_issuer, auth_subject, auth_username, auth_email, started_at, ended_at, summary FROM sessions WHERE id = ?`, normalizeSessionID(id),
	)
	var sess Session
	if err := row.Scan(&sess.ID, &sess.ClientSessionID, &sess.Project, &sess.Directory, &sess.AuthIssuer, &sess.AuthSubject, &sess.AuthUsername, &sess.AuthEmail, &sess.StartedAt, &sess.EndedAt, &sess.Summary); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) RecentSessions(project string, limit int) ([]SessionSummary, error) {
	if limit <= 0 {
		limit = 5
	}

	query := `
		SELECT s.id::text, s.client_session_id, s.project, s.auth_issuer, s.auth_subject, s.auth_username, s.auth_email, s.started_at, s.ended_at, s.summary,
		       COUNT(o.id) as observation_count
		FROM sessions s
		LEFT JOIN observations o ON o.session_id = s.id AND o.deleted_at IS NULL
		WHERE 1=1
	`
	args := []any{}

	if project != "" {
		normalizedProject := normalizeProject(project)
		query += ` AND (s.project = ? OR (s.project LIKE '/%' AND REVERSE(SPLIT_PART(REVERSE(s.project), '/', 1)) = ?) OR (s.project ~ '^[A-Za-z]:[/\\]' AND REVERSE(SPLIT_PART(REVERSE(REPLACE(s.project, '\', '/')), '/', 1)) = ?))`
		args = append(args, normalizedProject, normalizedProject, normalizedProject)
	}

	query += " GROUP BY s.id ORDER BY MAX(COALESCE(o.created_at, s.started_at)) DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.queryItHook(s.db, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SessionSummary
	for rows.Next() {
		var ss SessionSummary
		if err := rows.Scan(&ss.ID, &ss.ClientSessionID, &ss.Project, &ss.AuthIssuer, &ss.AuthSubject, &ss.AuthUsername, &ss.AuthEmail, &ss.StartedAt, &ss.EndedAt, &ss.Summary, &ss.ObservationCount); err != nil {
			return nil, err
		}
		results = append(results, ss)
	}
	return results, rows.Err()
}

// AllSessions returns recent sessions ordered by most recent first (for TUI browsing).
func (s *Store) AllSessions(project string, limit int) ([]SessionSummary, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT s.id::text, s.client_session_id, s.project, s.auth_issuer, s.auth_subject, s.auth_username, s.auth_email, s.started_at, s.ended_at, s.summary,
		       COUNT(o.id) as observation_count
		FROM sessions s
		LEFT JOIN observations o ON o.session_id = s.id AND o.deleted_at IS NULL
		WHERE 1=1
	`
	args := []any{}

	if project != "" {
		normalizedProject := normalizeProject(project)
		query += ` AND (s.project = ? OR (s.project LIKE '/%' AND REVERSE(SPLIT_PART(REVERSE(s.project), '/', 1)) = ?) OR (s.project ~ '^[A-Za-z]:[/\\]' AND REVERSE(SPLIT_PART(REVERSE(REPLACE(s.project, '\', '/')), '/', 1)) = ?))`
		args = append(args, normalizedProject, normalizedProject, normalizedProject)
	}

	query += " GROUP BY s.id ORDER BY MAX(COALESCE(o.created_at, s.started_at)) DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.queryItHook(s.db, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SessionSummary
	for rows.Next() {
		var ss SessionSummary
		if err := rows.Scan(&ss.ID, &ss.ClientSessionID, &ss.Project, &ss.AuthIssuer, &ss.AuthSubject, &ss.AuthUsername, &ss.AuthEmail, &ss.StartedAt, &ss.EndedAt, &ss.Summary, &ss.ObservationCount); err != nil {
			return nil, err
		}
		results = append(results, ss)
	}
	return results, rows.Err()
}

// AllObservations returns recent observations ordered by most recent first (for TUI browsing).
func (s *Store) AllObservations(project, scope string, limit int) ([]Observation, error) {
	if limit <= 0 {
		limit = s.cfg.MaxContextResults
	}

	query := `
		SELECT o.id::text, coalesce(o.sync_id::text, '') as sync_id, o.session_id::text, o.type, o.title, o.content, o.tool_name, o.project,
		       o.scope, o.topic_key, o.revision_count, o.duplicate_count, o.last_seen_at, o.created_at, o.updated_at, o.deleted_at
		FROM observations o
		WHERE o.deleted_at IS NULL
	`
	args := []any{}

	if project != "" {
		normalizedProject := normalizeProject(project)
		query += ` AND (o.project = ? OR (o.project LIKE '/%' AND REVERSE(SPLIT_PART(REVERSE(o.project), '/', 1)) = ?) OR (o.project ~ '^[A-Za-z]:[/\\]' AND REVERSE(SPLIT_PART(REVERSE(REPLACE(o.project, '\', '/')), '/', 1)) = ?))`
		args = append(args, normalizedProject, normalizedProject, normalizedProject)
	}
	if scope != "" {
		query += " AND o.scope = ?"
		args = append(args, normalizeScope(scope))
	}

	query += " ORDER BY o.created_at DESC LIMIT ?"
	args = append(args, limit)

	return s.queryObservations(query, args...)
}

// SessionObservations returns all observations for a specific session.
func (s *Store) SessionObservations(sessionID string, limit int) ([]Observation, error) {
	if limit <= 0 {
		limit = 200
	}

	query := `
		SELECT id::text, coalesce(sync_id::text, '') as sync_id, session_id::text, type, title, content, tool_name, project,
		       scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		FROM observations
		WHERE session_id = ? AND deleted_at IS NULL
		ORDER BY created_at ASC
		LIMIT ?
	`
	return s.queryObservations(query, normalizeSessionID(sessionID), limit)
}

// ─── Observations ────────────────────────────────────────────────────────────

func (s *Store) AddObservation(p AddObservationParams) (string, error) {
	// Strip <private>...</private> tags before persisting ANYTHING
	title := stripPrivateTags(p.Title)
	content := stripPrivateTags(p.Content)

	if len(content) > s.cfg.MaxObservationLength {
		content = content[:s.cfg.MaxObservationLength] + "... [truncated]"
	}
	scope := normalizeScope(p.Scope)
	normHash := hashNormalized(content)
	topicKey := normalizeTopicKey(p.TopicKey)
	sessionID := normalizeSessionID(p.SessionID)
	project := normalizeProject(p.Project)

	var observationID string
	err := s.withTx(func(tx *sql.Tx) error {
		projectCmpFn := "coalesce"
		nowExpr := "CURRENT_TIMESTAMP"
		orderExpr := "updated_at DESC, created_at DESC"
		windowClause := "AND created_at >= ?"
		argsProject := nullableString(project)
		if topicKey != "" {
			topicQuery := fmt.Sprintf(`SELECT id::text FROM observations
				 WHERE topic_key = ?
				   AND %s(project, '') = %s(?, '')
				   AND scope = ?
				   AND deleted_at IS NULL
				 ORDER BY %s
				 LIMIT 1`, projectCmpFn, projectCmpFn, orderExpr)
			var existingID string
			err := s.queryRowHook(tx,
				topicQuery,
				topicKey, argsProject, scope,
			).Scan(&existingID)
			if err == nil {
				if _, err := s.execHook(tx,
					fmt.Sprintf(`UPDATE observations
					 SET type = ?,
					     title = ?,
					     content = ?,
					     tool_name = ?,
					     topic_key = ?,
					     normalized_hash = ?,
					     revision_count = revision_count + 1,
					     last_seen_at = %s,
					     updated_at = %s
					 WHERE id = ?`, nowExpr, nowExpr),
					p.Type,
					title,
					content,
					nullableString(p.ToolName),
					nullableString(topicKey),
					normHash,
					existingID,
				); err != nil {
					return err
				}
				if _, err := s.getObservationTx(tx, existingID); err != nil {
					return err
				}
				observationID = existingID
				return nil
			}
			if err != sql.ErrNoRows {
				return err
			}
		}

		windowStart := time.Now().UTC().Add(-s.cfg.DedupeWindow).Format("2006-01-02 15:04:05")
		dedupeQuery := fmt.Sprintf(`SELECT id::text FROM observations
			 WHERE normalized_hash = ?
			   AND %s(project, '') = %s(?, '')
			   AND scope = ?
			   AND type = ?
			   AND title = ?
			   AND deleted_at IS NULL
			   %s
			 ORDER BY created_at DESC
			 LIMIT 1`, projectCmpFn, projectCmpFn, windowClause)
		args := []any{normHash, argsProject, scope, p.Type, title, windowStart}
		var existingID string
		err := s.queryRowHook(tx,
			dedupeQuery,
			args...,
		).Scan(&existingID)
		if err == nil {
			if _, err := s.execHook(tx,
				fmt.Sprintf(`UPDATE observations
				 SET duplicate_count = duplicate_count + 1,
				     last_seen_at = %s,
				     updated_at = %s
				 WHERE id = ?`, nowExpr, nowExpr),
				existingID,
			); err != nil {
				return err
			}
			if _, err := s.getObservationTx(tx, existingID); err != nil {
				return err
			}
			observationID = existingID
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}

		syncID := normalizeObservationSyncID("")
		observationID = normalizeObservationID("")
		if _, err := s.execHook(tx,
			`INSERT INTO observations (id, sync_id, session_id, type, title, content, tool_name, project, scope, topic_key, normalized_hash, revision_count, duplicate_count, last_seen_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			observationID, syncID, sessionID, p.Type, title, content,
			nullableString(p.ToolName), nullableString(project), scope, nullableString(topicKey), normHash,
		); err != nil {
			return err
		}
		if _, err := s.getObservationTx(tx, observationID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return observationID, nil
}

func (s *Store) RecentObservations(project, scope string, limit int) ([]Observation, error) {
	if limit <= 0 {
		limit = s.cfg.MaxContextResults
	}

	query := `
		SELECT o.id::text, coalesce(o.sync_id::text, '') as sync_id, o.session_id::text, o.type, o.title, o.content, o.tool_name, o.project,
		       o.scope, o.topic_key, o.revision_count, o.duplicate_count, o.last_seen_at, o.created_at, o.updated_at, o.deleted_at
		FROM observations o
		WHERE o.deleted_at IS NULL
	`
	args := []any{}

	if project != "" {
		normalizedProject := normalizeProject(project)
		query += ` AND (o.project = ? OR (o.project LIKE '/%' AND REVERSE(SPLIT_PART(REVERSE(o.project), '/', 1)) = ?) OR (o.project ~ '^[A-Za-z]:[/\\]' AND REVERSE(SPLIT_PART(REVERSE(REPLACE(o.project, '\', '/')), '/', 1)) = ?))`
		args = append(args, normalizedProject, normalizedProject, normalizedProject)
	}
	if scope != "" {
		query += " AND o.scope = ?"
		args = append(args, normalizeScope(scope))
	}

	query += " ORDER BY o.created_at DESC LIMIT ?"
	args = append(args, limit)

	return s.queryObservations(query, args...)
}

// ─── User Prompts ────────────────────────────────────────────────────────────

func (s *Store) AddPrompt(p AddPromptParams) (string, error) {
	content := stripPrivateTags(p.Content)
	if len(content) > s.cfg.MaxObservationLength {
		content = content[:s.cfg.MaxObservationLength] + "... [truncated]"
	}

	var promptID string
	project := normalizeProject(p.Project)
	err := s.withTx(func(tx *sql.Tx) error {
		syncID := normalizePromptSyncID("")
		promptID = normalizePromptID("")
		if _, err := s.execHook(tx,
			`INSERT INTO user_prompts (id, sync_id, session_id, content, project) VALUES (?, ?, ?, ?, ?)`,
			promptID, syncID, normalizeSessionID(p.SessionID), content, nullableString(project),
		); err != nil {
			return err
		}
		_ = syncID
		return nil
	})
	if err != nil {
		return "", err
	}
	return promptID, nil
}

func (s *Store) RecentPrompts(project string, limit int) ([]Prompt, error) {
	if limit <= 0 {
		limit = 20
	}

	query := `SELECT id::text as id, coalesce(sync_id::text, '') as sync_id, session_id::text, content, coalesce(project, '') as project, created_at FROM user_prompts`
	args := []any{}

	if project != "" {
		normalizedProject := normalizeProject(project)
		query += ` WHERE (project = ? OR (project LIKE '/%' AND REVERSE(SPLIT_PART(REVERSE(project), '/', 1)) = ?) OR (project ~ '^[A-Za-z]:[/\\]' AND REVERSE(SPLIT_PART(REVERSE(REPLACE(project, '\', '/')), '/', 1)) = ?))`
		args = append(args, normalizedProject, normalizedProject, normalizedProject)
	}

	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.queryItHook(s.db, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Prompt
	for rows.Next() {
		var p Prompt
		if err := rows.Scan(&p.ID, &p.SyncID, &p.SessionID, &p.Content, &p.Project, &p.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, p)
	}
	return results, rows.Err()
}

func (s *Store) SearchPrompts(query string, project string, limit int) ([]Prompt, error) {
	if limit <= 0 {
		limit = 10
	}

	sql := `
		SELECT coalesce(p.id::text, '') as id, coalesce(p.sync_id::text, '') as sync_id, p.session_id::text, p.content, coalesce(p.project, '') as project, p.created_at
		FROM user_prompts p
		WHERE p.content ILIKE ?
	`
	args := []any{"%" + query + "%"}

	if project != "" {
		normalizedProject := normalizeProject(project)
		sql += ` AND (p.project = ? OR (p.project LIKE '/%' AND REVERSE(SPLIT_PART(REVERSE(p.project), '/', 1)) = ?) OR (p.project ~ '^[A-Za-z]:[/\\]' AND REVERSE(SPLIT_PART(REVERSE(REPLACE(p.project, '\', '/')), '/', 1)) = ?))`
		args = append(args, normalizedProject, normalizedProject, normalizedProject)
	}

	sql += " ORDER BY p.created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.queryItHook(s.db, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search prompts: %w", err)
	}
	defer rows.Close()

	var results []Prompt
	for rows.Next() {
		var p Prompt
		if err := rows.Scan(&p.ID, &p.SyncID, &p.SessionID, &p.Content, &p.Project, &p.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, p)
	}
	return results, rows.Err()
}

// ─── Get Single Observation ──────────────────────────────────────────────────

func (s *Store) GetObservation(id string) (*Observation, error) {
	row := s.queryRowHook(s.db,
		`SELECT id::text, coalesce(sync_id::text, '') as sync_id, session_id::text, type, title, content, tool_name, project,
		        scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		 FROM observations WHERE id = ? AND deleted_at IS NULL`, normalizeObservationID(id),
	)
	var o Observation
	if err := row.Scan(
		&o.ID, &o.SyncID, &o.SessionID, &o.Type, &o.Title, &o.Content,
		&o.ToolName, &o.Project, &o.Scope, &o.TopicKey, &o.RevisionCount, &o.DuplicateCount, &o.LastSeenAt,
		&o.CreatedAt, &o.UpdatedAt, &o.DeletedAt,
	); err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *Store) UpdateObservation(id string, p UpdateObservationParams) (*Observation, error) {
	var updated *Observation
	err := s.withTx(func(tx *sql.Tx) error {
		nowExpr := "CURRENT_TIMESTAMP"
		obs, err := s.getObservationTx(tx, id)
		if err != nil {
			return err
		}

		typ := obs.Type
		title := obs.Title
		content := obs.Content
		project := derefString(obs.Project)
		scope := obs.Scope
		topicKey := derefString(obs.TopicKey)

		if p.Type != nil {
			typ = *p.Type
		}
		if p.Title != nil {
			title = stripPrivateTags(*p.Title)
		}
		if p.Content != nil {
			content = stripPrivateTags(*p.Content)
			if len(content) > s.cfg.MaxObservationLength {
				content = content[:s.cfg.MaxObservationLength] + "... [truncated]"
			}
		}
		if p.Project != nil {
			project = *p.Project
		}
		if p.Scope != nil {
			scope = normalizeScope(*p.Scope)
		}
		if p.TopicKey != nil {
			topicKey = normalizeTopicKey(*p.TopicKey)
		}

		if _, err := s.execHook(tx,
			fmt.Sprintf(`UPDATE observations
			 SET type = ?,
			     title = ?,
			     content = ?,
			     project = ?,
			     scope = ?,
			     topic_key = ?,
			     normalized_hash = ?,
			     revision_count = revision_count + 1,
			     updated_at = %s
			 WHERE id = ? AND deleted_at IS NULL`, nowExpr),
			typ,
			title,
			content,
			nullableString(project),
			scope,
			nullableString(topicKey),
			hashNormalized(content),
			id,
		); err != nil {
			return err
		}

		updated, err = s.getObservationTx(tx, id)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Store) DeleteObservation(id string, hardDelete bool) error {
	return s.withTx(func(tx *sql.Tx) error {
		nowExpr := "CURRENT_TIMESTAMP"
		_, err := s.getObservationTx(tx, id)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}

		deletedAt := Now()
		if hardDelete {
			if _, err := s.execHook(tx, `DELETE FROM observations WHERE id = ?`, id); err != nil {
				return err
			}
		} else {
			if _, err := s.execHook(tx,
				fmt.Sprintf(`UPDATE observations
				 SET deleted_at = %s,
				     updated_at = %s
				 WHERE id = ? AND deleted_at IS NULL`, nowExpr, nowExpr),
				id,
			); err != nil {
				return err
			}
			if err := s.queryRowHook(tx, `SELECT deleted_at FROM observations WHERE id = ?`, id).Scan(&deletedAt); err != nil {
				return err
			}
		}

		_ = deletedAt
		return nil
	})
}

// ─── Timeline ────────────────────────────────────────────────────────────────
//
// Timeline provides chronological context around a specific observation.
// Given an observation ID, it returns N observations before and M after,
// all within the same session. This is the "progressive disclosure" pattern
// from claude-mem — agents first search, then use timeline to drill into
// the chronological neighborhood of a result.

func (s *Store) Timeline(observationID string, before, after int) (*TimelineResult, error) {
	if before <= 0 {
		before = 5
	}
	if after <= 0 {
		after = 5
	}

	// 1. Get the focus observation
	focus, err := s.GetObservation(observationID)
	if err != nil {
		return nil, fmt.Errorf("timeline: observation %q not found: %w", observationID, err)
	}

	// 2. Get session info
	session, err := s.GetSession(focus.SessionID)
	if err != nil {
		// Session might be missing for manual-save observations — non-fatal
		session = nil
	}

	// 3. Get observations BEFORE the focus (same session, older, chronological order)
	beforeRows, err := s.queryItHook(s.db, `
		SELECT id, session_id, type, title, content, tool_name, project,
		       scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		FROM observations
		WHERE session_id = ? AND (created_at < ? OR (created_at = ? AND id < ?)) AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, focus.SessionID, focus.CreatedAt, focus.CreatedAt, observationID, before)
	if err != nil {
		return nil, fmt.Errorf("timeline: before query: %w", err)
	}
	defer beforeRows.Close()

	var beforeEntries []TimelineEntry
	for beforeRows.Next() {
		var e TimelineEntry
		if err := beforeRows.Scan(
			&e.ID, &e.SessionID, &e.Type, &e.Title, &e.Content,
			&e.ToolName, &e.Project, &e.Scope, &e.TopicKey, &e.RevisionCount, &e.DuplicateCount, &e.LastSeenAt,
			&e.CreatedAt, &e.UpdatedAt, &e.DeletedAt,
		); err != nil {
			return nil, err
		}
		beforeEntries = append(beforeEntries, e)
	}
	if err := beforeRows.Err(); err != nil {
		return nil, err
	}
	// Reverse to get chronological order (oldest first)
	for i, j := 0, len(beforeEntries)-1; i < j; i, j = i+1, j-1 {
		beforeEntries[i], beforeEntries[j] = beforeEntries[j], beforeEntries[i]
	}

	// 4. Get observations AFTER the focus (same session, newer, chronological order)
	afterRows, err := s.queryItHook(s.db, `
		SELECT id, session_id, type, title, content, tool_name, project,
		       scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		FROM observations
		WHERE session_id = ? AND (created_at > ? OR (created_at = ? AND id > ?)) AND deleted_at IS NULL
		ORDER BY created_at ASC, id ASC
		LIMIT ?
	`, focus.SessionID, focus.CreatedAt, focus.CreatedAt, observationID, after)
	if err != nil {
		return nil, fmt.Errorf("timeline: after query: %w", err)
	}
	defer afterRows.Close()

	var afterEntries []TimelineEntry
	for afterRows.Next() {
		var e TimelineEntry
		if err := afterRows.Scan(
			&e.ID, &e.SessionID, &e.Type, &e.Title, &e.Content,
			&e.ToolName, &e.Project, &e.Scope, &e.TopicKey, &e.RevisionCount, &e.DuplicateCount, &e.LastSeenAt,
			&e.CreatedAt, &e.UpdatedAt, &e.DeletedAt,
		); err != nil {
			return nil, err
		}
		afterEntries = append(afterEntries, e)
	}
	if err := afterRows.Err(); err != nil {
		return nil, err
	}

	// 5. Count total observations in the session for context
	var totalInRange int
	s.queryRowHook(s.db,
		"SELECT COUNT(*) FROM observations WHERE session_id = ? AND deleted_at IS NULL", focus.SessionID,
	).Scan(&totalInRange)

	return &TimelineResult{
		Focus:        *focus,
		Before:       beforeEntries,
		After:        afterEntries,
		SessionInfo:  session,
		TotalInRange: totalInRange,
	}, nil
}

// ─── Search ──────────────────────────────────────────────────────────────────

func (s *Store) Search(query string, opts SearchOptions) ([]SearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > s.cfg.MaxSearchResults {
		limit = s.cfg.MaxSearchResults
	}

	sql := `
		SELECT o.id::text, coalesce(o.sync_id::text, '') as sync_id, o.session_id::text, o.type, o.title, o.content, o.tool_name, o.project,
		       o.scope, o.topic_key, o.revision_count, o.duplicate_count, o.last_seen_at, o.created_at, o.updated_at, o.deleted_at,
		       0.0 as rank
		FROM observations o
		WHERE o.deleted_at IS NULL
		  AND (o.title ILIKE ? OR o.content ILIKE ?)
	`
	like := "%" + query + "%"
	args := []any{like, like}

	if opts.Type != "" {
		sql += " AND o.type = ?"
		args = append(args, opts.Type)
	}

	if opts.Project != "" {
		normalizedProject := normalizeProject(opts.Project)
		sql += ` AND (o.project = ? OR (o.project LIKE '/%' AND REVERSE(SPLIT_PART(REVERSE(o.project), '/', 1)) = ?) OR (o.project ~ '^[A-Za-z]:[/\\]' AND REVERSE(SPLIT_PART(REVERSE(REPLACE(o.project, '\', '/')), '/', 1)) = ?))`
		args = append(args, normalizedProject, normalizedProject, normalizedProject)
	}

	if opts.Scope != "" {
		sql += " AND o.scope = ?"
		args = append(args, normalizeScope(opts.Scope))
	}

	sql += " ORDER BY o.created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.queryItHook(s.db, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var sr SearchResult
		if err := rows.Scan(
			&sr.ID, &sr.SyncID, &sr.SessionID, &sr.Type, &sr.Title, &sr.Content,
			&sr.ToolName, &sr.Project, &sr.Scope, &sr.TopicKey, &sr.RevisionCount, &sr.DuplicateCount,
			&sr.LastSeenAt, &sr.CreatedAt, &sr.UpdatedAt, &sr.DeletedAt,
			&sr.Rank,
		); err != nil {
			return nil, err
		}
		results = append(results, sr)
	}
	return results, rows.Err()
}

// ─── Stats ───────────────────────────────────────────────────────────────────

func (s *Store) Stats() (*Stats, error) {
	stats := &Stats{}

	s.queryRowHook(s.db, "SELECT COUNT(*) FROM sessions").Scan(&stats.TotalSessions)
	s.queryRowHook(s.db, "SELECT COUNT(*) FROM observations WHERE deleted_at IS NULL").Scan(&stats.TotalObservations)
	s.queryRowHook(s.db, "SELECT COUNT(*) FROM user_prompts").Scan(&stats.TotalPrompts)

	rows, err := s.queryItHook(s.db, "SELECT project FROM observations WHERE project IS NOT NULL AND deleted_at IS NULL GROUP BY project ORDER BY MAX(created_at) DESC")
	if err != nil {
		return stats, nil
	}
	defer rows.Close()

	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			stats.Projects = append(stats.Projects, p)
		}
	}

	return stats, nil
}

// ─── Context Formatting ─────────────────────────────────────────────────────

func (s *Store) FormatContext(project, scope string) (string, error) {
	project = strings.TrimSpace(project)
	if project != "" {
		project = normalizeProject(project)
	}
	sessions, err := s.RecentSessions(project, 5)
	if err != nil {
		return "", err
	}

	observations, err := s.RecentObservations(project, scope, s.cfg.MaxContextResults)
	if err != nil {
		return "", err
	}

	prompts, err := s.RecentPrompts(project, 10)
	if err != nil {
		return "", err
	}

	if len(sessions) == 0 && len(observations) == 0 && len(prompts) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("## Memory from Previous Sessions\n\n")

	if len(sessions) > 0 {
		b.WriteString("### Recent Sessions\n")
		for _, sess := range sessions {
			summary := ""
			if sess.Summary != nil {
				summary = fmt.Sprintf(": %s", truncate(*sess.Summary, 200))
			}
			fmt.Fprintf(&b, "- **%s** (%s)%s [%d observations]\n",
				sess.Project, sess.StartedAt, summary, sess.ObservationCount)
		}
		b.WriteString("\n")
	}

	if len(prompts) > 0 {
		b.WriteString("### Recent User Prompts\n")
		for _, p := range prompts {
			fmt.Fprintf(&b, "- %s: %s\n", p.CreatedAt, truncate(p.Content, 200))
		}
		b.WriteString("\n")
	}

	if len(observations) > 0 {
		b.WriteString("### Recent Observations\n")
		for _, obs := range observations {
			fmt.Fprintf(&b, "- [%s] **%s**: %s\n",
				obs.Type, obs.Title, truncate(obs.Content, 300))
		}
		b.WriteString("\n")
	}

	return b.String(), nil
}

// ─── Export / Import ─────────────────────────────────────────────────────────

func (s *Store) Export() (*ExportData, error) {
	data := &ExportData{
		Version:    "0.1.0",
		ExportedAt: Now(),
	}

	// Sessions
	rows, err := s.queryItHook(s.db,
		"SELECT id::text, client_session_id, project, directory, auth_issuer, auth_subject, auth_username, auth_email, started_at, ended_at, summary FROM sessions ORDER BY started_at",
	)
	if err != nil {
		return nil, fmt.Errorf("export sessions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.ClientSessionID, &sess.Project, &sess.Directory, &sess.AuthIssuer, &sess.AuthSubject, &sess.AuthUsername, &sess.AuthEmail, &sess.StartedAt, &sess.EndedAt, &sess.Summary); err != nil {
			return nil, err
		}
		data.Sessions = append(data.Sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Observations
	obsRows, err := s.queryItHook(s.db,
		`SELECT id::text, coalesce(sync_id::text, '') as sync_id, session_id::text, type, title, content, tool_name, project,
		        scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		 FROM observations ORDER BY created_at, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("export observations: %w", err)
	}
	defer obsRows.Close()
	for obsRows.Next() {
		var o Observation
		if err := obsRows.Scan(
			&o.ID, &o.SyncID, &o.SessionID, &o.Type, &o.Title, &o.Content,
			&o.ToolName, &o.Project, &o.Scope, &o.TopicKey, &o.RevisionCount, &o.DuplicateCount, &o.LastSeenAt,
			&o.CreatedAt, &o.UpdatedAt, &o.DeletedAt,
		); err != nil {
			return nil, err
		}
		data.Observations = append(data.Observations, o)
	}
	if err := obsRows.Err(); err != nil {
		return nil, err
	}

	// Prompts
	promptRows, err := s.queryItHook(s.db,
		"SELECT id::text as id, coalesce(sync_id::text, '') as sync_id, session_id::text, content, coalesce(project, '') as project, created_at FROM user_prompts ORDER BY created_at, id",
	)
	if err != nil {
		return nil, fmt.Errorf("export prompts: %w", err)
	}
	defer promptRows.Close()
	for promptRows.Next() {
		var p Prompt
		if err := promptRows.Scan(&p.ID, &p.SyncID, &p.SessionID, &p.Content, &p.Project, &p.CreatedAt); err != nil {
			return nil, err
		}
		data.Prompts = append(data.Prompts, p)
	}
	if err := promptRows.Err(); err != nil {
		return nil, err
	}

	return data, nil
}

func (s *Store) Import(data *ExportData) (*ImportResult, error) {
	tx, err := s.beginTxHook()
	if err != nil {
		return nil, fmt.Errorf("import: begin tx: %w", err)
	}
	defer tx.Rollback()

	result := &ImportResult{}

	// Import sessions (skip duplicates)
	for _, sess := range data.Sessions {
		res, err := s.execHook(tx,
			`INSERT INTO sessions (id, client_session_id, project, directory, auth_issuer, auth_subject, auth_username, auth_email, started_at, ended_at, summary)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			normalizeSessionID(preferString(sess.ID, sess.ClientSessionID)), sess.ClientSessionID, sess.Project, sess.Directory, sess.AuthIssuer, sess.AuthSubject, sess.AuthUsername, sess.AuthEmail, sess.StartedAt, sess.EndedAt, sess.Summary,
		)
		if err != nil {
			return nil, fmt.Errorf("import session %s: %w", sess.ID, err)
		}
		n, _ := res.RowsAffected()
		result.SessionsImported += int(n)
	}

	// Import observations
	for _, obs := range data.Observations {
		res, err := s.execHook(tx,
			`INSERT INTO observations (id, sync_id, session_id, type, title, content, tool_name, project, scope, topic_key, normalized_hash, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			normalizeImportedObservationID(obs.ID, obs.SyncID),
			normalizeObservationSyncID(obs.SyncID),
			normalizeSessionID(obs.SessionID),
			obs.Type,
			obs.Title,
			obs.Content,
			obs.ToolName,
			obs.Project,
			normalizeScope(obs.Scope),
			nullableString(normalizeTopicKey(derefString(obs.TopicKey))),
			hashNormalized(obs.Content),
			maxInt(obs.RevisionCount, 1),
			maxInt(obs.DuplicateCount, 1),
			obs.LastSeenAt,
			obs.CreatedAt,
			obs.UpdatedAt,
			obs.DeletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("import observation %s: %w", obs.ID, err)
		}
		n, _ := res.RowsAffected()
		result.ObservationsImported += int(n)
	}

	// Import prompts
	for _, p := range data.Prompts {
		res, err := s.execHook(tx,
			`INSERT INTO user_prompts (id, sync_id, session_id, content, project, created_at)
			 VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			normalizePromptID(p.ID), normalizePromptSyncID(p.SyncID), normalizeSessionID(p.SessionID), p.Content, p.Project, p.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("import prompt %s: %w", p.ID, err)
		}
		n, _ := res.RowsAffected()
		result.PromptsImported += int(n)
	}

	if err := s.commitHook(tx); err != nil {
		return nil, fmt.Errorf("import: commit: %w", err)
	}

	return result, nil
}

type ImportResult struct {
	SessionsImported     int `json:"sessions_imported"`
	ObservationsImported int `json:"observations_imported"`
	PromptsImported      int `json:"prompts_imported"`
}

// ─── Project Migration ───────────────────────────────────────────────────────

type MigrateResult struct {
	Migrated            bool  `json:"migrated"`
	ObservationsUpdated int64 `json:"observations_updated"`
	SessionsUpdated     int64 `json:"sessions_updated"`
	PromptsUpdated      int64 `json:"prompts_updated"`
}

func (s *Store) MigrateProject(oldName, newName string) (*MigrateResult, error) {
	if oldName == "" || newName == "" || oldName == newName {
		return &MigrateResult{}, nil
	}

	// Check if old project has any records (short-circuit on first match)
	var exists bool
	err := s.queryRowHook(s.db,
		`SELECT EXISTS(
			SELECT 1 FROM observations WHERE project = ?
			UNION ALL
			SELECT 1 FROM sessions WHERE project = ?
			UNION ALL
			SELECT 1 FROM user_prompts WHERE project = ?
		)`, oldName, oldName, oldName,
	).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check old project: %w", err)
	}
	if !exists {
		return &MigrateResult{}, nil
	}

	result := &MigrateResult{Migrated: true}

	err = s.withTx(func(tx *sql.Tx) error {
		// FTS triggers handle index updates automatically on UPDATE
		res, err := s.execHook(tx, `UPDATE observations SET project = ? WHERE project = ?`, newName, oldName)
		if err != nil {
			return fmt.Errorf("migrate observations: %w", err)
		}
		result.ObservationsUpdated, _ = res.RowsAffected()

		res, err = s.execHook(tx, `UPDATE sessions SET project = ? WHERE project = ?`, newName, oldName)
		if err != nil {
			return fmt.Errorf("migrate sessions: %w", err)
		}
		result.SessionsUpdated, _ = res.RowsAffected()

		res, err = s.execHook(tx, `UPDATE user_prompts SET project = ? WHERE project = ?`, newName, oldName)
		if err != nil {
			return fmt.Errorf("migrate prompts: %w", err)
		}
		result.PromptsUpdated, _ = res.RowsAffected()

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func (s *Store) withTx(fn func(tx *sql.Tx) error) error {
	tx, err := s.beginTxHook()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return s.commitHook(tx)
}

func (s *Store) createSessionTx(tx *sql.Tx, id string, params CreateSessionParams) error {
	id = normalizeSessionID(id)
	project := strings.TrimSpace(params.Project)
	if project != "" {
		project = normalizeProject(project)
	}
	var existingIssuer, existingSubject sql.NullString
	if err := s.queryRowHook(tx, `SELECT auth_issuer, auth_subject FROM sessions WHERE id = ?`, id).Scan(&existingIssuer, &existingSubject); err != nil && err != sql.ErrNoRows {
		return err
	}
	newIssuer := strings.TrimSpace(params.AuthorField("issuer"))
	newSubject := strings.TrimSpace(params.AuthorField("subject"))
	if existingIssuer.Valid && existingSubject.Valid {
		if newIssuer == "" || newSubject == "" || existingIssuer.String != newIssuer || existingSubject.String != newSubject {
			return ErrSessionAuthorConflict
		}
	}
	_, err := s.execHook(tx,
		`INSERT INTO sessions (id, client_session_id, project, directory, auth_issuer, auth_subject, auth_username, auth_email)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   client_session_id = CASE WHEN sessions.client_session_id = '' THEN excluded.client_session_id ELSE sessions.client_session_id END,
		   project   = CASE WHEN sessions.project = '' THEN excluded.project ELSE sessions.project END,
		   directory = CASE WHEN sessions.directory = '' THEN excluded.directory ELSE sessions.directory END,
		   auth_issuer = CASE WHEN sessions.auth_issuer IS NULL OR sessions.auth_issuer = '' THEN excluded.auth_issuer ELSE sessions.auth_issuer END,
		   auth_subject = CASE WHEN sessions.auth_subject IS NULL OR sessions.auth_subject = '' THEN excluded.auth_subject ELSE sessions.auth_subject END,
		   auth_username = CASE WHEN excluded.auth_username IS NOT NULL AND excluded.auth_username != '' THEN excluded.auth_username ELSE sessions.auth_username END,
		   auth_email = CASE WHEN excluded.auth_email IS NOT NULL AND excluded.auth_email != '' THEN excluded.auth_email ELSE sessions.auth_email END`,
		id,
		strings.TrimSpace(params.ClientSessionID),
		project,
		params.Directory,
		nullableString(newIssuer),
		nullableString(newSubject),
		nullableString(strings.TrimSpace(params.AuthorField("username"))),
		nullableString(strings.TrimSpace(params.AuthorField("email"))),
	)
	return err
}

func (s *Store) getObservationTx(tx *sql.Tx, id string) (*Observation, error) {
	row := s.queryRowHook(tx,
		`SELECT id::text, coalesce(sync_id::text, '') as sync_id, session_id::text, type, title, content, tool_name, project,
		        scope, topic_key, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at
		 FROM observations WHERE id = ? AND deleted_at IS NULL`, normalizeObservationID(id),
	)
	var o Observation
	if err := row.Scan(&o.ID, &o.SyncID, &o.SessionID, &o.Type, &o.Title, &o.Content, &o.ToolName, &o.Project, &o.Scope, &o.TopicKey, &o.RevisionCount, &o.DuplicateCount, &o.LastSeenAt, &o.CreatedAt, &o.UpdatedAt, &o.DeletedAt); err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *Store) queryObservations(query string, args ...any) ([]Observation, error) {
	rows, err := s.queryItHook(s.db, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Observation
	for rows.Next() {
		var o Observation
		if err := rows.Scan(
			&o.ID, &o.SyncID, &o.SessionID, &o.Type, &o.Title, &o.Content,
			&o.ToolName, &o.Project, &o.Scope, &o.TopicKey, &o.RevisionCount, &o.DuplicateCount, &o.LastSeenAt,
			&o.CreatedAt, &o.UpdatedAt, &o.DeletedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, o)
	}
	return results, rows.Err()
}

func (s *Store) addColumnIfNotExists(tableName, columnName, definition string) error {
	var exists bool
	err := s.queryRowHook(s.db, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = $1
			  AND column_name = $2
		)`, tableName, columnName).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, columnName, definition))
	return err
}

func (s *Store) migrateLegacyObservationsTable() error {
	return nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

func normalizeScope(scope string) string {
	v := strings.TrimSpace(strings.ToLower(scope))
	if v == "personal" {
		return "personal"
	}
	return "project"
}

// SuggestTopicKey generates a stable topic key suggestion from type/title/content.
// It infers a topic family (e.g. architecture/*, bug/*) and then appends
// a normalized segment from title/content for stable cross-session keys.
func SuggestTopicKey(typ, title, content string) string {
	family := inferTopicFamily(typ, title, content)
	cleanTitle := stripPrivateTags(title)
	segment := normalizeTopicSegment(cleanTitle)

	if segment == "" {
		cleanContent := stripPrivateTags(content)
		words := strings.Fields(strings.ToLower(cleanContent))
		if len(words) > 8 {
			words = words[:8]
		}
		segment = normalizeTopicSegment(strings.Join(words, " "))
	}

	if segment == "" {
		segment = "general"
	}

	if strings.HasPrefix(segment, family+"-") {
		segment = strings.TrimPrefix(segment, family+"-")
	}
	if segment == "" || segment == family {
		segment = "general"
	}

	return family + "/" + segment
}

func inferTopicFamily(typ, title, content string) string {
	t := strings.TrimSpace(strings.ToLower(typ))
	switch t {
	case "architecture", "design", "adr", "refactor":
		return "architecture"
	case "bug", "bugfix", "fix", "incident", "hotfix":
		return "bug"
	case "decision":
		return "decision"
	case "pattern", "convention", "guideline":
		return "pattern"
	case "config", "setup", "infra", "infrastructure", "ci":
		return "config"
	case "discovery", "investigation", "root_cause", "root-cause":
		return "discovery"
	case "learning", "learn":
		return "learning"
	case "session_summary":
		return "session"
	}

	text := strings.ToLower(title + " " + content)
	if hasAny(text, "bug", "fix", "panic", "error", "crash", "regression", "incident", "hotfix") {
		return "bug"
	}
	if hasAny(text, "architecture", "design", "adr", "boundary", "hexagonal", "refactor") {
		return "architecture"
	}
	if hasAny(text, "decision", "tradeoff", "chose", "choose", "decide") {
		return "decision"
	}
	if hasAny(text, "pattern", "convention", "naming", "guideline") {
		return "pattern"
	}
	if hasAny(text, "config", "setup", "environment", "env", "docker", "pipeline") {
		return "config"
	}
	if hasAny(text, "discovery", "investigate", "investigation", "found", "root cause") {
		return "discovery"
	}
	if hasAny(text, "learned", "learning") {
		return "learning"
	}

	if t != "" && t != "manual" {
		return normalizeTopicSegment(t)
	}

	return "topic"
}

func hasAny(text string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

func normalizeTopicSegment(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return ""
	}
	re := regexp.MustCompile(`[^a-z0-9]+`)
	v = re.ReplaceAllString(v, " ")
	v = strings.Join(strings.Fields(v), "-")
	if len(v) > 100 {
		v = v[:100]
	}
	return v
}

func normalizeTopicKey(topic string) string {
	v := strings.TrimSpace(strings.ToLower(topic))
	if v == "" {
		return ""
	}
	v = strings.Join(strings.Fields(v), "-")
	if len(v) > 120 {
		v = v[:120]
	}
	return v
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func hashNormalized(content string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(content), " "))
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:])
}

func dedupeWindowExpression(window time.Duration) string {
	if window <= 0 {
		window = 15 * time.Minute
	}
	minutes := int(window.Minutes())
	if minutes < 1 {
		minutes = 1
	}
	return "-" + strconv.Itoa(minutes) + " minutes"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func newSyncID(prefix string) string {
	return uuid.NewString()
}

func normalizeExistingSyncID(existing, prefix string) string {
	trimmed := strings.TrimSpace(existing)
	if trimmed != "" {
		if parsed, err := uuid.Parse(trimmed); err == nil {
			return parsed.String()
		}
		return uuidFromStableText(prefix + "|" + trimmed)
	}
	return newSyncID(prefix)
}

func preferString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeSessionID(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return uuidFromStableText("client|manual-save")
	}
	if parsed, err := uuid.Parse(trimmed); err == nil {
		return parsed.String()
	}
	return uuidFromStableText("client|" + trimmed)
}

func normalizeObservationID(id string) string {
	return normalizeExistingSyncID(id, "observation-id")
}

func normalizeImportedObservationID(id, syncID string) string {
	return normalizeImportedEntityID(id, syncID, "observation-id")
}

func normalizePromptID(id string) string {
	return normalizeExistingSyncID(id, "prompt-id")
}

func normalizeImportedEntityID(id, fallback, prefix string) string {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return normalizeExistingSyncID(fallback, prefix)
	}
	if parsed, err := uuid.Parse(trimmedID); err == nil {
		return parsed.String()
	}
	trimmedFallback := strings.TrimSpace(fallback)
	if trimmedFallback != "" {
		return uuidFromStableText(prefix + "|" + trimmedID + "|" + trimmedFallback)
	}
	return uuidFromStableText(prefix + "|" + trimmedID)
}

func normalizeObservationSyncID(id string) string {
	return normalizeExistingSyncID(id, "observation-sync")
}

func normalizePromptSyncID(id string) string {
	return normalizeExistingSyncID(id, "prompt-sync")
}

// privateTagRegex matches <private>...</private> tags and their contents.
// Supports multiline and nested content. Case-insensitive.
var privateTagRegex = regexp.MustCompile(`(?is)<private>.*?</private>`)

// stripPrivateTags removes all <private>...</private> content from a string.
// This ensures sensitive information (API keys, passwords, personal data)
// is never persisted to the memory database.
func stripPrivateTags(s string) string {
	result := privateTagRegex.ReplaceAllString(s, "[REDACTED]")
	// Clean up multiple consecutive [REDACTED] and excessive whitespace
	result = strings.TrimSpace(result)
	return result
}

// sanitizeFTS wraps each word in quotes for consistent text-query handling.
// "fix auth bug" → `"fix" "auth" "bug"`
func sanitizeFTS(query string) string {
	words := strings.Fields(query)
	for i, w := range words {
		// Strip existing quotes to avoid double-quoting
		w = strings.Trim(w, `"`)
		words[i] = `"` + w + `"`
	}
	return strings.Join(words, " ")
}

// ─── Passive Capture ─────────────────────────────────────────────────────────

// PassiveCaptureParams holds the input for passive memory capture.
type PassiveCaptureParams struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Project   string `json:"project,omitempty"`
	Source    string `json:"source,omitempty"` // e.g. "subagent-stop", "session-end"
}

// PassiveCaptureResult holds the output of passive memory capture.
type PassiveCaptureResult struct {
	Extracted  int `json:"extracted"`  // Total learnings found in text
	Saved      int `json:"saved"`      // New observations created
	Duplicates int `json:"duplicates"` // Skipped because already existed
}

// learningHeaderPattern matches section headers for learnings in both English and Spanish.
var learningHeaderPattern = regexp.MustCompile(
	`(?im)^#{2,3}\s+(?:Aprendizajes(?:\s+Clave)?|Key\s+Learnings?|Learnings?):?\s*$`,
)

const (
	minLearningLength = 20
	minLearningWords  = 4
)

// ExtractLearnings parses structured learning items from text.
// It looks for sections like "## Key Learnings:" or "## Aprendizajes Clave:"
// and extracts numbered (1. text) or bullet (- text) items.
// Returns learnings from the LAST matching section (most recent output).
func ExtractLearnings(text string) []string {
	matches := learningHeaderPattern.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}

	// Process sections in reverse — use first valid one (most recent)
	for i := len(matches) - 1; i >= 0; i-- {
		sectionStart := matches[i][1]
		sectionText := text[sectionStart:]

		// Cut off at next major section header
		if nextHeader := regexp.MustCompile(`\n#{1,3} `).FindStringIndex(sectionText); nextHeader != nil {
			sectionText = sectionText[:nextHeader[0]]
		}

		var learnings []string

		// Try numbered items: "1. text" or "1) text"
		numbered := regexp.MustCompile(`(?m)^\s*\d+[.)]\s+(.+)`).FindAllStringSubmatch(sectionText, -1)
		if len(numbered) > 0 {
			for _, m := range numbered {
				cleaned := cleanMarkdown(m[1])
				if len(cleaned) >= minLearningLength && len(strings.Fields(cleaned)) >= minLearningWords {
					learnings = append(learnings, cleaned)
				}
			}
		}

		// Fall back to bullet items: "- text" or "* text"
		if len(learnings) == 0 {
			bullets := regexp.MustCompile(`(?m)^\s*[-*]\s+(.+)`).FindAllStringSubmatch(sectionText, -1)
			for _, m := range bullets {
				cleaned := cleanMarkdown(m[1])
				if len(cleaned) >= minLearningLength && len(strings.Fields(cleaned)) >= minLearningWords {
					learnings = append(learnings, cleaned)
				}
			}
		}

		if len(learnings) > 0 {
			return learnings
		}
	}

	return nil
}

// cleanMarkdown strips basic markdown formatting and collapses whitespace.
func cleanMarkdown(text string) string {
	text = regexp.MustCompile(`\*\*([^*]+)\*\*`).ReplaceAllString(text, "$1") // bold
	text = regexp.MustCompile("`([^`]+)`").ReplaceAllString(text, "$1")       // inline code
	text = regexp.MustCompile(`\*([^*]+)\*`).ReplaceAllString(text, "$1")     // italic
	return strings.TrimSpace(strings.Join(strings.Fields(text), " "))
}

// PassiveCapture extracts learnings from text and saves them as observations.
// It deduplicates against existing observations using content hash matching.
func (s *Store) PassiveCapture(p PassiveCaptureParams) (*PassiveCaptureResult, error) {
	result := &PassiveCaptureResult{}

	learnings := ExtractLearnings(p.Content)
	result.Extracted = len(learnings)

	if len(learnings) == 0 {
		return result, nil
	}

	for _, learning := range learnings {
		// Check if this learning already exists (by content hash) within this project
		normHash := hashNormalized(learning)
		var existingID string
		err := s.queryRowHook(s.db,
			`SELECT id::text FROM observations
			 WHERE normalized_hash = ?
			   AND coalesce(project, '') = coalesce(?, '')
			   AND deleted_at IS NULL
			 LIMIT 1`,
			normHash, nullableString(p.Project),
		).Scan(&existingID)

		if err == nil {
			// Already exists — skip
			result.Duplicates++
			continue
		}

		// Truncate for title: first 60 chars
		title := learning
		if len(title) > 60 {
			title = title[:60] + "..."
		}

		_, err = s.AddObservation(AddObservationParams{
			SessionID: p.SessionID,
			Type:      "passive",
			Title:     title,
			Content:   learning,
			Project:   p.Project,
			Scope:     "project",
			ToolName:  p.Source,
		})
		if err != nil {
			return result, fmt.Errorf("passive capture save: %w", err)
		}
		result.Saved++
	}

	return result, nil
}

// ClassifyTool returns the observation type for a given tool name.
func ClassifyTool(toolName string) string {
	switch toolName {
	case "write", "edit", "patch":
		return "file_change"
	case "bash":
		return "command"
	case "read", "view":
		return "file_read"
	case "grep", "glob", "ls":
		return "search"
	default:
		return "tool_use"
	}
}

// Now returns the current time in the store timestamp format.
func Now() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}

// NormalizeRemoteURL transforms a raw git remote origin URL into a normalized
// project identifier of the form "host/owner/repo".
//
// Supported formats:
//   - HTTPS:  https://github.com/owner/repo.git  → github.com/owner/repo
//   - HTTP:   http://github.com/owner/repo        → github.com/owner/repo
//   - SSH:    git@github.com:owner/repo.git       → github.com/owner/repo
//   - Credentials: https://user:token@github.com/owner/repo.git → github.com/owner/repo
//
// Returns "" if the URL cannot be parsed or does not match any known format.
func NormalizeRemoteURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}

	// SSH format: git@host:owner/repo.git
	if strings.HasPrefix(rawURL, "git@") {
		// Match git@host:path
		rest := rawURL[4:] // strip "git@"
		colonIdx := strings.Index(rest, ":")
		if colonIdx < 0 {
			return ""
		}
		host := rest[:colonIdx]
		path := rest[colonIdx+1:]
		path = strings.TrimSuffix(path, ".git")
		if host == "" || path == "" {
			return ""
		}
		return host + "/" + path
	}

	// HTTP/HTTPS format
	if strings.Contains(rawURL, "://") {
		u, err := url.Parse(rawURL)
		if err != nil || u.Host == "" {
			return ""
		}
		// Strip credentials
		host := u.Hostname()
		path := strings.TrimPrefix(u.Path, "/")
		path = strings.TrimSuffix(path, ".git")
		if host == "" || path == "" {
			return ""
		}
		return host + "/" + path
	}

	return ""
}

// normalizeProject normalizes a project value received by the Store.
// Priority rules (in order):
//  1. Empty or whitespace-only → "unknown"
//  2. Contains "://" or starts with "git@" → NormalizeRemoteURL (raw URL passed by agent)
//  3. Already-normalized identifier (host/owner/repo format): does NOT start with "/" or
//     Windows drive letter, does NOT contain "\", has ≥2 "/" segments → preserve as-is
//  4. Contains "/" or "\" → filepath.Base() (OS absolute path fallback)
//  5. Otherwise → value unchanged
func normalizeProject(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}

	// Rule 2: raw URL passed by agent
	if strings.Contains(v, "://") || strings.HasPrefix(v, "git@") {
		normalized := NormalizeRemoteURL(v)
		if normalized == "" {
			return "unknown"
		}
		return normalized
	}

	// Rule 3: already-normalized identifier like "github.com/owner/repo"
	// Heuristic: does not start with "/" or Windows drive (e.g. "C:\"),
	// does not contain "\", and has at least 2 "/" segments.
	if !strings.HasPrefix(v, "/") &&
		!isWindowsDrivePath(v) &&
		!strings.Contains(v, "\\") &&
		strings.Count(v, "/") >= 2 {
		return v
	}

	// Rule 4: OS absolute path → basename
	if strings.Contains(v, "/") || strings.Contains(v, "\\") {
		base := filepath.Base(strings.ReplaceAll(v, "\\", "/"))
		if base == "" || base == "." || base == "/" || base == "\\" {
			return "unknown"
		}
		return base
	}

	// Rule 5: simple name, no separators
	return v
}

// isWindowsDrivePath returns true for paths like "C:\..." or "C:/..."
func isWindowsDrivePath(v string) bool {
	if len(v) >= 3 && v[1] == ':' && (v[2] == '\\' || v[2] == '/') {
		c := v[0]
		return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	return false
}
