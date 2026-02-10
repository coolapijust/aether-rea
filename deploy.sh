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

    # 4. 内核优化 (可选)
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
