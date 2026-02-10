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
            echo -e "${YELLOW}请输入证书的【完整文件路径】${NC}"
            echo "示例: /root/cert/fullchain.pem"
            read -p "证书路径 (.crt/.pem): " CERT_PATH
            read -p "私钥路径 (.key): " KEY_PATH

            if [ -f "$CERT_PATH" ] && [ -f "$KEY_PATH" ]; then
                echo -e "已确认证书: ${GREEN}$CERT_PATH${NC}"
                echo -e "已确认私钥: ${GREEN}$KEY_PATH${NC}"
                mkdir -p deploy/certs
                cp "$CERT_PATH" deploy/certs/manual.crt
                cp "$KEY_PATH" deploy/certs/manual.key
                TLS_CONFIG="/certs/manual.crt /certs/manual.key"
            else
                echo -e "${RED}错误：找不到指定的文件，请检查路径是否正确。${NC}"
                echo -e "将回退到自签名模式。"
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

show_status() {
    echo -e "\n${YELLOW}=== Aether-Realist 服务状态 ===${NC}"
    if [ -f "deploy/docker-compose.yml" ]; then
        docker compose -f deploy/docker-compose.yml ps
        
        echo -e "\n${YELLOW}--- 健康检查 (API 响应头) ---${NC}"
        PSK=$(grep "^PSK=" "deploy/.env" | cut -d'=' -f2)
        if curl -sI -H "X-Aether-PSK: $PSK" "http://localhost:8080/probe" | grep -q "200 OK"; then
             echo -e "${GREEN}[OK] 网关探针响应正常 (Port 8080)${NC}"
        elif curl -sI -H "X-Aether-PSK: $PSK" "http://localhost:80/probe" | grep -q "200 OK"; then
             echo -e "${GREEN}[OK] 网关探针响应正常 (Port 80)${NC}"
        else
             echo -e "${RED}[WARN] 无法从本地回环确认 API 连通性，请检查 logs${NC}"
        fi
    else
        echo -e "${RED}未找到部署文件。${NC}"
    fi
}

view_logs() {
    echo -e "\n${YELLOW}请选择要查看日志的服务：${NC}"
    echo "1) 网关 (Gateway/Caddy)"
    echo "2) 后端 (Backend)"
    echo "3) 所有日志"
    read -p "请输入 [1-3]: " LOG_OPT
    case $LOG_OPT in
        1) docker compose -f deploy/docker-compose.yml logs -f --tail 100 gateway ;;
        2) docker compose -f deploy/docker-compose.yml logs -f --tail 100 backend ;;
        3) docker compose -f deploy/docker-compose.yml logs -f --tail 100 ;;
        *) echo "无效指令" ;;
    esac
}

check_bbr() {
    echo -e "\n${YELLOW}=== 系统传输加速 (BBR) 检查 ===${NC}"
    if lsmod | grep -q "bbr"; then
        echo -e "${GREEN}[OK] BBR 已开启。${NC}"
    else
        echo -e "${RED}[WARN] BBR 未开启。强烈建议在高丢包网络环境下开启。${NC}"
        echo "开启指令参考:"
        echo "echo \"net.core.default_qdisc=fq\" >> /etc/sysctl.conf"
        echo "echo \"net.ipv4.tcp_congestion_control=bbr\" >> /etc/sysctl.conf"
        echo "sysctl -p"
    fi
}

quick_config() {
    ENV_FILE="deploy/.env"
    if [ ! -f "$ENV_FILE" ]; then echo "错误: .env 不存在"; return; fi
    
    echo -e "\n${YELLOW}=== 快捷参数修改 ===${NC}"
    echo "1) 修改 PSK"
    echo "2) 修改 域名 (DOMAIN)"
    read -p "请输入 [1/2]: " CFG_OPT
    if [ "$CFG_OPT" = "1" ]; then
        read -p "新 PSK: " NEW_PSK
        [ -n "$NEW_PSK" ] && sed -i "s/^PSK=.*/PSK=$NEW_PSK/" "$ENV_FILE"
    elif [ "$CFG_OPT" = "2" ]; then
        read -p "新域名: " NEW_DOM
        [ -n "$NEW_DOM" ] && sed -i "s/^DOMAIN=.*/DOMAIN=$NEW_DOM/" "$ENV_FILE"
    fi
    echo -e "${GREEN}配置已更新。${NC}"
    read -p "是否同步重启容器以应用配置? [y/N]: " RESTART_CONFIRM
    if [[ "$RESTART_CONFIRM" =~ ^[Yy]$ ]]; then
        docker compose -f deploy/docker-compose.yml up -d
    fi
}

# 菜单逻辑
show_menu() {
    echo -e "\n${GREEN}请选择操作：${NC}"
    echo "1. 安装 / 更新服务"
    echo "2. 暂停服务"
    echo "3. 删除服务"
    echo "-----------------"
    echo "4. 查看实时运行状态"
    echo "5. 查看各服务实时日志"
    echo "6. 检查系统优化 (BBR)"
    echo "7. 快捷修改关键配置"
    echo "-----------------"
    echo "0. 退出"
    read -p "请输入选项 [0-7]: " OPTION
    case $OPTION in
        1) install_service ;;
        2) stop_service ;;
        3) remove_service ;;
        4) show_status ;;
        5) view_logs ;;
        6) check_bbr ;;
        7) quick_config ;;
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
