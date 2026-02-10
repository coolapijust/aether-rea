#!/bin/bash

# Aether-Realist V5 一键部署脚本
# 适用环境：Ubuntu/Debian/CentOS (Linux x64)

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}==============================================${NC}"
echo -e "${GREEN}    Aether-Realist V5 一键部署工具          ${NC}"
echo -e "${GREEN}==============================================${NC}"

GITHUB_RAW_BASE="https://raw.githubusercontent.com/coolapijust/Aether-Realist/main"

download_file() {
    local FILE_PATH=$1
    local FORCE_UPDATE=$2
    local URL="$GITHUB_RAW_BASE/$FILE_PATH"
    
    if [ -f "$FILE_PATH" ] && [ "$FORCE_UPDATE" = "true" ]; then
        # 如果是强制更新且文件已存在，先备份
        mv "$FILE_PATH" "${FILE_PATH}.bak"
        echo -e "已备份旧的 ${YELLOW}$FILE_PATH${NC} 为 ${YELLOW}${FILE_PATH}.bak${NC}"
    fi

    if [ ! -f "$FILE_PATH" ]; then
        echo -e "正在从 GitHub 获取/更新 ${YELLOW}$FILE_PATH${NC}..."
        mkdir -p "$(dirname "$FILE_PATH")"
        if ! curl -sL "$URL?$(date +%s)" -o "$FILE_PATH"; then
             echo -e "${RED}错误: 下载 $FILE_PATH 失败，请检查网络。${NC}"
             # 如果下载失败且有备份，还原备份
             [ -f "${FILE_PATH}.bak" ] && mv "${FILE_PATH}.bak" "$FILE_PATH"
             exit 1
        fi
    fi
}

# 核心逻辑函数
install_service() {
    echo -e "\n${YELLOW}[1/4] 环境检查...${NC}"
    if ! command -v docker &> /dev/null; then
        echo -e "${RED}错误: 未检测到 Docker，请先安装 Docker。${NC}"
        return 1
    fi

    echo -e "\n${YELLOW}[2/4] 准备工作目录与依赖...${NC}"
    mkdir -p deploy/certs
    chmod 755 deploy/certs

    # 强制更新编排文件
    download_file "deploy/docker-compose.yml" "true"
    download_file "deploy/Caddyfile" "true"
    download_file "deploy/.env.example" "false"

    echo -e "\n${YELLOW}[3/4] 配置环境变量...${NC}"
    ENV_FILE="deploy/.env"
    if [ ! -f "$ENV_FILE" ]; then
        if [ -f "deploy/.env.example" ]; then
            cp deploy/.env.example "$ENV_FILE"
        else
            touch "$ENV_FILE"
        fi
    fi

    # 获取当前配置
    CURRENT_PSK=$(grep "^PSK=" "$ENV_FILE" | cut -d'=' -f2)
    CURRENT_DOMAIN=$(grep "^DOMAIN=" "$ENV_FILE" | cut -d'=' -f2)

    # 交互输入 PSK
    if [ -z "$CURRENT_PSK" ]; then
        AUTO_PSK=$(head /dev/urandom | tr -dc A-Za-z0-9 | head -c 32)
        read -p "请输入预共享密钥 PSK (默认随机生成: $AUTO_PSK): " PSK
        PSK=${PSK:-$AUTO_PSK}
        sed -i "/^PSK=/d" "$ENV_FILE"
        echo "PSK=$PSK" >> "$ENV_FILE"
    fi

    # 交互输入 DOMAIN
    if [ -z "$CURRENT_DOMAIN" ]; then
        read -p "请输入部署域名 (例如: example.com): " DOMAIN
        if [ -z "$DOMAIN" ]; then
            echo -e "${RED}错误: 域名不能为空。${NC}"
            return 1
        fi
        sed -i "/^DOMAIN=/d" "$ENV_FILE"
        echo "DOMAIN=$DOMAIN" >> "$ENV_FILE"
    fi

    # 4. 智能端口检测与证书配置
    echo -e "\n${YELLOW}[4/5] 检测端口与配置证书...${NC}"
    check_port() {
        if command -v netstat &> /dev/null; then
            netstat -tulpn | grep -q ":$1 "
        elif command -v ss &> /dev/null; then
            ss -tulpn | grep -q ":$1 "
        else
            echo "0" # 无法检测，默认认为未占用或由 Caddy 自身报错
            return 1
        fi
    }

    CADDY_SITE_ADDRESS=""
    TLS_CONFIG=""

    if ! check_port 80 && ! check_port 443; then
        echo -e "${GREEN}检测到 80/443 端口空闲，将启用自动 HTTPS (Let's Encrypt)。${NC}"
        CADDY_SITE_ADDRESS="{$DOMAIN}"
        TLS_CONFIG="internal" # 实际上 Caddy 会自动覆盖，但给个默认值
    else
        echo -e "${YELLOW}检测到 80/443 端口被占用。${NC}"
        echo "请选择证书策略 (将运行在 8080 端口):"
        echo "1) 我有自己的证书 (输入路径)"
        echo "2) 自动生成自签名证书 (仅测试用)"
        read -p "请输入选项 [1/2]: " CERT_OPTION

        CADDY_SITE_ADDRESS=":8080"
        
        if [ "$CERT_OPTION" = "1" ]; then
            echo -e "${YELLOW}请输入证书所在的【目录路径】或【完整文件路径】${NC}"
            echo "示例: /root/cert 或 /root/cert/fullchain.pem"
            read -p "证书路径 (.crt/.pem): " CERT_INPUT
            read -p "私钥路径 (.key): " KEY_INPUT
            
            # 智能识别逻辑：支持目录或文件路径，并仅通过【文件内容】校验（忽略扩展名）
            find_cert_files() {
                local SEARCH_PATH="$1"
                # 如果输入是目录，扫描目录下的所有常规文件；如果是文件，直接检查该文件
                if [ -d "$SEARCH_PATH" ]; then
                    FILES=$(find "$SEARCH_PATH" -maxdepth 1 -type f)
                else
                    FILES="$SEARCH_PATH"
                fi

                for f in $FILES; do
                    # 忽略扩展名，只看内容
                    if grep -q "BEGIN CERTIFICATE" "$f"; then
                        CERT_PATH="$f"
                    elif grep -q "BEGIN.*PRIVATE KEY" "$f"; then
                        KEY_PATH="$f"
                    fi
                done
            }

            # 第一次通过输入寻找
            find_cert_files "$CERT_INPUT"
            # 如果没找全，再通过第二个输入寻找 (允许用户分别输入两个目录，或者一个目录一个文件)
            find_cert_files "$KEY_INPUT"

            if [ -f "$CERT_PATH" ] && [ -f "$KEY_PATH" ]; then
                echo -e "已识别证书: ${GREEN}$CERT_PATH${NC}"
                echo -e "已识别私钥: ${GREEN}$KEY_PATH${NC}"
                mkdir -p deploy/certs
                cp "$CERT_PATH" deploy/certs/manual.crt
                cp "$KEY_PATH" deploy/certs/manual.key
                TLS_CONFIG="/certs/manual.crt /certs/manual.key"
            else
                echo -e "${RED}错误：未能找到有效的证书/私钥对。${NC}"
                echo -e "请确保文件中包含 'BEGIN CERTIFICATE' 和 'BEGIN ... PRIVATE KEY'"
                echo "DEBUG: Cert=$CERT_PATH, Key=$KEY_PATH"
                CERT_OPTION="2"
            fi
        fi

        if [ "$CERT_OPTION" != "1" ]; then
            echo -e "${YELLOW}正在生成自签名证书...${NC}"
            mkdir -p deploy/certs
            openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
                -keyout deploy/certs/selfsigned.key \
                -out deploy/certs/selfsigned.crt \
                -subj "/CN=$DOMAIN" &> /dev/null
            TLS_CONFIG="/certs/selfsigned.crt /certs/selfsigned.key"
            echo -e "${GREEN}自签名证书已生成。${NC}"
        fi
    fi

    # 写入环境变量文件 (覆盖旧的配置)
    sed -i "/^CADDY_SITE_ADDRESS=/d" "$ENV_FILE"
    sed -i "/^TLS_CONFIG=/d" "$ENV_FILE"
    echo "CADDY_SITE_ADDRESS=$CADDY_SITE_ADDRESS" >> "$ENV_FILE"
    echo "TLS_CONFIG=$TLS_CONFIG" >> "$ENV_FILE"

    # 4.5 内核优化 (可选)
    echo -e "\n${YELLOW}[4/4] 检查内核参数优化 (UDP 缓存)...${NC}"
    RMEM_MAX=$(sysctl -n net.core.rmem_max 2>/dev/null || echo 0)
    if [ "$RMEM_MAX" -lt 16777216 ]; then
        echo -e "${YELLOW}检测到 UDP 接收缓冲区较小 ($RMEM_MAX)。${NC}"
        read -p "是否尝试自动优化内核参数? (需要 sudo 权限) [y/N]: " OPTIMIZE
        if [[ "$OPTIMIZE" =~ ^[Yy]$ ]]; then
            sudo sysctl -w net.core.rmem_max=16777216
            sudo sysctl -w net.core.wmem_max=16777216
            echo -e "${GREEN}内核参数已临时调整。${NC}"
        fi
    fi

    echo -e "\n${YELLOW}正在启动服务...${NC}"
    docker compose -f deploy/docker-compose.yml up -d
    echo -e "${GREEN}服务启动成功！${NC}"
}

stop_service() {
    echo -e "\n${YELLOW}正在暂停服务...${NC}"
    if [ -f "deploy/docker-compose.yml" ]; then
        docker compose -f deploy/docker-compose.yml stop
        echo -e "${GREEN}服务已暂停。${NC}"
    else
        echo -e "${RED}未找到 deploy/docker-compose.yml，无法操作。${NC}"
    fi
}

remove_service() {
    echo -e "\n${RED}警告：此操作将删除所有容器和网络。${NC}"
    read -p "确认删除服务吗? [y/N]: " CONFIRM
    if [[ "$CONFIRM" =~ ^[Yy]$ ]]; then
        if [ -f "deploy/docker-compose.yml" ]; then
            docker compose -f deploy/docker-compose.yml down
            echo -e "${GREEN}服务已移除。${NC}"
            
            read -p "是否同时清理未使用镜像 (docker image prune)? [y/N]: " PRUNE
            if [[ "$PRUNE" =~ ^[Yy]$ ]]; then
                docker image prune -f
            fi
        else
            echo -e "${RED}未找到 deploy/docker-compose.yml。${NC}"
        fi
    else
        echo "操作取消。"
    fi
}

# 菜单逻辑
show_menu() {
    echo -e "\n${GREEN}请选择操作：${NC}"
    echo "1. 安装 / 更新服务"
    echo "2. 暂停服务"
    echo "3. 删除服务"
    echo "0. 退出"
    read -p "请输入选项 [0-3]: " OPTION
    case $OPTION in
        1) install_service ;;
        2) stop_service ;;
        3) remove_service ;;
        0) exit 0 ;;
        *) echo -e "${RED}无效选项${NC}" ;;
    esac
}

# 主程序入口
if [ -n "$1" ]; then
    # 支持命令行参数模式 (non-interactive)
    case "$1" in
        install) install_service ;;
        stop) stop_service ;;
        remove) remove_service ;;
        *) echo "Usage: $0 {install|stop|remove}" ;;
    esac
else
    # 交互模式
    while true; do
        show_menu
        echo -e "\n按任意键返回菜单..."
        read -n 1
    done
fi
