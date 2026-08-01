package server

import (
	"html/template"
	"net/http"
	"time"
)
// Render writes the portal HTML page.
func Render(w http.ResponseWriter, info DaemonInfo, svcs []svcJSON) {
	page := pageData{
		GeneratedAt: time.Now().Format("15:04:05"),
		Daemon:     info,
		Services:   svcs,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, page); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

type pageData struct {
	GeneratedAt string
	Daemon      DaemonInfo
	Services    []svcJSON
}

var tmpl = template.Must(template.New("portal").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>localhostmgr</title>
  <link rel="icon" href="/favicon.svg"/>
  <style>
    :root {
      --bg: #0f1117;
      --surface: #1a1d27;
      --surface2: #242836;
      --border: #2e3347;
      --text: #e2e4ea;
      --muted: #7a7f94;
      --green: #22c55e;
      --red: #ef4444;
      --yellow: #eab308;
      --blue: #3b82f6;
      --font: system-ui, -apple-system, sans-serif;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { background: var(--bg); color: var(--text); font-family: var(--font); font-size: 14px; line-height: 1.5; min-height: 100vh; }

    header { background: var(--surface); border-bottom: 1px solid var(--border); padding: 16px 24px; display: flex; align-items: center; justify-content: space-between; position: sticky; top: 0; z-index: 10; }
    .logo { display: flex; align-items: center; gap: 10px; font-size: 16px; font-weight: 700; letter-spacing: -0.3px; }
    .logo svg { width: 24px; height: 24px; }
    .header-right { display: flex; align-items: center; gap: 12px; }
    .refresh-info { color: var(--muted); font-size: 12px; }
    .btn { display: inline-flex; align-items: center; gap: 6px; padding: 6px 14px; border-radius: 6px; border: 1px solid var(--border); background: var(--surface2); color: var(--text); font-size: 13px; font-family: var(--font); cursor: pointer; transition: all 0.15s; text-decoration: none; }
    .btn:hover { background: var(--border); }
    .btn-sm { padding: 4px 10px; font-size: 12px; }
    .btn-green { background: #14532d; border-color: var(--green); color: var(--green); }
    .btn-green:hover { background: #166534; }
    .btn-red { background: #450a0a; border-color: var(--red); color: var(--red); }
    .btn-red:hover { background: #7f1d1d; }
    .btn-yellow { background: #422006; border-color: var(--yellow); color: var(--yellow); }
    .btn-yellow:hover { background: #713f12; }

    main { max-width: 1100px; margin: 0 auto; padding: 24px; }

    .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 12px; margin-bottom: 24px; }
    .stat { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; padding: 14px 16px; }
    .stat-label { color: var(--muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 4px; }
    .stat-value { font-size: 22px; font-weight: 700; }
    .stat-value.green { color: var(--green); }
    .stat-value.red { color: var(--red); }
    .stat-value.yellow { color: var(--yellow); }
    .stat-value.muted { color: var(--muted); }

    .services-table { width: 100%; border-collapse: collapse; background: var(--surface); border: 1px solid var(--border); border-radius: 10px; overflow: hidden; }
    .services-table th { background: var(--surface2); color: var(--muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em; text-align: left; padding: 10px 14px; border-bottom: 1px solid var(--border); }
    .services-table td { padding: 12px 14px; border-bottom: 1px solid var(--border); vertical-align: middle; }
    .services-table tr:last-child td { border-bottom: none; }
    .services-table tr:hover td { background: rgba(255,255,255,0.02); }

    .state-badge { display: inline-flex; align-items: center; gap: 5px; padding: 3px 9px; border-radius: 20px; font-size: 12px; font-weight: 600; }
    .state-running { background: #14532d; color: var(--green); }
    .state-stopped { background: #1f1f2e; color: var(--muted); }
    .state-disabled { background: #1f1f2e; color: var(--muted); border: 1px dashed var(--border); }

    .name { font-weight: 600; font-size: 15px; }
    .cmd { font-size: 12px; color: var(--muted); font-family: ui-monospace, monospace; max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .cwd { font-size: 11px; color: var(--muted); font-family: ui-monospace, monospace; max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .pid { font-family: ui-monospace, monospace; color: var(--blue); font-size: 13px; }
    .port { font-family: ui-monospace, monospace; color: var(--muted); }
    .health { font-size: 11px; color: var(--blue); font-family: ui-monospace, monospace; max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .fail-badge { background: #450a0a; color: var(--red); border-radius: 4px; padding: 1px 6px; font-size: 11px; font-weight: 700; }
    .last-ok { font-size: 11px; color: var(--muted); }
    .last-error { font-size: 11px; color: var(--red); max-width: 160px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

    .actions { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }

    .log-section { margin-top: 24px; }
    .log-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
    .log-title { font-size: 13px; font-weight: 600; color: var(--muted); text-transform: uppercase; letter-spacing: 0.05em; }
    .log-box { background: #0a0a0f; border: 1px solid var(--border); border-radius: 8px; padding: 14px; font-family: ui-monospace, monospace; font-size: 12px; line-height: 1.6; color: #a1a1aa; max-height: 300px; overflow-y: auto; white-space: pre-wrap; word-break: break-all; }
    .log-box::-webkit-scrollbar { width: 6px; }
    .log-box::-webkit-scrollbar-track { background: transparent; }
    .log-box::-webkit-scrollbar-thumb { background: var(--border); border-radius: 3px; }

    .empty { text-align: center; padding: 60px 20px; color: var(--muted); }
    .empty-icon { font-size: 40px; margin-bottom: 12px; opacity: 0.4; }
    .empty-title { font-size: 16px; font-weight: 600; margin-bottom: 6px; color: var(--text); }
    .empty-sub { font-size: 13px; }

    .toast { position: fixed; bottom: 24px; right: 24px; background: var(--surface2); border: 1px solid var(--border); border-radius: 8px; padding: 10px 16px; font-size: 13px; z-index: 100; opacity: 0; transition: opacity 0.2s; pointer-events: none; }
    .toast.show { opacity: 1; }
    .toast.error { border-color: var(--red); color: var(--red); }
    .toast.success { border-color: var(--green); color: var(--green); }

    .daemon-bar { background: var(--surface2); border-bottom: 1px solid var(--border); padding: 10px 24px; display: flex; align-items: center; gap: 24px; flex-wrap: wrap; font-size: 12px; }
    .daemon-bar .d-item { display: flex; align-items: center; gap: 6px; }
    .daemon-bar .d-label { color: var(--muted); text-transform: uppercase; letter-spacing: 0.05em; font-size: 10px; }
    .daemon-bar .d-value { font-family: ui-monospace, monospace; color: var(--blue); font-weight: 600; }
    .daemon-bar .d-value.green { color: var(--green); }
    .daemon-bar .d-value.muted { color: var(--muted); }
    .daemon-bar .d-divider { width: 1px; height: 16px; background: var(--border); }
  </style>
</head>
<body>
<header>
  <div class="logo">
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
      <rect width="32" height="32" rx="6" fill="#1a1a2e"/>
      <circle cx="16" cy="16" r="8" fill="none" stroke="#00d4ff" stroke-width="2"/>
      <circle cx="16" cy="16" r="3" fill="#00d4ff"/>
    </svg>
    localhostmgr
  </div>
  <div class="header-right">
    <span class="refresh-info">page: {{.GeneratedAt}}</span>
    <button class="btn" onclick="location.reload()">Refresh</button>
  </div>
</header>

<div class="daemon-bar">
  <div class="d-item">
    <span class="d-label">PID</span>
    <span class="d-value">{{.Daemon.PID}}</span>
  </div>
  <div class="d-divider"></div>
  <div class="d-item">
    <span class="d-label">started</span>
    <span class="d-value">{{.Daemon.StartedAt}}</span>
  </div>
  <div class="d-divider"></div>
  <div class="d-item">
    <span class="d-label">tick</span>
    <span class="d-value muted">5s</span>
  </div>
  <div class="d-divider"></div>
  <div class="d-item">
    <span class="d-label">portal</span>
    <span class="d-value">localhost:{{.Daemon.PortalPort}}</span>
  </div>
  <div class="d-divider"></div>
  <div class="d-item">
    <span class="d-label">go</span>
    <span class="d-value muted">{{.Daemon.Version}}</span>
  </div>
</div>

<main>
{{if .Services}}
  <div class="summary">
    {{range $svc := .Services}}
    <div class="stat">
      <div class="stat-label">{{$svc.Name}}</div>
      <div class="stat-value {{if eq $svc.State "running"}}green{{else if eq $svc.State "stopped"}}red{{else}}muted{{end}}">
        {{$svc.State}}
      </div>
    </div>
    {{end}}
  </div>
{{end}}

{{if .Services}}
  <table class="services-table">
    <thead>
      <tr>
        <th>Service</th>
        <th>State</th>
        <th>PID</th>
        <th>Port</th>
        <th>Health</th>
        <th>Uptime</th>
        <th>Errors</th>
        <th>Actions</th>
      </tr>
    </thead>
    <tbody>
      {{range .Services}}
      <tr data-name="{{.Name}}">
        <td>
          <div class="name">{{.Name}}</div>
          <div class="cwd" title="{{.Cwd}}">{{.Cwd}}</div>
          <div class="cmd" title="{{.Cmd}}">{{.Cmd}}</div>
        </td>
        <td>
          {{if eq .State "running"}}<span class="state-badge state-running">&#9679; running</span>
          {{else if eq .State "stopped"}}<span class="state-badge state-stopped">&#9675; stopped</span>
          {{else}}<span class="state-badge state-disabled">&#9744; disabled</span>{{end}}
        </td>
        <td>
          {{if .PID}}<span class="pid">{{.PID}}</span>{{else}}<span class="muted">&#8212;</span>{{end}}
        </td>
        <td>
          {{if .Port}}<span class="port">{{.Port}}</span>{{else}}<span class="muted">&#8212;</span>{{end}}
        </td>
        <td>
          {{if .HealthURL}}<a class="health" href="{{.HealthURL}}" target="_blank" rel="noopener">{{.HealthURL}}</a>{{else}}<span class="muted">&#8212;</span>{{end}}
        </td>
        <td>
          {{if .LastHealthAt}}<span class="last-ok">{{.LastHealthAt}}</span>{{else}}<span class="muted">&#8212;</span>{{end}}
        </td>
        <td>
          {{if .FailCount}}<span class="fail-badge">{{.FailCount}} fail{{if gt .FailCount 1}}s{{end}}</span>{{end}}
          {{if .LastError}}<div class="last-error" title="{{.LastError}}">{{.LastError}}</div>{{end}}
        </td>
        <td>
          <div class="actions">
            {{if eq .State "running"}}
              <button class="btn btn-sm btn-red" onclick="doAction('stop','{{.Name}}',this)">Stop</button>
              <button class="btn btn-sm" onclick="doAction('restart','{{.Name}}',this)">Restart</button>
            {{else}}
              <button class="btn btn-sm btn-green" onclick="doAction('start','{{.Name}}',this)">Start</button>
            {{end}}
            {{if .Enabled}}
              <button class="btn btn-sm btn-yellow" onclick="doAction('disable','{{.Name}}',this)">Disable</button>
            {{else}}
              <button class="btn btn-sm" onclick="doAction('enable','{{.Name}}',this)">Enable</button>
            {{end}}
            <button class="btn btn-sm" onclick="showLog('{{.Name}}')">Log</button>
            <button class="btn btn-sm btn-red" onclick="doAction('remove','{{.Name}}',this)">Remove</button>
          </div>
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
{{else}}
  <div class="empty">
    <div class="empty-icon">&#9632;</div>
    <div class="empty-title">No services registered</div>
    <div class="empty-sub">Run <code>localhostmgr add --name my-app --cwd /path --cmd "node server.js" --port 3000</code> to register one.</div>
  </div>
{{end}}

<div class="log-section" id="logSection" style="display:none">
  <div class="log-header">
    <div class="log-title" id="logTitle">Log</div>
    <button class="btn btn-sm" onclick="closeLog()">Close</button>
  </div>
  <pre class="log-box" id="logBox"></pre>
</div>
</main>

<div class="toast" id="toast"></div>

<script>
const BASE = '/api';

async function doAction(action, name, btn) {
  btn.disabled = true;
  let method, path;
  switch (action) {
  case 'start':    method = 'POST'; path = '/api/services/' + name + '/start'; break;
  case 'stop':     method = 'POST'; path = '/api/services/' + name + '/stop'; break;
  case 'restart':  method = 'POST'; path = '/api/services/' + name + '/restart'; break;
  case 'enable':   method = 'POST'; path = '/api/services/' + name + '/enable'; break;
  case 'disable':  method = 'POST'; path = '/api/services/' + name + '/disable'; break;
  case 'remove':   method = 'DELETE'; path = '/api/services/' + name; break;
  }
  try {
    const r = await fetch(path, { method });
    const j = await r.json();
    if (j.error) { toast(j.error, 'error'); }
    else { toast(j.status || action, 'success'); setTimeout(() => location.reload(), 600); }
  } catch(e) { toast(e.message, 'error'); }
  btn.disabled = false;
}

async function showLog(name) {
  const section = document.getElementById('logSection');
  const box = document.getElementById('logBox');
  const title = document.getElementById('logTitle');
  section.style.display = 'block';
  title.textContent = name + ' log';
  box.textContent = 'loading...';
  try {
    const base = '/api';
    const r = await fetch(base + '/logs/' + name);
    box.textContent = await r.text() || '(empty)';
  } catch(e) { box.textContent = e.message; }
  section.scrollIntoView({ behavior: 'smooth' });
}

function closeLog() {
  document.getElementById('logSection').style.display = 'none';
}

let toastTimer;
function toast(msg, cls) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.className = 'toast show ' + (cls || '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.className = 'toast', 2500);
}
</script>
</body>
</html>
`))
