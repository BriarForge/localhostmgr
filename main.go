// localhostmgr — single-binary localhost process supervisor.
// Subcommands: serve | add | list | status | start | stop | restart | rebuild | update | enable | disable | remove | doctor | install-launchd | uninstall-launchd
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"localhostmgr/internal/launchd"
	"localhostmgr/internal/server"
	"localhostmgr/internal/store"
	"localhostmgr/internal/supervisor"
)

const (
	portalPortDefault = 19999
)

const usage = `localhostmgr — supervise localhost services.

Usage:
  localhostmgr serve
  localhostmgr add --name <id> --cwd <path> --cmd "<sh -c string>" [--port N] [--health URL] [--env KEY=VAL ...] [--build-cmd CMD] [--start-cmd CMD] [--disable]
  localhostmgr list
  localhostmgr status [name]
  localhostmgr start <name>        # spawn now, record pid
  localhostmgr stop <name>         # kill tracked pid (if alive)
  localhostmgr restart <name>      # stop then start
  localhostmgr rebuild <name>      # run build command without restarting
  localhostmgr enable <name>
  localhostmgr disable <name>
  localhostmgr update <name>       # update fields then restart if running
  localhostmgr remove <name>
  localhostmgr doctor
  localhostmgr install-launchd    # writes com.briarforge.localhostmgr.plist and load -w
  localhostmgr uninstall-launchd
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	st, err := openStore()
	if err != nil {
		fatal("open store: %v", err)
	}
	defer st.Close()

	switch cmd {
	case "serve":
		runServe(st)
	case "add":
		runAdd(st, args)
	case "list", "ls":
		runList(st)
	case "status":
		runStatus(st, args)
	case "start":
		requireName(args, "start")
		runStart(st, args[0])
	case "stop":
		requireName(args, "stop")
		runStop(st, args[0])
	case "restart":
		requireName(args, "restart")
		runRestart(st, args[0])
	case "rebuild":
		requireName(args, "rebuild")
		runRebuild(st, args[0])
	case "update":
		requireName(args, "update")
		runUpdate(st, args[0], args[1:])
	case "enable":
		requireName(args, "enable")
		runEnable(st, args[0], true)
	case "disable":
		requireName(args, "disable")
		runEnable(st, args[0], false)
	case "remove", "rm":
		requireName(args, "remove")
		runRemove(st, args[0])
	case "doctor":
		runDoctor(st)
	case "install-launchd":
		runInstallLaunchd()
	case "uninstall-launchd":
		runUninstallLaunchd()
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

// --- path resolution --------------------------------------------------------

func dataDir() (string, error) {
	// Honour LOCALHOSTMGR_DATA_DIR for tests; default to ~/.local/share/localhostmgr
	if v := os.Getenv("LOCALHOSTMGR_DATA_DIR"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "localhostmgr"), nil
}

func openStore() (*store.Store, error) {
	dir, err := dataDir()
	if err != nil {
		return nil, err
	}
	return store.Open(filepath.Join(dir, "localhostmgr.db"))
}

// --- commands ---------------------------------------------------------------

func runServe(st *store.Store) {
	dir, _ := dataDir()
	logPath := filepath.Join(dir, "logs", "daemon.out.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		fatal("mkdir logs: %v", err)
	}
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fatal("open daemon log: %v", err)
	}
	defer lf.Close()
	logf := func(format string, a ...interface{}) {
		ts := time.Now().Format(time.RFC3339)
		fmt.Fprintf(lf, ts+" "+format+"\n", a...)
	}
	logf("serve: starting (pid=%d)", os.Getpid())
	// Adopt: for each service, if its port is already bound by another process,
	// record that pid instead of spawning a new one.
	svcs, err := st.List()
	if err != nil {
		fatal("list: %v", err)
	}
	for _, svc := range svcs {
		if svc.Port > 0 {
			if pid, ok := pidOnPort(svc.Port); ok {
				if err := st.UpdatePID(svc.Name, pid); err == nil {
					logf("serve: adopted %s pid=%d on port %d", svc.Name, pid, svc.Port)
				}
			}
		}
	}

	// Build a supervisor and start the portal alongside it.
	s := supervisor.New(st, logf)
	daemonInfo := server.DaemonInfo{
		PID:       os.Getpid(),
		StartedAt:  time.Now(),
		PortalPort: portalPortDefault,
		Version:    runtime.Version(),
	}
	srv := server.New(st, daemonInfo, logf, s)

	// Start portal in background goroutine so supervisor loop can run concurrently.
	go func() {
		if err := srv.Serve(); err != nil {
			logf("portal: server error: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Run(ctx)
}

func runAdd(st *store.Store, args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	name := fs.String("name", "", "service name (required)")
	cwd := fs.String("cwd", "", "working directory (required)")
	cmdStr := fs.String("cmd", "", "shell command (required)")
	port := fs.Int("port", 0, "listening port (optional)")
	health := fs.String("health", "", "health probe URL (optional)")
	buildCmd := fs.String("build-cmd", "", "build command (run before start; e.g. 'npm run build')")
	startCmd := fs.String("start-cmd", "", "start command (overrides cmd; e.g. 'npm start')")
	disable := fs.Bool("disable", false, "register but leave disabled")
	var envFlags multiFlag
	fs.Var(&envFlags, "env", "KEY=VAL pair (repeatable)")
	if err := fs.Parse(args); err != nil {
		fatal("%v", err)
	}
	if *name == "" || *cwd == "" || *cmdStr == "" {
		fatal("--name, --cwd, --cmd are required")
	}
	// Refuse duplicate port registration.
	if *port > 0 {
		inUse, err := st.PortInUse(*name, *port)
		if err != nil {
			fatal("port check: %v", err)
		}
		if inUse {
			fatal("port %d is already registered to another service", *port)
		}
	}
	envMap, err := parseEnv(envFlags)
	if err != nil {
		fatal("%v", err)
	}
	if err := st.Add(store.Service{
		Name:      *name,
		Cwd:       *cwd,
		Cmd:       *cmdStr,
		Port:      *port,
		HealthURL: *health,
		Env:       envMap,
		Enabled:   !*disable,
		BuildCmd:  *buildCmd,
		StartCmd:  *startCmd,
	}); err != nil {
		fatal("add: %v", err)
	}
	if *buildCmd != "" {
		fmt.Printf("added %s (cwd=%s cmd=%q build=%q start=%q)\n", *name, *cwd, *cmdStr, *buildCmd, *startCmd)
	} else {
		fmt.Printf("added %s (cwd=%s cmd=%q)\n", *name, *cwd, *cmdStr)
	}
}

func runList(st *store.Store) {
	svcs, err := st.List()
	if err != nil {
		fatal("list: %v", err)
	}
	if len(svcs) == 0 {
		fmt.Println("(no services registered)")
		return
	}
	fmt.Printf("%-20s %-7s %-7s %-25s %s\n", "NAME", "STATE", "PORT", "PID", "FAIL")
	for _, svc := range svcs {
		state := "disabled"
		if svc.Enabled {
			if isRunning(svc) {
				state = "running"
			} else {
				state = "stopped"
			}
		}
		pidStr := "-"
		if svc.PID.Valid {
			pidStr = strconv.FormatInt(svc.PID.Int64, 10)
		}
		fmt.Printf("%-20s %-7s %-7d %-25s %d\n", svc.Name, state, svc.Port, pidStr, svc.FailCount)
	}
}

func runStatus(st *store.Store, args []string) {
	svcs, err := st.List()
	if err != nil {
		fatal("list: %v", err)
	}
	if len(args) > 0 {
		name := args[0]
		var found *store.Service
		for i := range svcs {
			if svcs[i].Name == name {
				found = &svcs[i]
				break
			}
		}
		if found == nil {
			fatal("service %q not found", name)
		}
		printStatus(*found)
		return
	}
	for _, svc := range svcs {
		printStatus(svc)
		fmt.Println()
	}
}

func printStatus(svc store.Service) {
	state := "disabled"
	if svc.Enabled {
		if isRunning(svc) {
			state = "running"
		} else {
			state = "stopped"
		}
	}
	pidStr := "-"
	if svc.PID.Valid {
		pidStr = strconv.FormatInt(svc.PID.Int64, 10)
	}
	fmt.Printf("name      : %s\n", svc.Name)
	fmt.Printf("state     : %s\n", state)
	fmt.Printf("cwd       : %s\n", svc.Cwd)
	fmt.Printf("cmd       : %s\n", svc.Cmd)
	if svc.Port > 0 {
		fmt.Printf("port      : %d\n", svc.Port)
	}
	if svc.HealthURL != "" {
		fmt.Printf("health    : %s\n", svc.HealthURL)
	}
	fmt.Printf("pid       : %s\n", pidStr)
	fmt.Printf("enabled   : %v\n", svc.Enabled)
	fmt.Printf("failures  : %d\n", svc.FailCount)
	if svc.LastError.Valid && svc.LastError.String != "" {
		fmt.Printf("last error: %s\n", svc.LastError.String)
	}
	if svc.LastHealthAt.Valid && svc.LastHealthAt.String != "" {
		fmt.Printf("last ok   : %s\n", svc.LastHealthAt.String)
	}
	if svc.LastStartAt.Valid && svc.LastStartAt.String != "" {
		fmt.Printf("last start: %s\n", svc.LastStartAt.String)
	}
}

func runStart(st *store.Store, name string) {
	svc, err := st.Get(name)
	if err != nil {
		fatal("get: %v", err)
	}
	if isRunning(svc) {
		fmt.Printf("%s: already running (pid=%d)\n", name, pidOf(svc))
		return
	}
	s := supervisor.New(st, func(format string, a ...interface{}) {
		fmt.Printf(format+"\n", a...)
	})
	if err := s.Start(svc); err != nil {
		fatal("start %s: %v", name, err)
	}
	updated, _ := st.Get(name)
	pid := pidOf(updated)
	fmt.Printf("started %s pid=%d\n", name, pid)
}

func runStop(st *store.Store, name string) {
	svc, err := st.Get(name)
	if err != nil {
		fatal("get: %v", err)
	}
	pid := pidOf(svc)
	if pid == 0 {
		fmt.Printf("%s: no tracked pid\n", name)
		return
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "kill %d: %v\n", pid, err)
	}
	_ = st.ClearPID(name)
	fmt.Printf("stopped %s (pid=%d)\n", name, pid)
}

func runRestart(st *store.Store, name string) {
	runStop(st, name)
	time.Sleep(500 * time.Millisecond)
	runStart(st, name)
}

func runEnable(st *store.Store, name string, on bool) {
	if err := st.SetEnabled(name, on); err != nil {
		fatal("%v", err)
	}
	if on {
		fmt.Printf("enabled %s\n", name)
	} else {
		fmt.Printf("disabled %s\n", name)
	}
}

func runRemove(st *store.Store, name string) {
	svc, err := st.Get(name)
	if err == nil {
		// Best-effort stop
		pid := pidOf(svc)
		if pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	}
	if err := st.Delete(name); err != nil {
		fatal("delete: %v", err)
	}
	fmt.Printf("removed %s\n", name)
}

func runDoctor(st *store.Store) {
	fmt.Println("=== localhostmgr doctor ===")
	fmt.Printf("go runtime: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	dir, _ := dataDir()
	fmt.Printf("data dir  : %s\n", dir)
	if ok, p, err := launchd.Installed(); err != nil {
		fmt.Printf("launchd   : ERROR %v\n", err)
	} else {
		state := "not installed"
		if ok {
			state = "installed at " + p
		}
		fmt.Printf("launchd   : %s\n", state)
	}
	fmt.Println()
	svcs, err := st.List()
	if err != nil {
		fatal("list: %v", err)
	}
	if len(svcs) == 0 {
		fmt.Println("(no services registered)")
		return
	}
	fmt.Printf("%-20s %-7s %-5s %-7s %s\n", "NAME", "STATE", "PORT", "PID", "FAIL")
	for _, svc := range svcs {
		state := "disabled"
		if svc.Enabled {
			if isRunning(svc) {
				state = "running"
			} else {
				state = "stopped"
			}
		}
		pidStr := "-"
		if svc.PID.Valid {
			pidStr = strconv.FormatInt(svc.PID.Int64, 10)
		}
		fmt.Printf("%-20s %-7s %-5d %-7s %d\n", svc.Name, state, svc.Port, pidStr, svc.FailCount)
	}
}

func runInstallLaunchd() {
	exe, err := os.Executable()
	if err != nil {
		fatal("cannot determine executable path: %v", err)
	}
	// Resolve symlinks so the plist points at the real binary, not /tmp/.
	resolved, err := filepath.EvalSymlinks(exe)
	if err == nil {
		exe = resolved
	}
	if err := launchd.Install(exe); err != nil {
		fatal("install-launchd: %v", err)
	}
	fmt.Printf("installed LaunchAgent %s\n", launchd.Label)
}

func runUninstallLaunchd() {
	if err := launchd.Uninstall(); err != nil {
		fatal("uninstall-launchd: %v", err)
	}
	fmt.Println("uninstalled LaunchAgent")
}

// --- helpers ----------------------------------------------------------------

func requireName(args []string, verb string) {
	if len(args) < 1 {
		fatal("%s requires a service name", verb)
	}
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func parseEnv(flags []string) (map[string]string, error) {
	out := map[string]string{}
	for _, kv := range flags {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			return nil, fmt.Errorf("invalid --env %q (expected KEY=VAL)", kv)
		}
		out[kv[:i]] = kv[i+1:]
	}
	return out, nil
}

func fatal(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "localhostmgr: "+format+"\n", a...)
	os.Exit(1)
}

func pidOf(svc store.Service) int {
	if svc.PID.Valid {
		return int(svc.PID.Int64)
	}
	return 0
}

// isRunning checks both: (a) tracked pid alive, (b) health URL returns 2xx.
// If no health URL is configured, fall back to pid-only check.
func isRunning(svc store.Service) bool {
	pid := pidOf(svc)
	if pid <= 0 {
		return false
	}
	if !processAlive(pid) {
		return false
	}
	if svc.HealthURL == "" {
		return true
	}
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(svc.HealthURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := p.Signal(syscall.Signal(0)); err == nil {
		return true
	}
	return strings.Contains(err.Error(), "operation not permitted")
}

// pidOnPort returns the pid bound to a TCP port, if any.
// Implemented via lsof on macOS (no netstat parsing needed).
func pidOnPort(port int) (int, bool) {
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
	if !strings.HasPrefix(s, "p") {
		return 0, false
	}
	pid, err := strconv.Atoi(s[1:])
	if err != nil {
		return 0, false
	}
	return pid, true
}

// lsofReachable is unused but kept to expose that we depend on lsof.
var _ = net.IPv4

// --- new commands -----------------------------------------------------------

func runRebuild(st *store.Store, name string) {
	sv, err := st.Get(name)
	if err != nil {
		fatal("get: %v", err)
	}
	if sv.BuildCmd == "" {
		fatal("no build_cmd set for %q — set one with: localhostmgr update --build-cmd 'npm run build' %s", name, name)
	}
	s := supervisor.New(st, func(string, ...interface{}) {})
	if err := s.Rebuild(sv); err != nil {
		fatal("rebuild failed: %v", err)
	}
	fmt.Printf("rebuilt %s (build_cmd=%q)\n", name, sv.BuildCmd)
}

func runUpdate(st *store.Store, name string, args []string) {
	// Fetch current record so we can overlay flags.
	current, err := st.Get(name)
	if err != nil {
		fatal("get: %v", err)
	}
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	cwd := fs.String("cwd", current.Cwd, "working directory")
	cmd := fs.String("cmd", current.Cmd, "shell command")
	port := fs.Int("port", current.Port, "listening port")
	health := fs.String("health", current.HealthURL, "health probe URL")
	buildCmd := fs.String("build-cmd", current.BuildCmd, "build command (run before start)")
	startCmd := fs.String("start-cmd", current.StartCmd, "start command (overrides cmd)")
	disable := fs.Bool("disable", false, "disable the service")
	enable := fs.Bool("enable", false, "enable the service")
	if err := fs.Parse(args); err != nil {
		fatal("%v", err)
	}
	patch := store.Service{
		Cwd:       *cwd,
		Cmd:       *cmd,
		Port:      *port,
		HealthURL: *health,
		BuildCmd:  *buildCmd,
		StartCmd:  *startCmd,
	}
	if err := st.Update(name, patch); err != nil {
		fatal("update: %v", err)
	}
	// Handle enable/disable.
	switch {
	case *enable:
		if err := st.SetEnabled(name, true); err != nil {
			fatal("enable: %v", err)
		}
		fmt.Printf("enabled %s\n", name)
	case *disable:
		if err := st.SetEnabled(name, false); err != nil {
			fatal("disable: %v", err)
		}
		fmt.Printf("disabled %s\n", name)
	}
	fmt.Printf("updated %s\n", name)
}
