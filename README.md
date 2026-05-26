# Argus CD - Lightweight single-binary GitOps agent

Lightweight single-binary GitOps agent for Docker Compose. Runs on each target
VM, polls GitHub for branch updates, deploys via `docker compose`, and rolls
back automatically when a new version fails its health check.

## Install

```sh
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
make deb
sudo dpkg -i dist/argus-cd_0.1.0_amd64.deb
```

## Configure

Edit `/etc/argus/config.toml`:

```toml
[agent]
id = "node-01"
poll_interval = "30s"

[[apps]]
name = "backend"
apps_dir = "/opt/argus/apps/backend"
health_port = 8080
health_path = "/health"
health_timeout = "90s"
health_interval = "5s"
webhook_url = "https://alerts.example.com/argus"  # optional

  [apps.git]
  provider = "github"
  repo_url = "https://github.com/me/backend"
  branch = "main"
  compose_path = "docker-compose.yml"
  token = "github_pat_…"
  token_obtained_at = 2026-05-20T10:00:00Z
  token_expires_at  = 2026-08-20T10:00:00Z
```

PAT scopes: `contents:read`, `metadata:read`. Local secrets go in a `.env`
file inside `apps_dir`.

## Run

```sh
sudo systemctl restart argus
sudo journalctl -u argus -f
```

## Build from source

```sh
make build   # → bin/argus
make test    # go test -race ./...
make deb     # → dist/argus-cd_<VERSION>_amd64.deb
```

## License

Apache-2.0. See [LICENSE](./LICENSE).
