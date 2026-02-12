#!/bin/bash

# Aether-Realist Native (non-Docker) one-click deploy script
# Runtime: systemd + local binary

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}==============================================${NC}"
echo -e "${GREEN}   Aether-Realist Native 一键部署工具         ${NC}"
echo -e "${GREEN}==============================================${NC}"

DEPLOY_REF="${DEPLOY_REF:-main}"
GITHUB_RAW_BASE="https://raw.githubusercontent.com/coolapijust/Aether-Realist/${DEPLOY_REF}"
SERVICE_NAME="aether-gateway"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
BIN_PATH="/usr/local/bin/aether-gateway"
ENV_FILE="deploy/.env"

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

ensure_prereqs() {
    if ! command -v go >/dev/null 2>&1; then
        echo -e "${RED}错误: 未检测到 Go，请先安装 Go (>=1.23)。${NC}"
        exit 1
    fi
    if ! command -v systemctl >/dev/null 2>&1; then
        echo -e "${RED}错误: 未检测到 systemd (systemctl)。${NC}"
        exit 1
    fi
    if ! command -v openssl >/dev/null 2>&1; then
        echo -e "${RED}错误: 未检测到 openssl。${NC}"
        exit 1
    fi
}

download_file() {
    local file_path="$1"
    local force_update="$2"
    local url="$GITHUB_RAW_BASE/$file_path"

    # Remove historical backups and do not create new ones.
    [ -f "${file_path}.bak" ] && rm -f "${file_path}.bak"
    if [ -f "$file_path" ] && [ "$force_update" = "true" ]; then
        rm -f "$file_path"
    fi

    if [ ! -f "$file_path" ]; then
        echo -e "正在从 GitHub 获取/更新 ${YELLOW}$file_path${NC}..."
        mkdir -p "$(dirname "$file_path")"
        if ! curl -fsSL "$url?$(date +%s)" -o "$file_path"; then
            echo -e "${RED}错误: 下载 $file_path 失败。${NC}"
            exit 1
        fi
    fi
}

cleanup_legacy_baks() {
    local count
    count=$(find . -maxdepth 3 -type f -name "*.bak" | wc -l | tr -d ' ')
    if [ "$count" != "0" ]; then
        find . -maxdepth 3 -type f -name "*.bak" -delete
        echo -e "${GREEN}已清理历史备份文件 (*.bak): $count 个。${NC}"
    fi
}

ensure_env_file() {
    mkdir -p deploy/certs deploy/decoy
    download_file "deploy/.env.example" "false"
    if [ ! -f "$ENV_FILE" ]; then
        cp deploy/.env.example "$ENV_FILE"
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
    current_psk=$(grep "^PSK=" "$ENV_FILE" | cut -d'=' -f2- | tr -d "'\"")
    current_domain=$(grep "^DOMAIN=" "$ENV_FILE" | cut -d'=' -f2- | tr -d "'\"")
    current_port=$(grep "^CADDY_PORT=" "$ENV_FILE" | cut -d'=' -f2- | tr -d "'\"[:space:]")
    current_payload=$(grep "^RECORD_PAYLOAD_BYTES=" "$ENV_FILE" | cut -d'=' -f2- | tr -d "'\"[:space:]")

    if [ "$current_psk" = "your_super_secret_token" ] || [ -z "$current_psk" ]; then
        local auto_psk
        auto_psk=$(head /dev/urandom | tr -dc A-Za-z0-9 | head -c 32)
        read -p "请输入 PSK (默认随机: $auto_psk): " input_psk
        current_psk="${input_psk:-$auto_psk}"
    fi

    if [ "$current_domain" = "your-domain.com" ] || [ -z "$current_domain" ]; then
        read -p "请输入 DOMAIN (可随时修改): " input_domain
        current_domain="${input_domain:-localhost}"
    fi

    if [[ ! "$current_port" =~ ^[0-9]+$ ]]; then
        current_port=443
    fi
    read -p "监听端口 CADDY_PORT (默认: $current_port): " input_port
    current_port="${input_port:-$current_port}"

    if ! validate_record_payload_size "$current_payload"; then
        current_payload=16384
    fi
    read -p "设置 RECORD_PAYLOAD_BYTES [4096/8192/16384] (默认: $current_payload): " input_payload
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
    decoy_root="$(pwd)/deploy/decoy"
    cert_path="$(pwd)/deploy/certs/server.crt"
    key_path="$(pwd)/deploy/certs/server.key"

    if [ ! -f "deploy/decoy/index.html" ]; then
        cat > deploy/decoy/index.html <<'EOF'
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

    set_env_kv "LISTEN_ADDR" ":$(grep '^CADDY_PORT=' "$ENV_FILE" | cut -d'=' -f2- | tr -d "'\"[:space:]")"
    set_env_kv "DECOY_ROOT" "$decoy_root"
    set_env_kv "SSL_CERT_FILE" "$cert_path"
    set_env_kv "SSL_KEY_FILE" "$key_path"
}

build_binary() {
    echo -e "${YELLOW}正在构建网关二进制...${NC}"
    go build -o ./bin/aether-gateway ./cmd/aether-gateway
    if [ ! -f ./bin/aether-gateway ]; then
        echo -e "${RED}构建失败。${NC}"
        exit 1
    fi
    chmod +x ./bin/aether-gateway
    sudo install -m 0755 ./bin/aether-gateway "$BIN_PATH"
}

write_service() {
    local workdir env_abs
    workdir="$(pwd)"
    env_abs="${workdir}/${ENV_FILE}"
    sudo tee "$SERVICE_FILE" >/dev/null <<EOF
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
    ensure_prereqs
    cleanup_legacy_baks
    ensure_env_file
    prompt_core_config
    prepare_decoy_and_cert
    build_binary
    write_service

    echo -e "${YELLOW}正在启动/更新服务...${NC}"
    sudo systemctl daemon-reload
    sudo systemctl enable --now "$SERVICE_NAME"
    sudo systemctl restart "$SERVICE_NAME"

    echo -e "${GREEN}部署完成。${NC}"
    show_status
}

show_status() {
    echo -e "\n${YELLOW}=== Native 服务状态 ===${NC}"
    sudo systemctl --no-pager --full status "$SERVICE_NAME" | sed -n '1,18p' || true
    local port
    port=$(grep "^CADDY_PORT=" "$ENV_FILE" | cut -d'=' -f2- | tr -d "'\"[:space:]")
    [ -z "$port" ] && port=443
    echo -e "\n${YELLOW}--- 健康检查 ---${NC}"
    if curl -ksI "https://localhost:${port}/health" | grep -q "200 OK"; then
        echo -e "${GREEN}[OK] https://localhost:${port}/health${NC}"
    else
        echo -e "${RED}[WARN] 健康检查失败，请查看日志。${NC}"
    fi
}

view_logs() {
    sudo journalctl -u "$SERVICE_NAME" -f --no-pager
}

stop_service() {
    sudo systemctl stop "$SERVICE_NAME"
    echo -e "${GREEN}服务已停止。${NC}"
}

remove_service() {
    sudo systemctl disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
    sudo rm -f "$SERVICE_FILE"
    sudo systemctl daemon-reload
    sudo rm -f "$BIN_PATH"
    cleanup_legacy_baks
    echo -e "${GREEN}服务及二进制已移除。配置文件保留在 deploy/.env。${NC}"
}

show_menu() {
    echo -e "\n${GREEN}请选择操作：${NC}"
    echo "1. 安装 / 更新服务 (Native)"
    echo "2. 暂停服务"
    echo "3. 删除服务"
    echo "4. 查看状态"
    echo "5. 查看日志"
    echo "0. 退出"
    read -p "请输入选项 [0-5]: " option
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
        read -n 1
    done
fi
