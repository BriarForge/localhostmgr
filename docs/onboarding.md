# localhostmgr Web App Onboarding

A guide for adding a new web app to `localhostmgr` for 24x7 supervision.

---

## TL;DR

```bash
# 1. Register the app
localhostmgr add \
  --name my-app \
  --cwd /path/to/my-app \
  --cmd "npm run dev" \
  --port 3000 \
  --health http://localhost:3000/ \
  --build-cmd "npm run build" \
  --start-cmd "npm start"

# 2. Start it
localhostmgr start my-app

# 3. Verify it's up
localhostmgr list
curl http://localhost:3000/
```

---

## Step by step

### 1. Choose a port

Pick a unique port not already in use. Check existing registrations:

```bash
localhostmgr list
```

### 2. Know your app's commands

Most Node/Next.js apps have three relevant scripts in `package.json`:

| Script | When to use |
|---|---|
| `npm run dev` | Development server with hot reload (default for local dev) |
| `npm run build` | Production build step — required before `npm start` |
| `npm start` | Production server (after build) |

For `localhostmgr`:
- **`--cmd`** — the command to run day-to-day (usually `npm run dev`)
- **`--build-cmd`** — run automatically before `--start-cmd` on every restart. Omit if no build step needed.
- **`--start-cmd`** — overrides `--cmd` after a build. Used for production starts.

**Tip:** If the app uses a custom port, set it with `--env PORT=3001` or embed it in the start command: `PORT=3001 npx next start`.

### 3. Register the service

```bash
localhostmgr add \
  --name my-app \
  --cwd /full/path/to/my-app \
  --cmd "npx next dev" \
  --port 3000 \
  --health http://localhost:3000/ \
  --build-cmd "npx next build" \
  --start-cmd "npx next start"
```

Required flags:
- `--name` — unique identifier (used in all subsequent commands)
- `--cwd` — absolute path to the app root (where `package.json` lives)
- `--cmd` — how to start the dev server

Optional flags:
- `--port` — for the portal status display and port-collision guard
- `--health` — URL `localhostmgr` hits every 5s to confirm the service is responding
- `--build-cmd` — run this before `--start-cmd` on every restart
- `--start-cmd` — replaces `--cmd` after a build (for production starts)
- `--env KEY=VAL` — environment variable (repeatable for multiple vars)
- `--disable` — register but leave disabled (won't auto-start)

### 4. Verify

```bash
# Check it's listed and running
localhostmgr list

# Hit the health endpoint
curl http://localhost:3000/

# Watch the logs
tail -f ~/.local/share/localhostmgr/logs/my-app.log
```

---

## Common app types

### Next.js (with build + production start)

```bash
localhostmgr add \
  --name my-next-app \
  --cwd /path/to/my-next-app \
  --cmd "npx next dev" \
  --port 3000 \
  --health http://localhost:3000/ \
  --build-cmd "npx next build" \
  --start-cmd "npx next start"
```

### Next.js with custom port

```bash
localhostmgr add \
  --name my-next-app \
  --cwd /path/to/my-next-app \
  --cmd "PORT=3001 npx next dev" \
  --port 3001 \
  --health http://localhost:3001/ \
  --build-cmd "npx next build" \
  --start-cmd "PORT=3001 npx next start"
```

### Node.js (no build step, just restart)

```bash
localhostmgr add \
  --name my-node-app \
  --cwd /path/to/my-node-app \
  --cmd "node server.js" \
  --port 3000 \
  --health http://localhost:3000/
```

### Node.js with build step

```bash
localhostmgr add \
  --name my-node-app \
  --cwd /path/to/my-node-app \
  --cmd "node server.js" \
  --port 3000 \
  --health http://localhost:3000/ \
  --build-cmd "npm run build"
```

### Python (Flask/FastAPI)

```bash
localhostmgr add \
  --name my-flask-app \
  --cwd /path/to/my-flask-app \
  --cmd "python server.py" \
  --port 3000 \
  --health http://localhost:3000/ \
  --env FLASK_ENV=production
```

### Go binary

```bash
localhostmgr add \
  --name my-go-app \
  --cwd /path/to/my-go-app \
  --cmd "/path/to/my-go-app/server" \
  --port 3000 \
  --health http://localhost:3000/health
```

---

## Updating a registration

Change commands, add build steps, update the port:

```bash
# Update build and start commands
localhostmgr update my-app --build-cmd "npm run build" --start-cmd "npm start"

# Update port
localhostmgr update my-app --port 4000

# Change the start command entirely
localhostmgr update my-app --cmd "PORT=4000 npm start"
```

---

## Rebuild without restart

Trigger a build-only step:

```bash
localhostmgr rebuild my-app
```

This runs `--build-cmd` in the app's `--cwd` without stopping or restarting the running process.

---

## Restart behavior

When `localhostmgr` detects a service is down (process dead or health check failing):

1. Runs `--build-cmd` if set (in `--cwd`)
2. Runs `--start-cmd` if set, otherwise `--cmd`
3. Polls health every 5 seconds
4. Increments fail counter on each failed attempt; resets on success

---

## Troubleshooting

**Service won't start — build fails (exit 127)**
The subprocess can't find `npm`/`node`. Use absolute paths via `npx` (e.g. `npx next dev` instead of `next dev`), or ensure the app's `node_modules/.bin/` is in PATH.

**Port already in use**
Another process is on that port. Choose a different port:
```bash
lsof -iTCP:3000 -sTCP:LISTEN
```

**Health check failing but app is running**
The health URL may be wrong. Test it directly:
```bash
curl http://localhost:3000/your-health-endpoint
```
Update with `localhostmgr update my-app --health http://localhost:3000/correct-path`

**App builds locally but fails under supervision**
Check the log for environment or path issues:
```bash
tail -f ~/.local/share/localhostmgr/logs/my-app.log
```

---

## Reference

| Command | Description |
|---|---|
| `localhostmgr list` | Show all registered services |
| `localhostmgr status <name>` | Detailed status of one service |
| `localhostmgr start <name>` | Start a stopped service |
| `localhostmgr stop <name>` | Stop a running service |
| `localhostmgr restart <name>` | Stop then start |
| `localhostmgr rebuild <name>` | Run build command only |
| `localhostmgr update <name> ...` | Update registration fields |
| `localhostmgr remove <name>` | Unregister a service |
| `localhostmgr doctor` | Run diagnostics |

Portal: `http://localhost:19999/`
