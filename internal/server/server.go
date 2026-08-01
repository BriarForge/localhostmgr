// Package server provides the embedded web portal for localhostmgr.
package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"localhostmgr/internal/store"
)

// Server is the portal HTTP server.
// server package doesn't import supervisor (which has tick-loop side-effects).
type serviceController interface {
	Start(svc store.Service) error
	Stop(name string) error
	Restart(name string) error
}

// Server is the portal HTTP server.
type Server struct {
	st      *store.Store
	port    int
	logf    func(string, ...interface{})
	svcCtrl serviceController
	httpSrv *http.Server
}

// New builds a Server ready to serve on the given port.
func New(st *store.Store, port int, logf func(string, ...interface{}), ctrl serviceController) *Server {
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}
	return &Server{st: st, port: port, logf: logf, svcCtrl: ctrl}
}

// Serve starts the portal HTTP server and blocks.
func (s *Server) Serve() error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /api/services", s.handleList)
	mux.HandleFunc("POST /api/services/{name}/start", s.handleStart)
	mux.HandleFunc("POST /api/services/{name}/stop", s.handleStop)
	mux.HandleFunc("POST /api/services/{name}/restart", s.handleRestart)
	mux.HandleFunc("POST /api/services/{name}/enable", s.handleEnable)
	mux.HandleFunc("POST /api/services/{name}/disable", s.handleDisable)
	mux.HandleFunc("DELETE /api/services/{name}", s.handleRemove)
	mux.HandleFunc("GET /api/logs/{name}", s.handleLog)

	// No static asset serving needed — favicon is handled inline or skipped.
	// (Favicon is purely decorative; the page is fully functional without it.)

	s.httpSrv = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	s.logf("portal: listening on http://localhost:%d", s.port)
	return s.httpSrv.ListenAndServe()
}

// Shutdown gracefully shuts down the portal server.
func (s *Server) Shutdown() error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Close()
}

// --- handlers ---------------------------------------------------------------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	svcs, err := s.st.List()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	statuses := s.collectStatuses(svcs)
	Render(w, statuses)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	svcs, err := s.st.List()
	if err != nil {
		writeJSON(w, nil, err)
		return
	}
	out := make([]svcJSON, 0, len(svcs))
	for _, svc := range svcs {
		out = append(out, toJSON(svc))
	}
	writeJSON(w, out, nil)
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	svc, err := s.st.Get(name)
	if err != nil {
		writeJSON(w, nil, err)
		return
	}
	if err := s.svcCtrl.Start(svc); err != nil {
		writeJSON(w, nil, err)
		return
	}
	writeJSON(w, map[string]string{"status": "started"}, nil)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.svcCtrl.Stop(name); err != nil {
		writeJSON(w, nil, err)
		return
	}
	writeJSON(w, map[string]string{"status": "stopped"}, nil)
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.svcCtrl.Restart(name); err != nil {
		writeJSON(w, nil, err)
		return
	}
	writeJSON(w, map[string]string{"status": "restarted"}, nil)
}

func (s *Server) handleEnable(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.st.SetEnabled(name, true); err != nil {
		writeJSON(w, nil, err)
		return
	}
	writeJSON(w, map[string]string{"status": "enabled"}, nil)
}

func (s *Server) handleDisable(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.st.SetEnabled(name, false); err != nil {
		writeJSON(w, nil, err)
		return
	}
	writeJSON(w, map[string]string{"status": "disabled"}, nil)
}

func (s *Server) handleRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	_ = s.svcCtrl.Stop(name)
	if err := s.st.Delete(name); err != nil {
		writeJSON(w, nil, err)
		return
	}
	writeJSON(w, map[string]string{"status": "removed"}, nil)
}

func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	logDir, _ := defaultLogDir()
	logPath := fmt.Sprintf("%s/%s.log", logDir, name)
	data, err := readLastLines(logPath, 200)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

// --- helpers ----------------------------------------------------------------

type svcJSON struct {
	Name         string `json:"name"`
	Cmd          string `json:"cmd"`
	Cwd          string `json:"cwd"`
	Port         int    `json:"port"`
	HealthURL    string `json:"health_url"`
	Enabled      bool   `json:"enabled"`
	PID          int64  `json:"pid"`
	FailCount    int    `json:"fail_count"`
	LastError    string `json:"last_error"`
	LastStartAt  string `json:"last_start_at"`
	LastHealthAt string `json:"last_health_at"`
	State        string `json:"state"`
}

func toJSON(svc store.Service) svcJSON {
	state := "disabled"
	if svc.Enabled {
		if svc.PID.Valid {
			state = "running"
		} else {
			state = "stopped"
		}
	}
	pid := int64(0)
	if svc.PID.Valid {
		pid = svc.PID.Int64
	}
	lastError := ""
	if svc.LastError.Valid {
		lastError = svc.LastError.String
	}
	lastStart := ""
	if svc.LastStartAt.Valid {
		lastStart = svc.LastStartAt.String
	}
	lastHealth := ""
	if svc.LastHealthAt.Valid {
		lastHealth = svc.LastHealthAt.String
	}
	return svcJSON{
		Name:         svc.Name,
		Cmd:          svc.Cmd,
		Cwd:          svc.Cwd,
		Port:         svc.Port,
		HealthURL:    svc.HealthURL,
		Enabled:      svc.Enabled,
		PID:          pid,
		FailCount:    svc.FailCount,
		LastError:    lastError,
		LastStartAt:  lastStart,
		LastHealthAt: lastHealth,
		State:        state,
	}
}

func (s *Server) collectStatuses(svcs []store.Service) []svcJSON {
	out := make([]svcJSON, 0, len(svcs))
	for _, svc := range svcs {
		out = append(out, toJSON(svc))
	}
	return out
}

func writeJSON(w http.ResponseWriter, v interface{}, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		fmt.Fprintf(w, `{"error":%q}`, err.Error())
		return
	}
	if m, ok := v.(map[string]string); ok {
		fmt.Fprintf(w, `{"status":%q}`, m["status"])
		return
	}
	js, ok := v.([]svcJSON)
	if !ok {
		return
	}
	fmt.Fprint(w, "[")
	for i, svc := range js {
		if i > 0 {
			fmt.Fprint(w, ",")
		}
		fmt.Fprintf(w, `{"name":%q,"cmd":%q,"cwd":%q,"port":%d,"health_url":%q,"enabled":%t,"pid":%d,"fail_count":%d,"last_error":%q,"last_start_at":%q,"last_health_at":%q,"state":%q}`,
			svc.Name, svc.Cmd, svc.Cwd, svc.Port, svc.HealthURL, svc.Enabled,
			svc.PID, svc.FailCount,
			svc.LastError, svc.LastStartAt, svc.LastHealthAt,
			svc.State)
	}
	fmt.Fprint(w, "]")
}

func defaultLogDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + "/.local/share/localhostmgr/logs", nil
}

func readLastLines(path string, n int) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	if n >= len(lines) {
		return data, nil
	}
	return []byte(strings.Join(lines[len(lines)-n:], "\n")), nil
}
