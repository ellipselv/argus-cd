# argus-cd

Lightweight single-binary GitOps agent for Docker Compose. Runs on each target
VM, polls GitHub for branch updates, deploys via `docker compose`, and rolls
back automatically when a new version fails its health check.

No central control plane, no inbound ports on the VM, no custom signing
infrastructure. Trust collapses to "do you trust GitHub + this PAT."

## What it does

Per-tick, per app: resolve the latest commit, dedupe against persisted state,
fetch the compose at that exact SHA, and run a four-phase transactional deploy
(**backup → write → bring up → health**). Only success persists the new SHA;
failures leave state untouched so the next tick aggressively retries.

## Quick start

### 1. Install

Build the `.deb` (requires Go ≥1.24 and [nfpm](https://nfpm.goreleaser.com/)):

```sh
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
make deb
```

…or grab a pre-built `.deb` from a release. Then on the target VM:

```sh
sudo dpkg -i argus-cd_0.1.0_amd64.deb
```

The package installs:

| Path | Purpose |
|---|---|
| `/usr/local/bin/arguscd` | the binary |
| `/lib/systemd/system/arguscd.service` | systemd unit |
| `/etc/argus/config.toml.template` | example config (copied to `config.toml` if absent) |
| `/etc/argus/config.toml` | your config (created by postinst, `chmod 600`) |
| `/opt/argus/apps/` | app working dirs + `argus-state.json` |

The postinst hook reloads systemd, enables, and starts the service — but it
will exit immediately the first time because the config is still all
placeholders. Edit it, then `systemctl restart arguscd`.

### 2. Configure

`/etc/argus/config.toml` looks like:

```toml
[agent]
id = "madrid-backend-node-01"
poll_interval = "30s"

[[apps]]
name = "lingua-backend"
apps_dir = "/opt/argus/apps/lingua-backend"
health_port = 8080
health_path = "/health"
health_timeout = "90s"
health_interval = "5s"
webhook_url = "https://alerts.example.com/argus"  # optional

  [apps.git]
  provider = "github"
  repo_url = "https://github.com/me/lingua-franca"
  branch = "main"
  compose_path = "docker-compose.yml"
  token = "github_pat_…"
  token_obtained_at = 2026-05-20T10:00:00Z
  token_expires_at  = 2026-08-20T10:00:00Z
```

Required PAT scopes: `contents:read`, `metadata:read`. If your images live in
private GHCR, docker on the VM additionally needs to be authenticated to pull
them — arguscd's PAT is for the GitHub API only, not the registry.

Local secrets (DB passwords, API keys, etc.) go in a `.env` file inside the
app's `apps_dir`. `docker compose up` picks it up automatically because arguscd
runs the command with that dir as the working directory.

### 3. Run

```sh
sudo systemctl restart arguscd
sudo journalctl -u arguscd -f
```

You should see structured JSON logs — one tick every `poll_interval`, with
`deploying` / `deploy finalized` lines when a new commit lands.

## How a deploy works

Per app, every `poll_interval`:

1. **Token check** — `slog.Warn` if `token_expires_at` is <7 days away,
   `slog.Error` if past. Doesn't block; lets GitHub's 401 surface the real
   failure.
2. **Latest SHA** — `GET /repos/<owner>/<repo>/commits/<branch>`.
3. **Dedupe** — if the persisted state already shows this SHA as deployed for
   this app, skip.
4. **Fetch compose** — `GET /repos/<owner>/<repo>/contents/<compose_path>?ref=<sha>`,
   base64-decoded into memory.
5. **Pipeline** in `apps_dir`:

   | Phase | What it does | On failure |
   |---|---|---|
   | Backup | `rename(docker-compose.yml → docker-compose.rollback.yml)` if present; stale rollback files are cleared first | Abort — host untouched |
   | Write | `os.WriteFile(docker-compose.yml, bytes, 0600)` | Restore rollback file, abort |
   | Bring up | `docker compose -p <app> up -d --remove-orphans` with cwd=`apps_dir` | `restore()` + notify `deploy_failure`, abort |
   | Health | Poll `http://127.0.0.1:<port><path>` every `health_interval` for up to `health_timeout` | `restore()` + notify `rollback`, abort |

6. **Commit point** — health returns 200 → delete `docker-compose.rollback.yml`.
   The new version is canonical and irrevocable from here.
7. **Persist** — atomic write of the new SHA into
   `/opt/argus/apps/argus-state.json`.

A failed deploy never updates state, so the next tick sees the same drift and
**retries automatically**. There is no failure-pollution mode where the agent
thinks something is deployed that isn't.

## Operational reference

**State file** — `/opt/argus/apps/argus-state.json`, a flat JSON map of
`app name → currently-deployed SHA`. Writes are atomic (`<file>.tmp` + rename).
Missing/empty → fresh state; corrupt → hard startup error so the operator can
intervene rather than silently lose history.

**Logs** — JSON via `slog` to stdout, captured by journald. Notable lines:

- `arguscd starting` — boot, with the app list and `poll_interval`
- `deploying app=… sha=…` — about to run the pipeline
- `deploy finalized app=… sha=…` — health succeeded, state persisted
- `image not ready yet` — GHCR HEAD returned 404, will retry next tick
- `arguscd alert kind=rollback|deploy_failure …` — paired with webhook POST if configured

**Webhook alerts** — POST JSON `{"kind":"…","application":"…","message":"…"}`
to `app.webhook_url`. Two kinds:

- `deploy_failure` — `docker compose up` errored
- `rollback` — health check failed, previous version restored

Delivery is best-effort (10s timeout, 5xx is logged but not retried). Empty
URL → alerts only land in logs.

**Failure modes**:

| What fails | What happens |
|---|---|
| GitHub API down / rate-limited | Tick errors, retries next tick |
| Token expired | 401 from GitHub surfaces as tick error |
| Compose path missing in repo | 404 from contents API, no deploy |
| Image not pulled yet (GH Actions still building) | `docker compose up` fails, `deploy_failure` alert + restore |
| Health probe times out | `rollback` alert, previous version restored |
| Argus crashes mid-deploy | Containers keep running; on boot, state still shows previous SHA, next tick re-runs |
| State file corruption | Argus refuses to start — operator removes the file to redeploy everything |

## Build from source

```sh
make build   # CGO=0 GOOS=linux GOARCH=amd64 → bin/arguscd
make test    # go test -race ./...
make deb     # → dist/argus-cd_<VERSION>_amd64.deb (needs nfpm on PATH)
make clean
```

Project layout:

```
cmd/arguscd/main.go            # flag parsing, slog setup, signal-cancellable Run
internal/agent/
  config.go    TOML loader (BurntSushi/toml) + custom Duration type + validation
  git.go       GitHub commits & contents API, repo URL parser, token expiry check
  state.go     Thread-safe atomic JSON state file (DefaultStatePath)
  health.go    HTTP polling with deadline + per-attempt timeout
  notify.go    Webhook POST with 10s timeout, URL per call
  deploy.go    Four-phase pipeline with injectable composeUp for tests
  runner.go    Ticker loop + per-app orchestration
scripts/
  arguscd.service     systemd unit
  postinstall.sh      deb postinst: dirs, perms, daemon-reload, enable, start
  prerm.sh            deb prerm: stop + disable
configs/
  config.example.toml shipped as /etc/argus/config.toml.template
nfpm.yaml             NFPM packaging manifest
Makefile              build / test / deb / clean
```

Testability is structural: `Deployer.composeUp` is an injectable function
field, `Git.baseURL` is overridable, `LoadState(path)` takes the path as a
parameter. Tests exercise these without touching docker, the real GitHub, or
`/opt/argus/`.

## What's not built yet

- **GitLab provider** — the switch in `git.go` returns "not yet implemented".
- **Metrics endpoint** — observability is log-only via `slog`.
- **CI** — `go test -race` and `make deb` aren't yet enforced on PRs.
- **Release automation** — `make deb` produces an artifact; uploading to
  GitHub Releases is manual.

## License

Apache-2.0. See [LICENSE](./LICENSE).
