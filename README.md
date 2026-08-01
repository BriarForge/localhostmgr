# localhostmgr

Single-binary localhost process supervisor for macOS. Keeps your `node`, `python`, `ruby`, or any shell-served app alive 24x7 via a `launchd` LaunchAgent.

```
launchd (KeepAlive=true)
   └─ localhostmgr serve     ← 5s tick loop
         ├─ [ food-calendar ] → lsof port 3300 → pid alive? → health probe → record OK
         └─ [ mission-ctrl ] → lsof port 3000 → pid dead → backoff → respawn
```

## Features

- **5s health tick** — every 5 seconds, checks each registered service's PID and optional HTTP health URL
- **Orphan adoption** — on startup, scans bound ports and adopts any pre-existing listener (zero-downtime migration from manual launch)
- **Crash backoff** — 2 fails → 5s, 5 fails → 15s, 10 fails → 60s; prevents thrash storms from config errors
- **launchd integration** — `install-launchd` writes a LaunchAgent with `KeepAlive=true`, survives logout and reboot
- **Port conflict guard** — refuses to register or start a service on a port already held by another registered service
- **SQLite registry** — `~/.local/share/localhostmgr/localhostmgr.db` (WAL mode, never under OneDrive/iCloud)
- **Per-service logs** — `~/.local/share/localhostmgr/logs/<name>.log`

## Install

```bash
# Binary already built? Put it on your PATH
cp localhostmgr /usr/local/bin/localhostmgr
chmod +x /usr/local/bin/localhostmgr

# Or build from source
go build -o /usr/local/bin/localhostmgr .
```

## Register a service

```bash
NODE=$(which node)

localhostmgr add \
  --name my-app \
  --cwd "/path/to/my-app" \
  --cmd "$NODE server.js" \
  --port 3300 \
  --health "http://localhost:3300/api/health"

# Install the launchd LaunchAgent (run once)
localhostmgr install-launchd
```

After install, the supervisor starts on boot and survives logout. Verify:

```bash
localhostmgr list
localhostmgr doctor
```

## Commands

| Command | Description |
|---|---|
| `localhostmgr serve` | Run the supervisor (called by launchd) |
| `localhostmgr add --name <id> --cwd <path> --cmd "<cmd>"` | Register a service |
| `localhostmgr list` | Show all services, state, PID, fail count |
| `localhostmgr status [name]` | Verbose status for one or all services |
| `localhostmgr start <name>` | Spawn now |
| `localhostmgr stop <name>` | Kill tracked PID |
| `localhostmgr restart <name>` | Stop then start |
| `localhostmgr enable <name>` | Supervisor will manage it |
| `localhostmgr disable <name>` | Supervisor ignores it |
| `localhostmgr remove <name>` | Unregister; best-effort stop first |
| `localhostmgr doctor` | One-shot health report |
| `localhostmgr install-launchd` | Write LaunchAgent plist and `launchctl load -w` |
| `localhostmgr uninstall-launchd` | Unload and remove plist |

## Options for `add`

| Flag | Required | Description |
|---|---|---|
| `--name` | Yes | Service identifier |
| `--cwd` | Yes | Working directory for the spawned process |
| `--cmd` | Yes | Shell command to run |
| `--port` | No | TCP port for orphan-adoption and conflict checking |
| `--health` | No | HTTP URL; 2xx = healthy, no probe = trust PID |
| `--env KEY=VAL` | No | Environment variables (repeatable) |
| `--disable` | No | Register but leave disabled |

## Health check behaviour

- **Health URL set** — PID must be alive AND HTTP probe must return 2xx-3xx
- **No health URL** — trust PID presence only (won't detect a frozen process)
- **PID dead** — clear PID, apply backoff, respawn
- **PID alive, health failing** — leave it alone for this tick (gives startup time to settle)

## PATH trap

`launchd` runs services with a minimal `PATH`. Always use absolute paths in `--cmd`:

```bash
# Wrong — will silently fail under launchd
localhostmgr add --name my-app --cwd /path --cmd "node server.js" ...

# Correct
NODE=$(which node)
localhostmgr add --name my-app --cwd /path --cmd "$NODE server.js" ...
```

The LaunchAgent plist and `buildEnv` also prepend a known-good PATH as defence-in-depth.

## Recovery proof (mandatory after install)

```bash
# Kill the running server
OLD=$(lsof -nP -iTCP:3300 -sTCP:LISTEN -F p | head -1 | tr -d p)
kill "$OLD"

# Wait one tick cycle + buffer
sleep 8

# Verify: new PID, failures cleared, health 200
localhostmgr status my-app
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:3300/api/health
```

## Uninstall

```bash
localhostmgr uninstall-launchd
localhostmgr remove my-app
```

## Data location

| File | Path |
|---|---|
| Registry DB | `~/.local/share/localhostmgr/localhostmgr.db` |
| Per-service logs | `~/.local/share/localhostmgr/logs/<name>.log` |
| Daemon logs | `~/Library/Logs/localhostmgr.{out,err}.log` |
| LaunchAgent plist | `~/Library/LaunchAgents/com.briarforge.localhostmgr.plist` |

## Requirements

- macOS (launchd is macOS-specific)
- `lsof` (built into macOS)
- Go 1.21+ (to build from source)
