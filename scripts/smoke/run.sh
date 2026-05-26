#!/usr/bin/env bash
# Smoke test for argus: one happy-path deploy + one forced rollback.
# Runs entirely locally: real docker compose, fake GitHub, no PAT required.
# Expects bash, docker (v2 compose plugin), curl, jq, go, make on PATH.
set -euo pipefail

SMOKE_DIR=/tmp/argus-smoke
APP_DIR=$SMOKE_DIR/app
STATE=$SMOKE_DIR/state.json
CONFIG=$SMOKE_DIR/config.toml
LOG=$SMOKE_DIR/argus.log
MOCK_URL=http://127.0.0.1:17070
HEALTH_URL=http://127.0.0.1:8080/health
COMPOSE_PROJECT=argus-smoke

cleanup() {
  set +e
  [[ -n "${ARGUS_PID-}" ]] && kill -TERM "$ARGUS_PID" 2>/dev/null
  [[ -n "${MOCK_PID-}" ]] && kill -TERM "$MOCK_PID" 2>/dev/null
  # `docker compose -p NAME down` works off the project label, no compose file needed.
  docker compose -p "$COMPOSE_PROJECT" down --remove-orphans >/dev/null 2>&1
  rm -rf "$SMOKE_DIR"
  echo "==> cleanup done"
}
trap cleanup EXIT

# Reap stragglers from any earlier failed run before we start.
pkill -f "argus-smoke/mock-github" 2>/dev/null || true
docker compose -p "$COMPOSE_PROJECT" down --remove-orphans >/dev/null 2>&1 || true

REPO_ROOT=$(git rev-parse --show-toplevel)
cd "$REPO_ROOT"

mkdir -p "$APP_DIR"

echo "==> Pre-pulling test image..."
docker pull -q hashicorp/http-echo >/dev/null

echo "==> Building argus..."
make build >/dev/null

echo "==> Building mock GitHub..."
go build -o "$SMOKE_DIR/mock-github" ./scripts/smoke

echo "==> Starting mock GitHub on $MOCK_URL..."
"$SMOKE_DIR/mock-github" &
MOCK_PID=$!
for i in $(seq 1 10); do
  if curl -sf "$MOCK_URL/state" >/dev/null 2>&1; then break; fi
  sleep 0.3
done

cat > "$CONFIG" <<EOF
[agent]
id = "smoke-01"
poll_interval = "3s"

[[apps]]
name = "smoke"
apps_dir = "$APP_DIR"
health_port = 8080
health_path = "/health"
health_timeout = "20s"
health_interval = "1s"

  [apps.git]
  provider = "github"
  repo_url = "https://github.com/me/test"
  branch = "main"
  compose_path = "docker-compose.yml"
  token = "fake"
EOF

echo "==> Starting argus..."
./bin/argus -config "$CONFIG" -state "$STATE" -git-base-url "$MOCK_URL" > "$LOG" 2>&1 &
ARGUS_PID=$!

echo "==> Waiting for happy-path deploy..."
for i in $(seq 1 60); do
  if grep -q '"msg":"deploy finalized"' "$LOG" 2>/dev/null; then
    echo "    deploy finalized at i=$i"
    break
  fi
  sleep 1
done
if ! grep -q '"msg":"deploy finalized"' "$LOG" 2>/dev/null; then
  echo "FAIL: happy-path deploy did not complete"
  echo "------- argus log -------"
  cat "$LOG"
  exit 1
fi

if ! curl -sf "$HEALTH_URL" >/dev/null; then
  echo "FAIL: health endpoint not responding after happy path"
  exit 1
fi

GOOD_SHA=$(jq -r .smoke "$STATE")
echo "    state[smoke] = $GOOD_SHA"
echo "    health OK"

echo ""
echo "==> Forcing rollback: switching mock to 'bad' fixture..."
curl -sf "$MOCK_URL/switch/bad" >/dev/null

echo "==> Waiting for rollback alert..."
for i in $(seq 1 90); do
  if grep -q '"kind":"rollback"' "$LOG" 2>/dev/null; then
    echo "    rollback alert fired at i=$i"
    break
  fi
  sleep 1
done
if ! grep -q '"kind":"rollback"' "$LOG" 2>/dev/null; then
  echo "FAIL: rollback alert did not fire in 90s"
  echo "------- argus log (last 30 lines) -------"
  tail -30 "$LOG"
  exit 1
fi

AFTER_SHA=$(jq -r .smoke "$STATE")
if [[ "$AFTER_SHA" != "$GOOD_SHA" ]]; then
  echo "FAIL: state updated to $AFTER_SHA (expected $GOOD_SHA preserved)"
  exit 1
fi

# Simulate "operator fixed the upstream" so the agent stops cycling bad → rollback
# and we can observe a steady state.
echo "==> Switching mock back to good to simulate fixed upstream..."
curl -sf "$MOCK_URL/switch/good" >/dev/null

echo "==> Waiting for health to recover after restore..."
for i in $(seq 1 30); do
  if curl -sf --max-time 2 "$HEALTH_URL" >/dev/null 2>&1; then
    echo "    health recovered at i=$i"
    break
  fi
  sleep 1
done
if ! curl -sf --max-time 2 "$HEALTH_URL" >/dev/null 2>&1; then
  echo "FAIL: health not 200 within 30s after rollback restore"
  echo "------- argus log (last 30 lines) -------"
  tail -30 "$LOG"
  echo "------- docker ps -------"
  docker ps --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" 2>&1 || true
  exit 1
fi

echo ""
echo "============================================="
echo "  SMOKE PASSED"
echo "    happy path:  deployed sha=$GOOD_SHA"
echo "    rollback:    state preserved, health back"
echo "============================================="
