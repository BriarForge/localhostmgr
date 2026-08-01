// Package supervisor runs the periodic health check + (re)start loop.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"localhostmgr/internal/store"
)

type Supervisor struct {
	st     *store.Store
	period time.Duration
	client *http.Client
	logf   func(string, ...interface{})
}

func New(st *store.Store, logf func(string, ...interface{})) *Supervisor {
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}
	return &Supervisor{
		st:     st,
		period: 5 * time.Second,
		client: &http.Client{Timeout: 3 * time.Second},
		logf:   logf,
	}
}

func (s *Supervisor) Run(ctx context.Context) {
	t := time.NewTicker(s.period)
	defer t.Stop()
	s.tick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick()
		}
	}
}

func (s *Supervisor) tick() {
	svcs, err := s.st.List()
	if err != nil {
		s.logf("supervisor: list error: %v", err)
		return
	}
	for _, svc := range svcs {
		if !svc.Enabled {
			continue
		}
		s.checkOne(svc)
	}
}

func (s *Supervisor) checkOne(svc store.Service) {
	// 1. PID alive?
	pid := pidOf(svc)
	if pid > 0 && processAlive(pid) {
		// 2. health probe
		if svc.HealthURL != "" && s.healthOK(svc.HealthURL) {
			_ = s.st.RecordHealth(svc.Name)
			return
		}
		if svc.HealthURL == "" {
			// No health URL — trust PID presence
			_ = s.st.RecordHealth(svc.Name)
			return
		}
		// PID alive but unhealthy: leave it (give it time), only restart on hard failure
		s.logf("supervisor[%s]: pid=%d alive but health check failed", svc.Name, pid)
		return
	}

	// PID missing or dead — clear & (re)start
	if pid > 0 {
		s.logf("supervisor[%s]: pid=%d dead, restarting", svc.Name, pid)
		_ = s.st.ClearPID(svc.Name)
	}

	// Backoff: if many consecutive fails, wait longer
	failCount := svc.FailCount
	backoff := time.Duration(0)
	switch {
	case failCount >= 10:
		backoff = 60 * time.Second
	case failCount >= 5:
		backoff = 15 * time.Second
	case failCount >= 2:
		backoff = 5 * time.Second
	}
	if backoff > 0 {
		// Check last_start_at vs now
		if svc.LastStartAt.Valid {
			if t, err := time.Parse(time.RFC3339, svc.LastStartAt.String); err == nil {
				if time.Since(t) < backoff {
					return
				}
			}
		}
	}

	if err := s.Start(svc); err != nil {
		s.logf("supervisor[%s]: start error: %v", svc.Name, err)
		_ = s.st.RecordFailure(svc.Name, err.Error())
	}
}

// Stop sends SIGTERM to the service's tracked PID and clears it from the store.
func (s *Supervisor) Stop(name string) error {
	svc, err := s.st.Get(name)
	if err != nil {
		return err
	}
	pid := pidOf(svc)
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	_ = s.st.ClearPID(name)
	s.logf("supervisor[%s]: stopped", name)
	return nil
}

// Restart stops and immediately starts the service.
func (s *Supervisor) Restart(name string) error {
	svc, err := s.st.Get(name)
	if err != nil {
		return err
	}
	// Stop first (ignore errors — may already be dead).
	s.Stop(name)
	time.Sleep(500 * time.Millisecond)
	return s.Start(svc)
}

// Start spawns the service's shell command and records the PID in the store.
func (s *Supervisor) Start(svc store.Service) error {
	if svc.Cwd == "" {
		return errors.New("cwd is empty")
	}
	if _, err := os.Stat(svc.Cwd); err != nil {
		return fmt.Errorf("cwd %q not accessible: %w", svc.Cwd, err)
	}
	// Port conflict: refuse to start if another service owns this port.
	if svc.Port > 0 {
		if existing, ok := portOwnerPID(svc.Port); ok && existing != pidOf(svc) {
			return fmt.Errorf("port %d is in use by pid %d (another service)", svc.Port, existing)
		}
	}
	logDir, _ := defaultLogDir()
	_ = os.MkdirAll(logDir, 0o755)
	logPath := fmt.Sprintf("%s/%s.log", logDir, svc.Name)
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer lf.Close()

	cmd := exec.Command("sh", "-c", svc.Cmd)
	cmd.Dir = svc.Cwd
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = buildEnv(svc)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	pid := cmd.Process.Pid
	if err := s.st.UpdatePID(svc.Name, pid); err != nil {
		// DB error after spawn — kill the orphan so we don't leak
		_ = cmd.Process.Kill()
		return fmt.Errorf("record pid: %w", err)
	}
	s.logf("supervisor[%s]: started pid=%d cmd=%q cwd=%s", svc.Name, pid, svc.Cmd, svc.Cwd)

	// Reap in background; on early death count a failure so backoff kicks in next tick.
	go func(name string, c *exec.Cmd, svc store.Service) {
		err := c.Wait()
		_ = s.st.ClearPID(name)
		if err != nil {
			s.logf("supervisor[%s]: child exited: %v", name, err)
			_ = s.st.RecordFailure(name, err.Error())
		} else if c.ProcessState != nil && !c.ProcessState.Success() {
			// Process exited with non-zero status — almost always a startup error
			// (e.g. "node: command not found"). Backoff is required to prevent thrash.
			s.logf("supervisor[%s]: child exited with non-zero status (%s)", name, c.ProcessState.String())
			_ = s.st.RecordFailure(name, c.ProcessState.String())
		}
	}(svc.Name, cmd, svc)
	return nil
}

func (s *Supervisor) healthOK(url string) bool {
	resp, err := s.client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func pidOf(svc store.Service) int {
	if svc.PID.Valid {
		return int(svc.PID.Int64)
	}
	return 0
}

// processAlive returns true if pid resolves to a running process.
// On macOS this uses kill -0 which is the canonical "is this pid alive" probe.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// ESRCH = no such process; EPERM = exists but no permission — treat EPERM as alive
	return strings.Contains(err.Error(), "operation not permitted")
}

// portOwnerPID returns the PID bound to a TCP port, or 0, false if none.
// Uses lsof on macOS.
func portOwnerPID(port int) (int, bool) {
	if port <= 0 {
		return 0, false
	}
	out, err := exec.Command("sh", "-c",
		fmt.Sprintf("lsof -nP -iTCP:%d -sTCP:LISTEN -F p 2>/dev/null | head -1", port),
	).Output()
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(out))
	s = strings.TrimPrefix(s, "p")
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return pid, true
}

func buildEnv(svc store.Service) []string {
	env := os.Environ()
	merged := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			merged[kv[:i]] = kv[i+1:]
		}
	}
	for k, v := range svc.Env {
		merged[k] = v
	}
	// Always ensure node-relevant bins are reachable, even under launchd's minimal PATH.
	extra := "/usr/local/bin:/usr/local/Cellar/node@22/22.22.3/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	if existing, ok := merged["PATH"]; ok && existing != "" {
		merged["PATH"] = extra + ":" + existing
	} else {
		merged["PATH"] = extra
	}
	if _, ok := merged["HOME"]; !ok {
		if h, err := os.UserHomeDir(); err == nil {
			merged["HOME"] = h
		}
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	return out
}

func defaultLogDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := home + "/.local/share/localhostmgr/logs"
	return dir, nil
}
