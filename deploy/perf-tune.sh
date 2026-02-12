#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ENV_FILE="${PROJECT_ROOT}/deploy/.env"
COMPOSE_FILE="${PROJECT_ROOT}/deploy/docker-compose.yml"

usage() {
  cat <<'EOF'
Usage:
  ./deploy/perf-tune.sh status
  ./deploy/perf-tune.sh apply <preset> [record_payload]
  ./deploy/perf-tune.sh logs [seconds]
  ./deploy/perf-tune.sh run
  ./deploy/perf-tune.sh matrix

Presets:
  baseline    Follow WINDOW_PROFILE only (clear QUIC_* overrides)
  baseline-smooth  baseline + TCP->WT coalescing knobs
  dl-a        Tuned for downlink test A (6/12/48/64 MB windows)
  dl-b        Tuned for downlink test B (8/16/64/96 MB windows)
  dl-c        Tuned for downlink test C (10/20/96/128 MB windows)

Examples:
  ./deploy/perf-tune.sh apply baseline 16384
  ./deploy/perf-tune.sh apply baseline-smooth 16384
  ./deploy/perf-tune.sh apply dl-a 16384
  ./deploy/perf-tune.sh logs 120
  ./deploy/perf-tune.sh run     # schedules background capture; survives SSH disconnect
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

apply_preset_env_only() {
  local preset="$1"
  local payload="$2"

  set_env "PERF_DIAG_ENABLE" "1"
  set_env "PERF_DIAG_INTERVAL_SEC" "10"
  set_env "RECORD_PAYLOAD_BYTES" "$payload"

  case "$preset" in
    baseline)
      set_or_clear_env "QUIC_INITIAL_STREAM_RECV_WINDOW" ""
      set_or_clear_env "QUIC_INITIAL_CONN_RECV_WINDOW" ""
      set_or_clear_env "QUIC_MAX_STREAM_RECV_WINDOW" ""
      set_or_clear_env "QUIC_MAX_CONN_RECV_WINDOW" ""
      set_env "TCP_TO_WT_ADAPTIVE" "1"
      set_or_clear_env "TCP_TO_WT_COALESCE_MS" ""
      set_or_clear_env "TCP_TO_WT_FLUSH_THRESHOLD" ""
      ;;
    baseline-smooth)
      set_or_clear_env "QUIC_INITIAL_STREAM_RECV_WINDOW" ""
      set_or_clear_env "QUIC_INITIAL_CONN_RECV_WINDOW" ""
      set_or_clear_env "QUIC_MAX_STREAM_RECV_WINDOW" ""
      set_or_clear_env "QUIC_MAX_CONN_RECV_WINDOW" ""
      set_env "TCP_TO_WT_ADAPTIVE" "1"
      # Keep baseline windows, add mild downlink smoothing knobs.
      set_env "TCP_TO_WT_COALESCE_MS" "8"
      set_env "TCP_TO_WT_FLUSH_THRESHOLD" "32768"
      ;;
    dl-a)
      set_env "QUIC_INITIAL_STREAM_RECV_WINDOW" "6291456"
      set_env "QUIC_INITIAL_CONN_RECV_WINDOW" "12582912"
      set_env "QUIC_MAX_STREAM_RECV_WINDOW" "50331648"
      set_env "QUIC_MAX_CONN_RECV_WINDOW" "67108864"
      set_env "TCP_TO_WT_ADAPTIVE" "1"
      set_or_clear_env "TCP_TO_WT_COALESCE_MS" ""
      set_or_clear_env "TCP_TO_WT_FLUSH_THRESHOLD" ""
      ;;
    dl-b)
      set_env "QUIC_INITIAL_STREAM_RECV_WINDOW" "8388608"
      set_env "QUIC_INITIAL_CONN_RECV_WINDOW" "16777216"
      set_env "QUIC_MAX_STREAM_RECV_WINDOW" "67108864"
      set_env "QUIC_MAX_CONN_RECV_WINDOW" "100663296"
      set_env "TCP_TO_WT_ADAPTIVE" "1"
      set_or_clear_env "TCP_TO_WT_COALESCE_MS" ""
      set_or_clear_env "TCP_TO_WT_FLUSH_THRESHOLD" ""
      ;;
    dl-c)
      set_env "QUIC_INITIAL_STREAM_RECV_WINDOW" "10485760"
      set_env "QUIC_INITIAL_CONN_RECV_WINDOW" "20971520"
      set_env "QUIC_MAX_STREAM_RECV_WINDOW" "100663296"
      set_env "QUIC_MAX_CONN_RECV_WINDOW" "134217728"
      set_env "TCP_TO_WT_ADAPTIVE" "1"
      set_or_clear_env "TCP_TO_WT_COALESCE_MS" ""
      set_or_clear_env "TCP_TO_WT_FLUSH_THRESHOLD" ""
      ;;
    *)
      echo "ERROR: unknown preset '$preset'"
      usage
      exit 1
      ;;
  esac
}

show_status() {
  ensure_env_file
  echo "=== PERF/QUIC current env ==="
  grep -E "^(WINDOW_PROFILE|RECORD_PAYLOAD_BYTES|PERF_DIAG_ENABLE|PERF_DIAG_INTERVAL_SEC|QUIC_INITIAL_STREAM_RECV_WINDOW|QUIC_INITIAL_CONN_RECV_WINDOW|QUIC_MAX_STREAM_RECV_WINDOW|QUIC_MAX_CONN_RECV_WINDOW|TCP_TO_WT_ADAPTIVE|TCP_TO_WT_COALESCE_MS|TCP_TO_WT_FLUSH_THRESHOLD)=" "$ENV_FILE" || true
}

apply_preset() {
  ensure_env_file
  local preset="$1"
  local payload="${2:-16384}"

  if [[ ! "$payload" =~ ^[0-9]+$ ]]; then
    echo "ERROR: record_payload must be an integer."
    exit 1
  fi

  apply_preset_env_only "$preset" "$payload"

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

  echo "Collecting [PERF]/[PERF-GW]/[PERF-GW2] logs for ${seconds}s..."
  timeout "${seconds}" docker compose -f "$COMPOSE_FILE" logs -f aether-gateway-core 2>&1 | grep -E --line-buffered "\[(PERF|PERF-GW|PERF-GW2)\]" || true
}

run_interactive_once() {
  ensure_env_file

  local payload seconds choice preset out_root run_ts out_dir start_delay
  run_ts="$(date +%Y%m%d-%H%M%S)"

  echo "Select test group:"
  echo "1) baseline"
  echo "2) baseline-smooth"
  echo "3) dl-a"
  echo "4) dl-b"
  echo "5) dl-c"
  read -rp "Choice [1-5, default 1]: " choice

  case "$choice" in
    ""|1) preset="baseline" ;;
    2) preset="baseline-smooth" ;;
    3) preset="dl-a" ;;
    4) preset="dl-b" ;;
    5) preset="dl-c" ;;
    *)
      echo "ERROR: invalid choice"
      exit 1
      ;;
  esac

  read -rp "RECORD_PAYLOAD_BYTES [default 16384]: " payload
  payload="${payload:-16384}"
  if [[ ! "$payload" =~ ^[0-9]+$ ]]; then
    echo "ERROR: record_payload must be an integer."
    exit 1
  fi

  read -rp "PERF log seconds [default 60]: " seconds
  seconds="${seconds:-60}"
  if [[ ! "$seconds" =~ ^[0-9]+$ ]]; then
    echo "ERROR: seconds must be an integer."
    exit 1
  fi

  read -rp "Start delay seconds after Enter [default 5]: " start_delay
  start_delay="${start_delay:-5}"
  if [[ ! "$start_delay" =~ ^[0-9]+$ ]]; then
    echo "ERROR: start delay must be an integer."
    exit 1
  fi

  read -rp "Save root dir [default \$HOME/perf-runs]: " out_root
  out_root="${out_root:-$HOME/perf-runs}"
  out_dir="${out_root}/${run_ts}-${preset}"
  mkdir -p "$out_dir"

  echo "Applying preset: $preset (payload=$payload)..."
  {
    echo "ts=$run_ts"
    echo "preset=$preset"
    echo "payload=$payload"
    echo "seconds=$seconds"
    echo "start_delay=$start_delay"
  } > "$out_dir/${preset}-meta.env"

  apply_preset_env_only "$preset" "$payload"
  restart_service > "$out_dir/${preset}-restart.log" 2>&1
  show_status > "$out_dir/${preset}-status.log" 2>&1

  local perf_file runner_file runner_log runner_pid_file summary_file since_ts
  perf_file="$out_dir/${preset}-perf.log"
  runner_file="$out_dir/${preset}-capture.sh"
  runner_log="$out_dir/${preset}-runner.log"
  runner_pid_file="$out_dir/${preset}-runner.pid"
  summary_file="$out_dir/${preset}-summary.log"
  since_ts="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

  echo "since_utc=$since_ts" >> "$out_dir/${preset}-meta.env"

  cat > "$runner_file" <<EOF
#!/usr/bin/env bash
set -euo pipefail
sleep ${start_delay}
timeout ${seconds} docker compose -f "${COMPOSE_FILE}" logs --since "${since_ts}" -f aether-gateway-core 2>&1 | grep -E --line-buffered "\\[(PERF|PERF-GW|PERF-GW2)\\]" > "${perf_file}" || true

{
  echo "preset=${preset}"
  echo "since_utc=${since_ts}"
  echo "window_sec=${seconds}"
  if [[ -s "${perf_file}" ]]; then
    awk '
      match(\$0, /down\{mbps=([0-9.]+).*read_us=([0-9.]+)/, m) {
        n_core++
        mbps = m[1] + 0
        r = m[2] + 0
        core_sum += mbps
        if (n_core == 1 || mbps < core_min) core_min = mbps
        if (mbps > core_max) core_max = mbps
        core_rsum += r
      }
      match(\$0, /dl\{mbps=([0-9.]+).*write_us=([0-9.]+)/, g) {
        n_gw++
        mbps = g[1] + 0
        w = g[2] + 0
        gw_sum += mbps
        if (mbps > 0.10) {
          gw_nz++
          gw_nzsum += mbps
          gw_streak++
          if (gw_streak > gw_max_streak) gw_max_streak = gw_streak
        } else {
          gw_streak = 0
        }
        if (n_gw == 1 || mbps < gw_min) gw_min = mbps
        if (mbps > gw_max) gw_max = mbps
        gw_wsum += w
      }
      match(\$0, /dl_stage\{read_wait_us=([0-9.]+).*reads=([0-9]+).*build_us=([0-9.]+).*builds=([0-9]+).*write_block_us=([0-9.]+).*writes=([0-9]+).*flush_avg_bytes=([0-9.]+).*flushes=([0-9]+)/, s) {
        n_gw2++
        gw2_read_wait_us_sum += (s[1] + 0)
        gw2_reads_sum += (s[2] + 0)
        gw2_build_us_sum += (s[3] + 0)
        gw2_builds_sum += (s[4] + 0)
        gw2_write_block_us_sum += (s[5] + 0)
        gw2_writes_sum += (s[6] + 0)
        gw2_flush_avg_bytes_sum += (s[7] + 0)
        gw2_flushes_sum += (s[8] + 0)
      }
      END {
        printf "points_total=%d\n", n_core + n_gw
        if (n_core > 0) {
          printf "core_down_points=%d\n", n_core
          printf "core_down_avg_mbps=%.3f\n", core_sum / n_core
          printf "core_down_max_mbps=%.3f\n", core_max
          printf "core_down_min_mbps=%.3f\n", core_min
          printf "core_down_avg_read_us=%.1f\n", core_rsum / n_core
        }
        if (n_gw > 0) {
          printf "gw_dl_points=%d\n", n_gw
          printf "gw_dl_avg_mbps=%.3f\n", gw_sum / n_gw
          printf "gw_dl_max_mbps=%.3f\n", gw_max
          printf "gw_dl_min_mbps=%.3f\n", gw_min
          printf "gw_dl_nonzero_points=%d\n", gw_nz
          if (gw_nz > 0) printf "gw_dl_nonzero_avg_mbps=%.3f\n", gw_nzsum / gw_nz
          printf "gw_dl_avg_write_us=%.1f\n", gw_wsum / n_gw
          printf "gw_dl_max_nonzero_streak=%d\n", gw_max_streak
          if (gw_max_streak >= 6) print "gw_dl_is_continuous6=yes"; else print "gw_dl_is_continuous6=no"
        }
        if (n_gw2 > 0) {
          printf "gw2_points=%d\n", n_gw2
          printf "gw2_avg_read_wait_us=%.1f\n", gw2_read_wait_us_sum / n_gw2
          printf "gw2_avg_reads=%.1f\n", gw2_reads_sum / n_gw2
          printf "gw2_avg_build_us=%.1f\n", gw2_build_us_sum / n_gw2
          printf "gw2_avg_builds=%.1f\n", gw2_builds_sum / n_gw2
          printf "gw2_avg_write_block_us=%.1f\n", gw2_write_block_us_sum / n_gw2
          printf "gw2_avg_writes=%.1f\n", gw2_writes_sum / n_gw2
          printf "gw2_avg_flush_bytes=%.1f\n", gw2_flush_avg_bytes_sum / n_gw2
          printf "gw2_avg_flushes=%.1f\n", gw2_flushes_sum / n_gw2
        }
        if (n_core == 0 && n_gw == 0) {
          print "points=0"
        }
      }
    ' "${perf_file}"
    echo "--- tail ---"
    tail -n 5 "${perf_file}" || true
  else
    echo "points=0"
    echo "note=perf file is empty; check runner log"
  fi
} > "${summary_file}" 2>&1
EOF
  chmod +x "$runner_file"

  echo
  echo "Ready. Switch to local client and prepare speed test."
  read -rp "Press Enter to schedule background PERF capture (delay=${start_delay}s, window=${seconds}s)..."

  nohup bash "$runner_file" > "$runner_log" 2>&1 < /dev/null &
  echo $! > "$runner_pid_file"

  echo "Saved:"
  echo "  $out_dir/${preset}-meta.env"
  echo "  $out_dir/${preset}-status.log"
  echo "  $runner_log"
  echo "  $runner_pid_file"
  echo "  $perf_file"
  echo "  $summary_file"
  echo
  echo "Capture has started in background and will continue even if SSH disconnects."
  echo "You can check progress with:"
  echo "  tail -f $runner_log"
  echo "After it finishes:"
  echo "  ls -lh $perf_file"
  echo "  cat $summary_file"
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
    run)
      run_interactive_once
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
