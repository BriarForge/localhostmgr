// Package store manages the localhostmgr service registry backed by SQLite.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Service struct {
	Name         string
	Cwd          string
	Cmd          string
	Port         int
	HealthURL    string
	Env          map[string]string
	Enabled      bool
	PID          sql.NullInt64
	LastStartAt  sql.NullString
	LastHealthAt sql.NullString
	LastError    sql.NullString
	FailCount    int
	BuildCmd     string
	StartCmd     string
}

type Store struct {
	mu sync.Mutex
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS services (
  name TEXT PRIMARY KEY,
  cwd TEXT NOT NULL,
  cmd TEXT NOT NULL,
  port INTEGER,
  health_url TEXT,
  env_json TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  pid INTEGER,
  last_start_at TEXT,
  last_health_at TEXT,
  last_error TEXT,
  fail_count INTEGER NOT NULL DEFAULT 0,
  build_cmd TEXT DEFAULT '',
  start_cmd TEXT DEFAULT ''
);
`)
	if err != nil {
		return err
	}
	// Backfill new columns on existing schema (no-op if already present).
	_, _ = s.db.Exec(`ALTER TABLE services ADD COLUMN build_cmd TEXT DEFAULT ''`)
	_, _ = s.db.Exec(`ALTER TABLE services ADD COLUMN start_cmd TEXT DEFAULT ''`)
	return nil
}

func (s *Store) Add(svc Service) error {
	if svc.Name == "" || svc.Cmd == "" || svc.Cwd == "" {
		return errors.New("name, cmd, cwd are required")
	}
	envJSON := ""
	if len(svc.Env) > 0 {
		b, err := json.Marshal(svc.Env)
		if err != nil {
			return err
		}
		envJSON = string(b)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
INSERT INTO services (name, cwd, cmd, port, health_url, env_json, enabled, build_cmd, start_cmd)
VALUES (?,?,?,?,?,?,?,?,?)
ON CONFLICT(name) DO UPDATE SET
  cwd=excluded.cwd,
  cmd=excluded.cmd,
  port=excluded.port,
  health_url=excluded.health_url,
  env_json=excluded.env_json,
  build_cmd=excluded.build_cmd,
  start_cmd=excluded.start_cmd
`, svc.Name, svc.Cwd, svc.Cmd, nullableInt(svc.Port), svc.HealthURL, envJSON, boolToInt(svc.Enabled), svc.BuildCmd, svc.StartCmd)
	return err
}

func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM services WHERE name=?`, name)
	return err
}

func (s *Store) List() ([]Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT name, cwd, cmd, port, health_url, env_json, enabled, pid, last_start_at, last_health_at, last_error, fail_count, build_cmd, start_cmd FROM services ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		svc, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}

func (s *Store) Get(name string) (Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT name, cwd, cmd, port, health_url, env_json, enabled, pid, last_start_at, last_health_at, last_error, fail_count, build_cmd, start_cmd FROM services WHERE name=?`, name)
	return scanService(row)
}

// Update applies a partial update to an existing service record.
// Only non-zero/non-empty fields are updated; zero-value fields are skipped.
func (s *Store) Update(name string, patch Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Build dynamic UPDATE based on which fields are set.
	if patch.Cwd != "" {
		_, _ = s.db.Exec(`UPDATE services SET cwd=? WHERE name=?`, patch.Cwd, name)
	}
	if patch.Cmd != "" {
		_, _ = s.db.Exec(`UPDATE services SET cmd=? WHERE name=?`, patch.Cmd, name)
	}
	if patch.Port != 0 {
		_, _ = s.db.Exec(`UPDATE services SET port=? WHERE name=?`, patch.Port, name)
	}
	if patch.HealthURL != "" {
		_, _ = s.db.Exec(`UPDATE services SET health_url=? WHERE name=?`, patch.HealthURL, name)
	}
	if patch.BuildCmd != "" {
		_, _ = s.db.Exec(`UPDATE services SET build_cmd=? WHERE name=?`, patch.BuildCmd, name)
	}
	if patch.StartCmd != "" {
		_, _ = s.db.Exec(`UPDATE services SET start_cmd=? WHERE name=?`, patch.StartCmd, name)
	}
	if len(patch.Env) > 0 {
		b, _ := json.Marshal(patch.Env)
		_, _ = s.db.Exec(`UPDATE services SET env_json=? WHERE name=?`, string(b), name)
	}
	return nil
}

func (s *Store) SetEnabled(name string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE services SET enabled=? WHERE name=?`, boolToInt(enabled), name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("service %q not found", name)
	}
	return nil
}

func (s *Store) UpdatePID(name string, pid int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE services SET pid=?, last_start_at=? WHERE name=?`, pid, time.Now().UTC().Format(time.RFC3339), name)
	return err
}

func (s *Store) ClearPID(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE services SET pid=NULL WHERE name=?`, name)
	return err
}

func (s *Store) RecordHealth(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE services SET last_health_at=?, fail_count=0, last_error=NULL WHERE name=?`, time.Now().UTC().Format(time.RFC3339), name)
	return err
}

func (s *Store) RecordFailure(name string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE services SET fail_count=fail_count+1, last_error=? WHERE name=?`, errMsg, name)
	return err
}

// PortInUse returns true if another service (excluding exceptName) is registered on port.
// Port 0 = "no port" = never conflicts.
func (s *Store) PortInUse(exceptName string, port int) (bool, error) {
	if port == 0 {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT name FROM services WHERE port=? AND name!=? LIMIT 1`, port, exceptName)
	var name string
	if err := row.Scan(&name); err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) EnvMap(svc Service) map[string]string {
	if svc.Env == nil {
		return map[string]string{}
	}
	return svc.Env
}

// --- helpers ---------------------------------------------------------------

func scanService(row interface {
	Scan(...interface{}) error
}) (Service, error) {
	var svc Service
	var envJSON sql.NullString
	var port sql.NullInt64
	var enabled int
	if err := row.Scan(&svc.Name, &svc.Cwd, &svc.Cmd, &port, &svc.HealthURL, &envJSON, &enabled,
		&svc.PID, &svc.LastStartAt, &svc.LastHealthAt, &svc.LastError, &svc.FailCount,
		&svc.BuildCmd, &svc.StartCmd); err != nil {
		return Service{}, err
	}
	if port.Valid {
		svc.Port = int(port.Int64)
	}
	svc.Enabled = enabled != 0
	if envJSON.Valid && envJSON.String != "" {
		_ = json.Unmarshal([]byte(envJSON.String), &svc.Env)
	}
	return svc, nil
}

func nullableInt(v int) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
