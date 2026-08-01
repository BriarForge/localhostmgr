# localhostmgr

Localhost process supervisor — keeps your `node server.js` services alive 24x7.

## Git identity and wrappers (mandatory)

All git activity in this repo MUST go through a per-person wrapper. No bare `git push`.

| Agent | Wrapper |
|---|---|
| Aoife | `git-aoife` |
| Declan | `git-declan` |
| Milena | `git-milena` |
| Sofia | `git-sofia` |

Whoever pushes uses their own wrapper. Wrappers set committer identity and route the push to the correct per-person remote on the matching `github-<person>` SSH host.

Run `git-<person> whoami` to confirm before pushing.

## Secrets policy (mandatory)

**Never commit secrets to this repo.** API keys, tokens, passwords, private keys, connection strings with credentials, and any other sensitive values must not appear in any committed file — source code, config, scripts, documentation, or otherwise.

If a secret is accidentally committed: treat it as fully compromised, rotate it immediately, and scrub it from history with `git filter-repo` or similar before pushing. When in doubt, don't commit it.

## Source of truth

Inherited from `/Users/mike/Projects/BriarForge/AGENTS.md`. When this file and the parent conflict, the parent wins until this file is updated to match.
