#!/usr/bin/env bash

set -euo pipefail

ENV_FILE="deploy/.env"
COMPOSE_FILE="deploy/docker-compose.yml"

usage() {
  cat <<'EOF'
Usage:
  ./deploy/perf-tune.sh status
  ./deploy/perf-tune.sh apply <preset> [record_payload]
  ./deploy/perf-tune.sh logs [seconds]
  ./deploy/perf-tune.sh matrix

Presets:
  baseline    Follow WINDOW_PROFILE only (clear QUIC_* overrides)
  dl-a        Tuned for downlink test A (6/12/48/64 MB windows)
  dl-b        Tuned for downlink test B (8/16/64/96 MB windows)
  dl-c        Tuned for downlink test C (10/20/96/128 MB windows)

Examples:
  ./deploy/perf-tune.sh apply baseline 16384
  ./deploy/perf-tune.sh apply dl-a 16384
  ./deploy/perf-tune.sh logs 120
EOF
}

ensure_env_file() {
  if [[ ! -f "$ENV_FILE" ]]; then
    echo "ERROR: $ENV_FILE not found. Run ./deploy.sh install first."
    exit 1
  fi
}

set_env() {
  local key="$1"
  local value="$2"
  sed -i "/^${key}=/d" "$ENV_FILE"
  echo "${key}=${value}" >> "$ENV_FILE"
}

set_or_clear_env() {
  local key="$1"
  local value="${2:-}"
  sed -i "/^${key}=/d" "$ENV_FILE"
  if [[ -n "$value" ]]; then
    echo "${key}=${value}" >> "$ENV_FILE"
  else
    echo "${key}=" >> "$ENV_FILE"
  fi
}

restart_service() {
  docker compose -f "$COMPOSE_FILE" up -d --remove-orphans
}

show_status() {
  ensure_env_file
  echo "=== PERF/QUIC current env ==="
  grep -E "^(WINDOW_PROFILE|RECORD_PAYLOAD_BYTES|PERF_DIAG_ENABLE|PERF_DIAG_INTERVAL_SEC|QUIC_INITIAL_STREAM_RECV_WINDOW|QUIC_INITIAL_CONN_RECV_WINDOW|QUIC_MAX_STREAM_RECV_WINDOW|QUIC_MAX_CONN_RECV_WINDOW)=" "$ENV_FILE" || true
}

apply_preset() {
  ensure_env_file
  local preset="$1"
  local payload="${2:-16384}"

  if [[ ! "$payload" =~ ^[0-9]+$ ]]; then
    echo "ERROR: record_payload must be an integer."
    exit 1
  fi

  set_env "PERF_DIAG_ENABLE" "1"
  set_env "PERF_DIAG_INTERVAL_SEC" "10"
  set_env "RECORD_PAYLOAD_BYTES" "$payload"

  case "$preset" in
    baseline)
      set_or_clear_env "QUIC_INITIAL_STREAM_RECV_WINDOW" ""
      set_or_clear_env "QUIC_INITIAL_CONN_RECV_WINDOW" ""
      set_or_clear_env "QUIC_MAX_STREAM_RECV_WINDOW" ""
      set_or_clear_env "QUIC_MAX_CONN_RECV_WINDOW" ""
      ;;
    dl-a)
      set_env "QUIC_INITIAL_STREAM_RECV_WINDOW" "6291456"
      set_env "QUIC_INITIAL_CONN_RECV_WINDOW" "12582912"
      set_env "QUIC_MAX_STREAM_RECV_WINDOW" "50331648"
      set_env "QUIC_MAX_CONN_RECV_WINDOW" "67108864"
      ;;
    dl-b)
      set_env "QUIC_INITIAL_STREAM_RECV_WINDOW" "8388608"
      set_env "QUIC_INITIAL_CONN_RECV_WINDOW" "16777216"
      set_env "QUIC_MAX_STREAM_RECV_WINDOW" "67108864"
      set_env "QUIC_MAX_CONN_RECV_WINDOW" "100663296"
      ;;
    dl-c)
      set_env "QUIC_INITIAL_STREAM_RECV_WINDOW" "10485760"
      set_env "QUIC_INITIAL_CONN_RECV_WINDOW" "20971520"
      set_env "QUIC_MAX_STREAM_RECV_WINDOW" "100663296"
      set_env "QUIC_MAX_CONN_RECV_WINDOW" "134217728"
      ;;
    *)
      echo "ERROR: unknown preset '$preset'"
      usage
      exit 1
      ;;
  esac

  echo "Applied preset: $preset, RECORD_PAYLOAD_BYTES=$payload"
  restart_service
  show_status
  echo
  echo "Next:"
  echo "1) Run your speed test (same route, 3 rounds)."
  echo "2) Capture PERF logs: ./deploy/perf-tune.sh logs 120"
}

show_logs() {
  local seconds="${1:-60}"
  if [[ ! "$seconds" =~ ^[0-9]+$ ]]; then
    echo "ERROR: seconds must be an integer."
    exit 1
  fi

  echo "Collecting [PERF] logs for ${seconds}s..."
  timeout "${seconds}" docker compose -f "$COMPOSE_FILE" logs -f aether-gateway-core 2>&1 | grep --line-buffered "\[PERF\]" || true
}

matrix_plan() {
  cat <<'EOF'
Recommended matrix order:
1) ./deploy/perf-tune.sh apply baseline 16384
2) ./deploy/perf-tune.sh apply dl-a 16384
3) ./deploy/perf-tune.sh apply dl-b 16384
4) ./deploy/perf-tune.sh apply dl-c 16384

For each step:
1) Run 3 rounds of speed test
2) Run: ./deploy/perf-tune.sh logs 120
3) Record: down/up Mbps + [PERF] down.read_us, down.parse_us
EOF
}

main() {
  local cmd="${1:-}"
  case "$cmd" in
    status)
      show_status
      ;;
    apply)
      if [[ $# -lt 2 ]]; then
        usage
        exit 1
      fi
      apply_preset "$2" "${3:-16384}"
      ;;
    logs)
      show_logs "${2:-60}"
      ;;
    matrix)
      matrix_plan
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"

