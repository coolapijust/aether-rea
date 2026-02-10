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

# 1. 环境检查
echo -e "\n${YELLOW}[1/5] 正在检查环境...${NC}"

if ! command -v docker &> /dev/null; then
    echo -e "${RED}错误: 未检测到 Docker，请先安装 Docker。${NC}"
    exit 1
fi

if ! docker compose version &> /dev/null; then
    echo -e "${RED}错误: 未检测到 Docker Compose (v2+)，请先安装。${NC}"
    exit 1
fi

echo -e "${GREEN}环境检查通过。${NC}"

# 2. 目录与依赖准备
echo -e "\n${YELLOW}[2/5] 准备工作目录与依赖...${NC}"

GITHUB_RAW_BASE="https://raw.githubusercontent.com/coolapijust/Aether-Realist/main"

download_file() {
    local FILE_PATH=$1
    local URL="$GITHUB_RAW_BASE/$FILE_PATH"
    if [ ! -f "$FILE_PATH" ]; then
        echo -e "正在从 GitHub 下载 ${YELLOW}$FILE_PATH${NC}..."
        mkdir -p "$(dirname "$FILE_PATH")"
        if ! curl -sL "$URL" -o "$FILE_PATH"; then
             echo -e "${RED}错误: 下载 $FILE_PATH 失败，请检查网络。${NC}"
             exit 1
        fi
    fi
}

# 确保 deploy 目录存在
mkdir -p deploy/certs
chmod 755 deploy/certs

# 下载缺失的关键文件
download_file "deploy/docker-compose.yml"
download_file "deploy/.env.example"
download_file "deploy/Caddyfile"

echo -e "${GREEN}目录结构与依赖文件已就绪。${NC}"

# 3. 配置初始化
echo -e "\n${YELLOW}[3/5] 配置环境变量...${NC}"

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
        exit 1
    fi
    sed -i "/^DOMAIN=/d" "$ENV_FILE"
    echo "DOMAIN=$DOMAIN" >> "$ENV_FILE"
fi

echo -e "${GREEN}环境变量配置完成。${NC}"

# 4. 内核优化 (可选)
echo -e "\n${YELLOW}[4/5] 检查内核参数优化 (UDP 缓存)...${NC}"
RMEM_MAX=$(sysctl -n net.core.rmem_max 2>/dev/null || echo 0)
if [ "$RMEM_MAX" -lt 16777216 ]; then
    echo -e "${YELLOW}检测到 UDP 接收缓冲区较小 ($RMEM_MAX)，可能影响 QUIC 性能。${NC}"
    read -p "是否尝试自动优化内核参数? (需要 sudo 权限) [y/N]: " OPTIMIZE
    if [[ "$OPTIMIZE" =~ ^[Yy]$ ]]; then
        sudo sysctl -w net.core.rmem_max=16777216
        sudo sysctl -w net.core.wmem_max=16777216
        echo -e "${GREEN}内核参数已临时调整。若需永久生效，请参考 docs/deployment.md 修改 /etc/sysctl.conf${NC}"
    fi
else
    echo -e "${GREEN}内核参数已达标。${NC}"
fi

# 5. 启动服务
echo -e "\n${YELLOW}[5/5] 正在拉取镜像并启动服务...${NC}"
docker compose -f deploy/docker-compose.yml up -d

echo -e "\n${GREEN}==============================================${NC}"
echo -e "${GREEN}    部署成功！Aether-Realist 正在运行。      ${NC}"
echo -e "    控制面板 (Caddy): https://${DOMAIN}"
echo -e "    网关端口 (UDP): 8080 (WebTransport)"
echo -e "==============================================${NC}"
echo -e "提示: 查看日志请使用 'docker compose -f deploy/docker-compose.yml logs -f'"
