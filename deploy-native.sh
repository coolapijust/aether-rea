#!/bin/bash

# Aether-Realist Native (non-Docker) one-click deploy script
# Runtime: systemd + local binary

# -E: inherit ERR trap in functions/command substitutions
set -Eeuo pipefail

# Print the failing command and line number. This makes "silent exits" diagnosable over SSH.
trap 'rc=$?; echo "ERROR: rc=${rc} line ${LINENO}: ${BASH_COMMAND}" >&2' ERR

# Colors must be defined before any function uses them (set -u would otherwise abort).
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() {
    # Under `set -u`, always tolerate missing color vars.
    echo -e "${YELLOW:-}[native]${NC:-} $*"
}

SCRIPT_VERSION="2026-02-13-c4ddfdfe"
log "script_version=${SCRIPT_VERSION}"

# When this script is executed via `curl | bash`, stdin is a pipe so `read -p` sees EOF.
# Always prefer reading from /dev/tty when available so one-liner installs remain interactive.
read_tty() {
    # Usage: read_tty <var_name> <prompt> [default]
    local __var="$1"
    local __prompt="$2"
    local __default="${3:-}"
    local __in=""

    if [ -r /dev/tty ]; then
        # Don't let `read` failure abort the script under `set -e`.
        read -r -p "$__prompt" __in </dev/tty || true
    else
        read -r -p "$__prompt" __in || true
    fi

    if [ -z "$__in" ]; then
        __in="$__default"
    fi
    printf -v "$__var" "%s" "$__in"
}

read_tty_yn() {
    # Usage: read_tty_yn <var_name> <prompt> <default_y_or_n>
    local __var="$1"
    local __prompt="$2"
    local __default="${3:-n}"
    local __in=""

    read_tty __in "$__prompt" "$__default"
    case "$__in" in
        y|Y) printf -v "$__var" "%s" "y" ;;
        n|N) printf -v "$__var" "%s" "n" ;;
        *)   printf -v "$__var" "%s" "$__default" ;;
    esac
}

echo -e "${GREEN}==============================================${NC}"
echo -e "${GREEN}   Aether-Realist Native 一键部署工具         ${NC}"
echo -e "${GREEN}==============================================${NC}"

DEPLOY_REF="${DEPLOY_REF:-main}"
GITHUB_RAW_BASE="https://raw.githubusercontent.com/coolapijust/Aether-Realist/${DEPLOY_REF}"
GITHUB_REPO="https://github.com/coolapijust/Aether-Realist.git"
GITHUB_API_REPO="https://api.github.com/repos/coolapijust/Aether-Realist"
SERVICE_NAME="aether-gateway"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
BIN_PATH="/usr/local/bin/aether-gateway"

# Native install layout (works from any current directory).
# AETHER_HOME is the persistent state directory on the server.
AETHER_HOME="${AETHER_HOME:-/opt/aether-realist}"
SRC_DIR="${AETHER_HOME}/src"
ENV_FILE="${AETHER_HOME}/deploy/.env"

# Release download behavior
# - Default: pin to a specific tag (no redirect, deterministic).
# - Optional: set AETHER_RELEASE_LATEST=1 to use GitHub "latest" redirect URL.
# - Optional: set AETHER_RELEASE_TAG=vX.Y.Z to pin an exact release.
# - Optional: set AETHER_RELEASE_URL to force an exact binary URL (skips arch/tag).
# - Optional: set VERIFY_SHA256=1 to verify downloaded binary (downloads sha text but does not persist it).
VERIFY_SHA256="${VERIFY_SHA256:-0}"
AETHER_RELEASE_LATEST="${AETHER_RELEASE_LATEST:-0}"
AETHER_RELEASE_URL="${AETHER_RELEASE_URL:-}"
AETHER_RELEASE_SHA256_URL="${AETHER_RELEASE_SHA256_URL:-}"
DEFAULT_RELEASE_TAG="v5.2.2"

# Optional ACME (Let's Encrypt) integration via acme.sh.
# Enable with: ACME_ENABLE=1
ACME_ENABLE="${ACME_ENABLE:-0}"
ACME_EMAIL="${ACME_EMAIL:-}"
# Modes:
# - standalone: use HTTP-01 on TCP/80 (no downtime; recommended)
# - alpn-stop: stop gateway briefly and use TLS-ALPN-01 on TCP/443 (downtime)
ACME_MODE="${ACME_MODE:-standalone}"
ACME_CA="${ACME_CA:-letsencrypt}"
ACME_KEYLENGTH="${ACME_KEYLENGTH:-ec-256}"
ACME_HOME_DIR="${ACME_HOME_DIR:-${AETHER_HOME}/acme-home}"

is_root() {
    [ "$(id -u)" -eq 0 ]
}

run_root() {
    if is_root; then
        "$@"
    else
        sudo "$@"
    fi
}

require_cmd() {
    local c="$1"
    if ! command -v "$c" >/dev/null 2>&1; then
        echo -e "${RED}错误: 未检测到依赖命令: ${c}${NC}"
        return 1
    fi
    return 0
}

env_get() {
    # Read KEY=VALUE from ENV_FILE and normalize:
    # - keep empty if missing
    # - trim whitespace
    # - strip a single pair of surrounding quotes ('' or "")
    local key="$1"
    local file="$2"
    local v=""

    if [ ! -f "$file" ]; then
        printf "%s" ""
        return 0
    fi

    v="$(grep -m1 "^${key}=" "$file" 2>/dev/null | cut -d'=' -f2- || true)"
    v="$(printf "%s" "$v" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    # Strip one leading/trailing quote if present.
    v="$(printf "%s" "$v" | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")"
    printf "%s" "$v"
}

validate_record_payload_size() {
    local value="$1"
    if [[ ! "$value" =~ ^[0-9]+$ ]]; then
        return 1
    fi
    if [ "$value" -lt 1024 ] || [ "$value" -gt 262144 ]; then
        return 1
    fi
    return 0
}

port_in_use() {
    local port="$1"
    if command -v ss >/dev/null 2>&1; then
        ss -ltn | awk '{print $4}' | grep -qE "(:|\\[::\\])${port}$"
        return $?
    fi
    if command -v netstat >/dev/null 2>&1; then
        netstat -ltn 2>/dev/null | awk '{print $4}' | grep -qE "(:|\\[::\\])${port}$"
        return $?
    fi
    # Unknown: assume in use to avoid surprising failures.
    return 0
}

detect_arch() {
    local m
    m="$(uname -m)"
    case "$m" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) return 1 ;;
    esac
}

try_install_from_release() {
    # Prefer release binary download so native deploy does not require Go.
    # Priority:
    #  1) AETHER_RELEASE_URL (exact URL)
    #  2) AETHER_RELEASE_LATEST=1 (GitHub latest redirect)
    #  3) AETHER_RELEASE_TAG (pinned tag)
    #  4) DEFAULT_RELEASE_TAG (script default)
    local tag arch url out sha_url

    arch="$(detect_arch)" || return 1
    tag="${AETHER_RELEASE_TAG:-}"
    tag="$(printf %s "$tag" | tr -d '\r\n\t ')"

    out="${AETHER_HOME}/bin/aether-gateway"
    run_root mkdir -p "$(dirname "$out")"

    if [ -n "$AETHER_RELEASE_URL" ]; then
        url="$AETHER_RELEASE_URL"
        sha_url="${AETHER_RELEASE_SHA256_URL:-${url}.sha256}"
    elif [ "$AETHER_RELEASE_LATEST" = "1" ]; then
        url="https://github.com/coolapijust/Aether-Realist/releases/latest/download/aether-gateway-linux-${arch}"
        sha_url="${url}.sha256"
    else
        [ -z "$tag" ] && tag="$DEFAULT_RELEASE_TAG"
        url="https://github.com/coolapijust/Aether-Realist/releases/download/${tag}/aether-gateway-linux-${arch}"
        sha_url="${url}.sha256"
    fi
    url="$(printf %s "$url" | tr -d '\r\n')"
    sha_url="$(printf %s "$sha_url" | tr -d '\r\n')"

    if [ -n "$AETHER_RELEASE_URL" ]; then
        echo -e "${YELLOW}尝试下载预编译网关: custom-url (linux-${arch})...${NC}"
    elif [ "$AETHER_RELEASE_LATEST" = "1" ]; then
        echo -e "${YELLOW}尝试下载预编译网关: latest (linux-${arch})...${NC}"
    else
        echo -e "${YELLOW}尝试下载预编译网关: ${tag} (linux-${arch})...${NC}"
    fi
    # Capture curl error so we can see why it failed over SSH.
    local curl_err=""
    if ! curl_err="$(curl -fsSL "$url" -o "$out" 2>&1)"; then
        echo -e "${YELLOW}Release URL (escaped): $(printf %q "$url")${NC}" 1>&2
        echo -e "${YELLOW}curl error:${NC} $(printf %s "$curl_err" | head -n 3)" 1>&2
        return 1
    fi
    chmod +x "$out"

    # Optional checksum verify (disabled by default to avoid extra network + file writes).
    if [ "$VERIFY_SHA256" = "1" ]; then
        # sha file may contain either "hash  dist/file" or "hash  file".
        local expected actual
        expected="$(curl -fsSL "$sha_url" 2>/dev/null | awk '{print $1}' | head -n 1 | tr -d '\r')"
        if [ -n "$expected" ]; then
            actual="$(sha256sum "$out" | awk '{print $1}')"
            if [ "$expected" != "$actual" ]; then
                echo -e "${RED}校验失败: sha256 不匹配${NC}"
                echo -e "${YELLOW}expected=${expected}${NC}"
                echo -e "${YELLOW}actual  =${actual}${NC}"
                return 1
            fi
        fi
    fi

    run_root install -m 0755 "$out" "$BIN_PATH"
    INSTALLED_FROM_RELEASE=1
    return 0
}

ensure_prereqs() {
    require_cmd curl || exit 1
    require_cmd systemctl || exit 1
    require_cmd openssl || exit 1
    # Go is only required when falling back to source build.
}

download_file() {
    local rel_path="$1"
    local force_update="$2"
    local dest="${AETHER_HOME}/${rel_path}"
    local url="$GITHUB_RAW_BASE/$rel_path"

    # Remove historical backups and do not create new ones.
    [ -f "${dest}.bak" ] && rm -f "${dest}.bak"
    if [ -f "$dest" ] && [ "$force_update" = "true" ]; then
        rm -f "$dest"
    fi

    if [ ! -f "$dest" ]; then
        echo -e "正在从 GitHub 获取/更新 ${YELLOW}$rel_path${NC}..."
        run_root mkdir -p "$(dirname "$dest")"
        if ! curl -fsSL "$url?$(date +%s)" -o "$dest"; then
            echo -e "${RED}错误: 下载 $rel_path 失败。${NC}"
            exit 1
        fi
    fi
}

cleanup_legacy_baks() {
    local count
    # Only touch our own home to avoid accidental deletions elsewhere.
    [ -d "$AETHER_HOME" ] || return 0
    count=$(find "$AETHER_HOME" -maxdepth 5 -type f -name "*.bak" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$count" != "0" ]; then
        find "$AETHER_HOME" -maxdepth 5 -type f -name "*.bak" -delete 2>/dev/null || true
        echo -e "${GREEN}已清理历史备份文件 (*.bak): $count 个。${NC}"
    fi
}

acme_bin() {
    echo "${ACME_HOME_DIR}/.acme.sh/acme.sh"
}

ensure_acme_sh() {
    run_root mkdir -p "$ACME_HOME_DIR"
    run_root chown "$(id -u)":"$(id -g)" "$ACME_HOME_DIR" 2>/dev/null || true

    if [ -x "$(acme_bin)" ]; then
        return 0
    fi

    echo -e "${YELLOW}正在安装 acme.sh...${NC}"
    if [ -z "$ACME_EMAIL" ]; then
        read_tty ACME_EMAIL "请输入 ACME 账号邮箱 (用于 Let's Encrypt; 可留空): " ""
    fi

    # Install into $ACME_HOME_DIR (do not pollute /root).
    (export HOME="$ACME_HOME_DIR"; curl -fsSL https://get.acme.sh | sh -s email="${ACME_EMAIL}")
    if [ ! -x "$(acme_bin)" ]; then
        echo -e "${RED}错误: acme.sh 安装失败。${NC}"
        exit 1
    fi
}

setup_acme_cert() {
    if [ "$ACME_ENABLE" != "1" ]; then
        return 0
    fi

    local domain cert_path key_path
    domain=$(grep "^DOMAIN=" "$ENV_FILE" | cut -d'=' -f2- | tr -d "'\"")
    cert_path="${AETHER_HOME}/deploy/certs/server.crt"
    key_path="${AETHER_HOME}/deploy/certs/server.key"

    if [ -z "$domain" ] || [ "$domain" = "your-domain.com" ] || [ "$domain" = "localhost" ]; then
        echo -e "${YELLOW}ACME_ENABLE=1 但 DOMAIN 未正确配置，跳过 acme.sh。${NC}"
        return 0
    fi

    ensure_acme_sh

    echo -e "${YELLOW}正在申请/更新证书 (acme.sh, mode=${ACME_MODE})...${NC}"

    if [ "$ACME_MODE" = "standalone" ]; then
        if port_in_use 80; then
            echo -e "${RED}错误: 80/tcp 被占用，无法使用 standalone HTTP-01。${NC}"
            echo -e "${YELLOW}退路:${NC}"
            echo -e "  1) 释放 80/tcp 后重试 (推荐)"
            echo -e "  2) 设置 ACME_MODE=alpn-stop (会短暂停服占用 443 走 TLS-ALPN-01)"
            return 1
        fi
        (export HOME="$ACME_HOME_DIR"; "$(acme_bin)" --set-default-ca --server "$ACME_CA" >/dev/null 2>&1 || true)
        (export HOME="$ACME_HOME_DIR"; "$(acme_bin)" --issue -d "$domain" --standalone --keylength "$ACME_KEYLENGTH")
    elif [ "$ACME_MODE" = "alpn-stop" ]; then
        echo -e "${YELLOW}ACME_MODE=alpn-stop: 将短暂停止网关以占用 443 申请证书。${NC}"
        run_root systemctl stop "$SERVICE_NAME" || true
        (export HOME="$ACME_HOME_DIR"; "$(acme_bin)" --set-default-ca --server "$ACME_CA" >/dev/null 2>&1 || true)
        (export HOME="$ACME_HOME_DIR"; "$(acme_bin)" --issue -d "$domain" --alpn --keylength "$ACME_KEYLENGTH")
        run_root systemctl start "$SERVICE_NAME" || true
    else
        echo -e "${RED}错误: 未知 ACME_MODE=$ACME_MODE。${NC}"
            return 1
    fi

    # Install cert and auto-reload gateway via SIGHUP.
    (export HOME="$ACME_HOME_DIR"; "$(acme_bin)" --install-cert -d "$domain" \
        --fullchain-file "$cert_path" \
        --key-file "$key_path" \
        --reloadcmd "systemctl kill -s HUP ${SERVICE_NAME}")

    echo -e "${GREEN}ACME 证书已安装: ${cert_path}${NC}"
    return 0
}

ensure_source() {
    run_root mkdir -p "$AETHER_HOME"
    run_root chown "$(id -u)":"$(id -g)" "$AETHER_HOME" 2>/dev/null || true

    if command -v git >/dev/null 2>&1; then
        if [ -d "${SRC_DIR}/.git" ]; then
            echo -e "${YELLOW}检测到已有源码目录，正在更新...${NC}"
            (cd "$SRC_DIR" && git fetch --all --prune)
        else
            echo -e "${YELLOW}正在拉取源码到 ${SRC_DIR}...${NC}"
            run_root rm -rf "$SRC_DIR"
            git clone --depth 1 "$GITHUB_REPO" "$SRC_DIR"
        fi
        (cd "$SRC_DIR" && {
            git checkout -f "$DEPLOY_REF" 2>/dev/null || git checkout -f "origin/$DEPLOY_REF"
            git pull --ff-only 2>/dev/null || true
        })
        return 0
    fi

    # Fallback: tarball download (requires no git).
    echo -e "${YELLOW}未检测到 git，使用源码压缩包方式拉取...${NC}"
    local tmp tgz extract_dir
    tmp="$(mktemp -d)"
    tgz="${tmp}/src.tgz"
    extract_dir="${tmp}/extract"
    mkdir -p "$extract_dir"

    # Note: this URL works for branches; if you need tags/commits, install git.
    if ! curl -fsSL "https://codeload.github.com/coolapijust/Aether-Realist/tar.gz/refs/heads/${DEPLOY_REF}" -o "$tgz"; then
        echo -e "${RED}错误: 下载源码压缩包失败。建议安装 git 后重试。${NC}"
        rm -rf "$tmp"
        exit 1
    fi
    tar -xzf "$tgz" -C "$extract_dir"
    local top
    top="$(find "$extract_dir" -maxdepth 1 -type d -name "Aether-Realist-*" | head -n 1)"
    if [ -z "$top" ]; then
        echo -e "${RED}错误: 解压源码失败。${NC}"
        rm -rf "$tmp"
        exit 1
    fi
    run_root rm -rf "$SRC_DIR"
    run_root mkdir -p "$(dirname "$SRC_DIR")"
    run_root mv "$top" "$SRC_DIR"
    rm -rf "$tmp"
}

ensure_env_file() {
    run_root mkdir -p "${AETHER_HOME}/deploy/certs" "${AETHER_HOME}/deploy/decoy"
    download_file "deploy/.env.example" "false"
    if [ ! -f "$ENV_FILE" ]; then
        cp "${AETHER_HOME}/deploy/.env.example" "$ENV_FILE"
    fi
}

set_env_kv() {
    local key="$1"
    local value="$2"
    if grep -q "^${key}=" "$ENV_FILE"; then
        sed -i "s#^${key}=.*#${key}=${value}#g" "$ENV_FILE"
    else
        echo "${key}=${value}" >> "$ENV_FILE"
    fi
}

prompt_core_config() {
    local current_psk current_domain current_port current_payload
    current_psk="$(env_get PSK "$ENV_FILE")"
    current_domain="$(env_get DOMAIN "$ENV_FILE")"
    current_port="$(env_get CADDY_PORT "$ENV_FILE")"
    current_payload="$(env_get RECORD_PAYLOAD_BYTES "$ENV_FILE")"

    # Allow env overrides for one-liner installs.
    # Example:
    #   PSK=xxx DOMAIN=example.com CADDY_PORT=443 RECORD_PAYLOAD_BYTES=16384 curl ... | sudo bash -s -- install
    # Note: under `set -u`, unbound variables abort the script. Always use ${VAR:-}.
    [ -n "${PSK:-}" ] && current_psk="${PSK}"
    [ -n "${DOMAIN:-}" ] && current_domain="${DOMAIN}"
    [ -n "${CADDY_PORT:-}" ] && current_port="${CADDY_PORT}"
    [ -n "${RECORD_PAYLOAD_BYTES:-}" ] && current_payload="${RECORD_PAYLOAD_BYTES}"

    if [ "$current_psk" = "your_super_secret_token" ] || [ -z "$current_psk" ]; then
        local auto_psk
        auto_psk=$(head /dev/urandom | tr -dc A-Za-z0-9 | head -c 32)
        read_tty input_psk "请输入 PSK (默认随机: $auto_psk): " ""
        current_psk="${input_psk:-$auto_psk}"
    fi

    if [ "$current_domain" = "your-domain.com" ] || [ -z "$current_domain" ]; then
        read_tty input_domain "请输入 DOMAIN (可随时修改): " ""
        current_domain="${input_domain:-localhost}"
    fi

    if [[ ! "$current_port" =~ ^[0-9]+$ ]]; then
        current_port=443
    fi
    read_tty input_port "监听端口 CADDY_PORT (默认: $current_port): " ""
    current_port="${input_port:-$current_port}"

    if ! validate_record_payload_size "$current_payload"; then
        current_payload=16384
    fi
    read_tty input_payload "设置 RECORD_PAYLOAD_BYTES [4096/8192/16384] (默认: $current_payload): " ""
    current_payload="${input_payload:-$current_payload}"
    if ! validate_record_payload_size "$current_payload"; then
        echo -e "${YELLOW}输入无效，回退为 16384。${NC}"
        current_payload=16384
    fi

    set_env_kv "PSK" "'$current_psk'"
    set_env_kv "DOMAIN" "'$current_domain'"
    set_env_kv "CADDY_PORT" "$current_port"
    set_env_kv "RECORD_PAYLOAD_BYTES" "$current_payload"
}

prepare_decoy_and_cert() {
    local decoy_root cert_path key_path
    decoy_root="${AETHER_HOME}/deploy/decoy"
    cert_path="${AETHER_HOME}/deploy/certs/server.crt"
    key_path="${AETHER_HOME}/deploy/certs/server.key"

    if [ ! -f "${AETHER_HOME}/deploy/decoy/index.html" ]; then
        cat > "${AETHER_HOME}/deploy/decoy/index.html" <<'EOF'
<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Aether Gateway</title></head>
<body><h1>Service Online</h1><p>Static decoy page.</p></body></html>
EOF
    fi

    if [ ! -f "$cert_path" ] || [ ! -f "$key_path" ]; then
        local domain
        domain=$(grep "^DOMAIN=" "$ENV_FILE" | cut -d'=' -f2- | tr -d "'\"")
        [ -z "$domain" ] && domain="localhost"
        echo -e "${YELLOW}未检测到证书，自动生成 10 年自签名证书...${NC}"
        openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
            -keyout "$key_path" \
            -out "$cert_path" \
            -subj "/CN=$domain" >/dev/null 2>&1
    fi

    set_env_kv "LISTEN_ADDR" ":$(env_get CADDY_PORT "$ENV_FILE")"
    set_env_kv "DECOY_ROOT" "$decoy_root"
    set_env_kv "SSL_CERT_FILE" "$cert_path"
    set_env_kv "SSL_KEY_FILE" "$key_path"
}

build_binary() {
    if [ "${INSTALLED_FROM_RELEASE:-0}" = "1" ]; then
        return 0
    fi

    if try_install_from_release; then
        echo -e "${GREEN}已从 Release 安装网关二进制。${NC}"
        return 0
    fi

    require_cmd go || {
        echo -e "${RED}错误: 未检测到 Go，且未能下载 Release 预编译二进制。${NC}"
        echo -e "${YELLOW}退路:${NC}"
        echo -e "  1) 安装 Go (go.mod 要求 Go 1.26) 后重试"
        echo -e "  2) 或设置 AETHER_RELEASE_TAG=某个已发布 tag 再试"
        exit 1
    }

    echo -e "${YELLOW}正在从源码构建网关二进制...${NC}"
    mkdir -p "${AETHER_HOME}/bin"
    (cd "$SRC_DIR" && go build -o "${AETHER_HOME}/bin/aether-gateway" ./cmd/aether-gateway)
    if [ ! -f "${AETHER_HOME}/bin/aether-gateway" ]; then
        echo -e "${RED}构建失败。${NC}"
        exit 1
    fi
    chmod +x "${AETHER_HOME}/bin/aether-gateway"
    run_root install -m 0755 "${AETHER_HOME}/bin/aether-gateway" "$BIN_PATH"
}

write_service() {
    local workdir env_abs
    workdir="${AETHER_HOME}"
    env_abs="${ENV_FILE}"
    run_root tee "$SERVICE_FILE" >/dev/null <<EOF
[Unit]
Description=Aether Realist Gateway (Native)
After=network.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${workdir}
EnvironmentFile=${env_abs}
ExecStart=${BIN_PATH}
Restart=always
RestartSec=2
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
}

install_or_update_service() {
    log "step=prereqs"
    ensure_prereqs

    log "step=release_download_or_source_sync"
    # Only fetch source if we might need to build; we try release binary first.
    INSTALLED_FROM_RELEASE=0
    if ! try_install_from_release; then
        log "release download failed, falling back to source sync"
        ensure_source
    else
        log "release download ok"
    fi

    log "step=cleanup_baks"
    cleanup_legacy_baks

    log "step=env_file"
    ensure_env_file

    log "step=prompt_config"
    prompt_core_config
    # Optional: obtain real cert before generating self-signed.
    log "step=acme(optional)"
    setup_acme_cert || true
    log "step=decoy_and_cert"
    prepare_decoy_and_cert
    log "step=install_binary"
    build_binary
    log "step=write_systemd"
    write_service

    log "step=systemd_start"
    run_root systemctl daemon-reload
    run_root systemctl enable --now "$SERVICE_NAME"
    run_root systemctl restart "$SERVICE_NAME"

    echo -e "${GREEN}部署完成。${NC}"
    show_status
}

show_status() {
    echo -e "\n${YELLOW}=== Native 服务状态 ===${NC}"
    run_root systemctl --no-pager --full status "$SERVICE_NAME" | sed -n '1,18p' || true
    local port
    port="$(env_get CADDY_PORT "$ENV_FILE")"
    [ -z "$port" ] && port=443
    echo -e "\n${YELLOW}--- 健康检查 ---${NC}"
    if curl -ksI "https://localhost:${port}/health" | grep -q "200 OK"; then
        echo -e "${GREEN}[OK] https://localhost:${port}/health${NC}"
    else
        echo -e "${RED}[WARN] 健康检查失败，请查看日志。${NC}"
    fi
}

view_logs() {
    run_root journalctl -u "$SERVICE_NAME" -f --no-pager
}

stop_service() {
    run_root systemctl stop "$SERVICE_NAME"
    echo -e "${GREEN}服务已停止。${NC}"
}

remove_service() {
    run_root systemctl disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
    run_root rm -f "$SERVICE_FILE"
    run_root systemctl daemon-reload
    run_root rm -f "$BIN_PATH"
    cleanup_legacy_baks
    echo -e "${GREEN}服务及二进制已移除。配置文件保留在 ${ENV_FILE}。${NC}"
}

show_menu() {
    echo -e "\n${GREEN}请选择操作：${NC}"
    echo "1. 安装 / 更新服务 (Native)"
    echo "2. 暂停服务"
    echo "3. 删除服务"
    echo "4. 查看状态"
    echo "5. 查看日志"
    echo "0. 退出"
    read_tty option "请输入选项 [0-5]: " ""
    case "$option" in
        1) install_or_update_service ;;
        2) stop_service ;;
        3) remove_service ;;
        4) show_status ;;
        5) view_logs ;;
        0) exit 0 ;;
        *) echo -e "${RED}无效选项${NC}" ;;
    esac
}

if [ -n "$1" ]; then
    case "$1" in
        install|update) install_or_update_service ;;
        stop) stop_service ;;
        remove) remove_service ;;
        status) show_status ;;
        logs) view_logs ;;
        *) echo "Usage: $0 {install|update|stop|remove|status|logs}" ;;
    esac
else
    while true; do
        show_menu
        echo -e "\n按任意键返回菜单..."
        if [ -r /dev/tty ]; then
            read -n 1 </dev/tty || true
        else
            read -n 1 || true
        fi
    done
fi
