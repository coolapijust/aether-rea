#!/bin/bash

# Aether-Realist V5.1 一键部署脚本
# 适用环境：Ubuntu/Debian/CentOS (Linux x64)

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}==============================================${NC}"
echo -e "${GREEN}    Aether-Realist V5.1 一键部署工具          ${NC}"
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

# 自我更新功能
check_self_update() {
    # 防止递归更新死循环
    if [ "$1" == "--no-update" ]; then
        return
    fi

    echo -e "${YELLOW}正在检查部署脚本更新...${NC}"
    local SELF_URL="$GITHUB_RAW_BASE/deploy.sh"
    local TEMP_SCRIPT="/tmp/deploy.sh.tmp"
    
    # 下载最新脚本
    if curl -sL "$SELF_URL?$(date +%s)" -o "$TEMP_SCRIPT"; then
        # 简单校验：且确保文件非空
        if [ -s "$TEMP_SCRIPT" ] && grep -q "Aether-Realist" "$TEMP_SCRIPT"; then
            # 比较差异
            if ! cmp -s "$TEMP_SCRIPT" "$0"; then
                echo -e "${GREEN}发现新版本！正在自动更新并重启脚本...${NC}"
                chmod +x "$TEMP_SCRIPT"
                mv "$TEMP_SCRIPT" "$0"
                # 使用 exec 替换当前进程，并带上防递归参数
                exec "$0" --no-update "$@"
            else
                echo -e "脚本已是最新。"
                rm -f "$TEMP_SCRIPT"
            fi
        else
            echo -e "${RED}更新校验失败，跳过。${NC}"
            rm -f "$TEMP_SCRIPT"
        fi
    else
        echo -e "${RED}检查更新失败，继续使用当前版本。${NC}"
    fi
}

# 执行自动更新检查
check_self_update "$1"

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
    CURRENT_CADDY_PORT=$(grep "^CADDY_PORT=" "$ENV_FILE" | cut -d'=' -f2 | tr -d "'\"[:space:]")

    # 过滤默认占位符 (防止 .env.example 的值被误用)
    if [ "$CURRENT_PSK" = "your_super_secret_token" ]; then
        CURRENT_PSK=""
    fi
    if [ "$CURRENT_DOMAIN" = "your-domain.com" ]; then
        CURRENT_DOMAIN=""
    fi

    # 交互输入 PSK
    if [ -z "$CURRENT_PSK" ]; then
        AUTO_PSK=$(head /dev/urandom | tr -dc A-Za-z0-9 | head -c 32)
        read -p "请输入预共享密钥 PSK (默认随机生成: $AUTO_PSK): " PSK
        PSK=${PSK:-$AUTO_PSK}
        
        # 安全转义: 将所有单引号替换为 '\'' (结束当前引号，转义单引号，重新开始引号)
        # 这样 abc'123 就会变成 abc'\''123，配合外层单引号包裹，最终在 .env 中变成 'abc'\''123'
        SAFE_PSK=$(echo "$PSK" | sed "s/'/'\\\\''/g")
        
        sed -i "/^PSK=/d" "$ENV_FILE"
        echo "PSK='$SAFE_PSK'" >> "$ENV_FILE"
    fi

    # 交互输入 DOMAIN
    if [ -z "$CURRENT_DOMAIN" ]; then
        read -p "请输入部署域名 (例如: example.com): " DOMAIN
        if [ -z "$DOMAIN" ]; then
            echo -e "${RED}错误: 域名不能为空。${NC}"
            return 1
        fi
        
        SAFE_DOMAIN=$(echo "$DOMAIN" | sed "s/'/'\\\\''/g")
        
        sed -i "/^DOMAIN=/d" "$ENV_FILE"
        echo "DOMAIN='$SAFE_DOMAIN'" >> "$ENV_FILE"
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

    # 若已安装过服务，更新时优先沿用已有端口，避免把“自身监听”误判为冲突
    if [[ "$CURRENT_CADDY_PORT" =~ ^[0-9]+$ ]] && [ "$CURRENT_CADDY_PORT" -ge 1 ] && [ "$CURRENT_CADDY_PORT" -le 65535 ]; then
        CADDY_PORT="$CURRENT_CADDY_PORT"
        echo -e "${GREEN}检测到现有端口配置 CADDY_PORT=$CADDY_PORT，更新将沿用该端口。${NC}"
        if [ "$CADDY_PORT" != "443" ]; then
            CADDY_SITE_ADDRESS=":$CADDY_PORT"
        fi
    else
        if ! check_port 443; then
            echo -e "${GREEN}检测到 443 端口空闲，将使用标准 HTTPS 端口。${NC}"
            CADDY_PORT="443"
        else
            echo -e "${YELLOW}检测到 80/443 端口被占用。${NC}"
            
            # 默认回落端口
            DEFAULT_PORT=8080
            if check_port $DEFAULT_PORT; then
                echo -e "${RED}警告: 默认回落端口 $DEFAULT_PORT 也被占用！${NC}"
                read -p "请输入一个新的可用端口 (例如 8443): " CUSTOM_PORT
                CADDY_PORT=${CUSTOM_PORT:-$DEFAULT_PORT}
                while check_port $CADDY_PORT; do
                    read -p "${RED}端口 $CADDY_PORT 仍被占用，请重新输入: ${NC}" CADDY_PORT
                done
            else
                CADDY_PORT=$DEFAULT_PORT
            fi

            echo -e "${GREEN}将使用端口: $CADDY_PORT${NC}"
            CADDY_SITE_ADDRESS=":$CADDY_PORT"
        fi
    fi

    # 4. 多态伪装站点配置 (迁移至独立目录)
    echo -e "\n${YELLOW}[4/4] 配置多态伪装系统...${NC}"
    echo "请选择伪装站点类型 (将在 443/监听端口 展示):"
        echo "1) [推荐] 企业级 SSO 登录门户 (Enterprise Access)"
        echo "2) [可选] IT 运维监控面板 (System Monitor)"
        echo "3) [兜底] Nginx 403 错误页 (Forbidden)"
        echo "4) [自定义] Git 仓库地址 (例如 GitHub Pages)"
        echo "5) [自定义] 本地目录路径"
        read -p "请输入选项 [1-5]: " DECOY_OPT

        DECOY_PATH=""
        
        # 伪装引擎逻辑
        setup_decoy() {
            local TEMPLATE_TYPE="$1"
            local DEST_DIR="deploy/decoy"
            
            # [优化] 检测已有站点，支持更新时跳过
            if [ -f "$DEST_DIR/index.html" ]; then
                echo -e "${YELLOW}检测到已有伪装站点 (deploy/decoy)。${NC}"
                read -p "是否覆盖现有伪装站点? [y/N]: " OVERWRITE_DECOY
                if [[ ! "$OVERWRITE_DECOY" =~ ^[Yy]$ ]]; then
                    echo -e "${GREEN}已跳过伪装站点部署，保留现有文件。${NC}"
                    return 0
                fi
            fi

            echo -e "${YELLOW}正在部署伪装站点...${NC}"

            rm -rf "$DEST_DIR" && mkdir -p "$DEST_DIR"
            
            case $TEMPLATE_TYPE in
                "sso")
                    echo "Generating Enterprise SSO Portal..."
                    cat > "$DEST_DIR/index.html" <<'EOF'
<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"><title>Sign In - Enterprise Access</title><style>body{font-family:'Segoe UI',SanFrancisco,sans-serif;background:#f0f2f5;display:flex;align-items:center;justify-content:center;height:100vh;margin:0}.card{background:#fff;padding:40px;border-radius:8px;box-shadow:0 4px 12px rgba(0,0,0,0.1);width:360px;text-align:center}.logo{width:64px;height:64px;background:#0078d4;border-radius:50%;margin:0 auto 20px;display:flex;align-items:center;justify-content:center;color:#fff;font-size:32px;font-weight:bold}input{width:100%;padding:12px;margin:10px 0;border:1px solid #ddd;border-radius:4px;box-sizing:border-box}button{width:100%;padding:12px;background:#0078d4;color:#fff;border:none;border-radius:4px;cursor:pointer;font-weight:600}.error{color:#d93025;font-size:13px;margin:10px 0;display:none}</style><script>function login(){document.getElementById('err').style.display='block';setTimeout(()=>{document.getElementById('err').style.display='none'},3000)}</script></head><body><div class="card"><div class="logo">E</div><h2>Enterprise Access</h2><p style="color:#666;font-size:14px;margin-bottom:20px">Sign in with your organizational account</p><input type="email" placeholder="someone@example.com"><input type="password" placeholder="Password"><div id="err" class="error">Account temporarily locked. Please contact IT support.</div><button onclick="login()">Sign In</button><p style="margin-top:20px;font-size:12px;color:#999">&copy; 2024 Secure Identity Provider</p></div></body></html>
EOF
                    ;;
                "monitor")
                    echo "Generating IT Monitor Dashboard..."
                    cat > "$DEST_DIR/index.html" <<'EOF'
<!DOCTYPE html><html><head><title>System Monitor - Node 8a2f</title><style>body{background:#111;color:#0f0;font-family:monospace;padding:20px}canvas{border:1px solid #333;width:100%;height:300px;background:#000}.stat{display:inline-block;width:30%;margin-right:2%;border:1px solid #333;padding:10px;margin-bottom:20px}h2{margin-top:0}</style></head><body><h1>System Status: OPERATIONAL</h1><div class="stat"><h2>CPU Load</h2><div id="cpu">0%</div></div><div class="stat"><h2>Memory</h2><div id="mem">0GB / 64GB</div></div><div class="stat"><h2>Network</h2><div id="net">0.0 Mb/s</div></div><canvas id="chart"></canvas><script>const ctx=document.getElementById('chart').getContext('2d');let data=new Array(100).fill(0);function draw(){ctx.clearRect(0,0,1000,300);ctx.beginPath();ctx.moveTo(0,150);data.forEach((v,i)=>{ctx.lineTo(i*10,150-v)});ctx.strokeStyle='#0f0';ctx.stroke();document.getElementById('cpu').innerText=Math.floor(Math.random()*20)+'%';document.getElementById('net').innerText=(Math.random()*50).toFixed(1)+' Mb/s';data.push(Math.random()*50);data.shift();requestAnimationFrame(draw)}draw();</script></body></html>
EOF
                    ;;
                "nginx")
                    echo "Generating Nginx 403 Page..."
                    cat > "$DEST_DIR/index.html" <<'EOF'
<!DOCTYPE html><html><head><title>403 Forbidden</title><style>body{width:35em;margin:0 auto;font-family:Tahoma,Verdana,Arial,sans-serif}h1{font-weight:normal;color:#444}hr{border:0;border-top:1px solid #eee}</style></head><body><h1>403 Forbidden</h1><p>You don't have permission to access this resource.</p><hr><address>nginx/1.18.0 (Ubuntu) Server at localhost Port 80</address></body></html>
EOF
                    ;;
            esac
            echo -e "${GREEN}伪装站点部署完成。${NC}"
        }

        case $DECOY_OPT in
            1) setup_decoy "sso" ;;
            2) setup_decoy "monitor" ;;
            3) setup_decoy "nginx" ;;
            4) 
                read -p "请输入 Git 仓库地址: " GIT_REPO
                if [ -n "$GIT_REPO" ]; then
                    rm -rf deploy/decoy
                    git clone --depth 1 "$GIT_REPO" deploy/decoy
                    echo -e "${GREEN}自定义站点已克隆。${NC}"
                fi
                ;;
            5)
                read -p "请输入本地目录路径: " LOCAL_PATH
                if [ -d "$LOCAL_PATH" ]; then
                   DECOY_PATH="$LOCAL_PATH"
                else
                   echo -e "${RED}目录不存在，使用默认伪装。${NC}"
                   setup_decoy "default"
                fi
                ;;
            *) setup_decoy "default" ;;
        esac


        # 证书配置逻辑
        echo -e "\n${YELLOW}配置 TLS 证书...${NC}"
        echo "1) 自动生成自签名证书 (默认)"
        echo "2) 使用 deploy/certs 目录下的证书"
        echo "3) 指定宿主机绝对路径 (例如 /etc/letsencrypt/...)"
        read -p "请选择 [1-3]: " CERT_OPT

        HOST_CERT_PATH=""
        HOST_KEY_PATH=""
        CONTAINER_CERT_PATH="/certs/server.crt"
        CONTAINER_KEY_PATH="/certs/server.key"

        case $CERT_OPT in
            2)
                if [ -f "deploy/certs/server.crt" ] && [ -f "deploy/certs/server.key" ]; then
                    echo -e "${GREEN}使用 deploy/certs 证书。${NC}"
                else
                    echo -e "${RED}未找到 deploy/certs 下的证书，将回退到自签名。${NC}"
                    CERT_OPT=1
                fi
                ;;
            3)
                read -p "请输入证书文件绝对路径 (.crt/.pem): " HOST_CERT_PATH
                read -p "请输入私钥文件绝对路径 (.key): " HOST_KEY_PATH
                if [ -f "$HOST_CERT_PATH" ] && [ -f "$HOST_KEY_PATH" ]; then
                    CONTAINER_CERT_PATH="/certs/custom.crt"
                    CONTAINER_KEY_PATH="/certs/custom.key"
                    echo -e "${GREEN}将挂载外部证书: $HOST_CERT_PATH${NC}"
                else
                    echo -e "${RED}文件不存在！将回退到自签名。${NC}"
                    CERT_OPT=1
                    HOST_CERT_PATH=""
                    HOST_KEY_PATH=""
                fi
                ;;
            *) CERT_OPT=1 ;;
        esac

        if [ "$CERT_OPT" = "1" ]; then
            if [ ! -f "deploy/certs/server.crt" ]; then
                echo -e "${YELLOW}正在生成 10 年期自签名证书...${NC}"
                mkdir -p deploy/certs
                openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
                    -keyout deploy/certs/server.key \
                    -out deploy/certs/server.crt \
                    -subj "/CN=$DOMAIN" &> /dev/null
                echo -e "${GREEN}自签名证书已生成。${NC}"
            else
                echo -e "${GREEN}检测到现有自签名证书，跳过生成。${NC}"
            fi
        fi

    # 写入环境变量文件 (覆盖旧的配置)
    sed -i "/^CADDY_SITE_ADDRESS=/d" "$ENV_FILE"
    sed -i "/^CADDY_PORT=/d" "$ENV_FILE"
    sed -i "/^DECOY_PATH=/d" "$ENV_FILE"
    sed -i "/^HOST_CERT_PATH=/d" "$ENV_FILE"
    sed -i "/^HOST_KEY_PATH=/d" "$ENV_FILE"
    sed -i "/^CERT_FILE=/d" "$ENV_FILE"
    sed -i "/^KEY_FILE=/d" "$ENV_FILE"

    echo "CADDY_SITE_ADDRESS=$CADDY_SITE_ADDRESS" >> "$ENV_FILE"
    echo "CADDY_PORT=${CADDY_PORT:-8080}" >> "$ENV_FILE"
    echo "DECOY_PATH=${DECOY_PATH:-deploy/decoy}" >> "$ENV_FILE"
    # 保存绝对路径供 compose 使用
    echo "HOST_CERT_PATH=$HOST_CERT_PATH" >> "$ENV_FILE"
    echo "HOST_KEY_PATH=$HOST_KEY_PATH" >> "$ENV_FILE"
    # 保存容器内路径供 APP 使用
    echo "CERT_FILE=$CONTAINER_CERT_PATH" >> "$ENV_FILE"
    echo "KEY_FILE=$CONTAINER_KEY_PATH" >> "$ENV_FILE"
    
    # 生成 Docker Compose 配置
    echo -e "${YELLOW}生成 Docker Compose 配置...${NC}"
    
    # [Fix] 处理 Docker Compose 相对路径问题 (Must start with ./)
    # 如果 DECOY_PATH 是默认值 "deploy/decoy" (相对于项目根目录)，
    # 在 deploy/docker-compose.yml 的上下文中，它应该是 "./decoy"
    COMPOSE_DECOY_PATH="${DECOY_PATH:-deploy/decoy}"
    if [ "$COMPOSE_DECOY_PATH" == "deploy/decoy" ]; then
        COMPOSE_DECOY_PATH="./decoy"
    fi

    # 根据是否使用外部证书决定挂载卷配置
    if [ -n "$HOST_CERT_PATH" ] && [ -n "$HOST_KEY_PATH" ]; then
        VOLUME_CONFIG="
      - ${HOST_CERT_PATH}:/certs/custom.crt:ro
      - ${HOST_KEY_PATH}:/certs/custom.key:ro
      - ${COMPOSE_DECOY_PATH}:/decoy:ro"
    else
        VOLUME_CONFIG="
      - ./certs:/certs:ro
      - ${COMPOSE_DECOY_PATH}:/decoy:ro"
    fi

    cat > deploy/docker-compose.yml <<EOF
services:
  aether-gateway-core:
    image: ghcr.io/coolapijust/aether-realist:main
    container_name: aether-gateway-core
    restart: always
    network_mode: "host"
    sysctls:
      - net.core.rmem_max=2500000
      - net.core.wmem_max=2500000
    volumes:
      - /etc/localtime:/etc/localtime:ro
      - /etc/timezone:/etc/timezone:ro$VOLUME_CONFIG
    environment:
      - PSK=\${PSK}
      - LISTEN_ADDR=:\${CADDY_PORT}
      - SSL_CERT_FILE=\${CERT_FILE:-/certs/server.crt}
      - SSL_KEY_FILE=\${KEY_FILE:-/certs/server.key}
      - DECOY_ROOT=/decoy
      - WINDOW_PROFILE=\${WINDOW_PROFILE:-normal}
      - RECORD_PAYLOAD_BYTES=\${RECORD_PAYLOAD_BYTES:-16384}
    cap_add:
      - NET_ADMIN
EOF

    cleanup_legacy() {
        echo -e "\n${YELLOW}正在准备无缝升级 (环境检查)...${NC}"
        # 仅清理真的已经废弃的组件 (早于 V5 的版本)
        if docker ps -a --format '{{.Names}}' | grep -q "aether-cert-manager"; then
            echo -e "${YELLOW}清理旧版证书管理器...${NC}"
            docker stop aether-cert-manager >/dev/null 2>&1 || true
            docker rm aether-cert-manager >/dev/null 2>&1 || true
        fi
        
        # 不要手动删除 aether-gateway-core，让 docker compose up -d 处理“覆盖更新”
        # 这样可以减少停机时间并避免某些 Race Condition
        
        # 清理旧的 Caddy 数据卷
        if docker volume ls -q | grep -q "deploy_caddy_data"; then
            docker volume rm deploy_caddy_data >/dev/null 2>&1 || true
        fi
        
        # 清理旧的 Caddyfile 目录/文件
        [ -e "deploy/Caddyfile" ] && rm -rf "deploy/Caddyfile"
        echo -e "${GREEN}环境检查完成。${NC}"
    }

    cleanup_legacy

    # 4.5 内核优化 (可选)
    echo -e "\n${YELLOW}[5/5] 检查内核参数优化 (UDP 缓存)...${NC}"
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

    # 智能端口占用解决 (针对优雅更新的补充)
    resolve_port_conflict() {
        local TARGET_PORT=$1
        # host 网络模式更新时，先停掉旧核心容器，避免同端口抢占失败
        if docker ps -a --format '{{.Names}}' | grep -qx "aether-gateway-core"; then
            if docker ps --format '{{.Names}}' | grep -qx "aether-gateway-core"; then
                echo -e "${YELLOW}检测到旧核心容器正在运行，先执行平滑下线以释放端口...${NC}"
                docker stop aether-gateway-core >/dev/null 2>&1 || true
            fi
            docker rm aether-gateway-core >/dev/null 2>&1 || true
        fi

        if check_port "$TARGET_PORT"; then
            echo -e "${YELLOW}检测到端口 $TARGET_PORT 被占用。正在分析进程...${NC}"
            local PID_INFO=$(lsof -i :$TARGET_PORT -t 2>/dev/null || fuser $TARGET_PORT/tcp 2>/dev/null | awk '{print $NF}')
            
            if [ -n "$PID_INFO" ]; then
                local PROCESS_NAME=$(ps -p $PID_INFO -o comm= 2>/dev/null || echo "未知进程")
                echo -e "${RED}警告: 端口 $TARGET_PORT 正被进程 [${PROCESS_NAME}] (PID: $PID_INFO) 占用。${NC}"
                
                # 如果是本项目容器，提示覆盖，否则警示
                if [[ "$PROCESS_NAME" == *"docker"* ]]; then
                    echo -e "${GREEN}提示: 这是一个容器进程，Docker Compose 将会在接下来的步骤中尝试自动替换它。${NC}"
                else
                    read -p "是否强制结束此进程并抢占端口? [y/N]: " KILL_CONFIRM
                    if [[ "$KILL_CONFIRM" =~ ^[Yy]$ ]]; then
                        echo -e "${YELLOW}正在释放端口...${NC}"
                        kill -9 $PID_INFO 2>/dev/null || sudo kill -9 $PID_INFO 2>/dev/null
                    else
                        echo -e "${RED}操作取消，启动可能会失败。${NC}"
                    fi
                fi
            fi
        fi
    }

    resolve_port_conflict "${CADDY_PORT:-443}"

    echo -e "\n${YELLOW}正在获取最新镜像 (V5.1 Optimized)...${NC}"
    docker compose -f deploy/docker-compose.yml pull
    
    echo -e "\n${YELLOW}正在启动服务 (覆盖式更新)...${NC}"
    docker compose -f deploy/docker-compose.yml up -d --remove-orphans
    echo -e "${GREEN}服务同步/升级完成！已实现无缝切换。${NC}"
}

show_status() {
    echo -e "\n${YELLOW}=== Aether-Realist 服务状态 ===${NC}"
    if [ -f "deploy/docker-compose.yml" ]; then
        docker compose -f deploy/docker-compose.yml ps
        
        echo -e "\n${YELLOW}--- 健康检查 (API 响应头) ---${NC}"
        # PSK=$(grep "^PSK=" "deploy/.env" | cut -d'=' -f2) # PSK 不再是 /health 必需的
        PORT=$(grep "^CADDY_PORT=" "deploy/.env" | cut -d'=' -f2 | tr -d ':')
        if curl -ksI "https://localhost:${PORT:-8080}/health" | grep -q "200 OK"; then
             echo -e "${GREEN}[OK] Aether Backend 响应正常 (Port ${PORT:-8080})${NC}"
        else
             echo -e "${RED}[WARN] 无法从本地回环确认 API 连通性,请检查 logs${NC}"
             echo -e "${YELLOW}提示: 如果您使用了非 443 端口,请确保防火墙已放行该端口。${NC}"
        fi
        
        # 自动检查时间同步
        check_time_sync

        # 检查防火墙与 UDP 监听
        check_firewall_and_udp
    else
        echo -e "${RED}未找到 docker-compose.yml 文件${NC}"
    fi
}

check_firewall_and_udp() {
    echo -e "\n${YELLOW}--- 网络与防火墙检查 (QUIC/HTTP3) ---${NC}"
    PORT=$(grep "^CADDY_PORT=" "deploy/.env" | cut -d'=' -f2 | tr -d ':')
    PORT=${PORT:-8080}

    # 1. 检查 UDP 监听
    if command -v ss >/dev/null; then
        if ss -uln | grep -q ":$PORT "; then
            echo -e "${GREEN}[OK] UDP 端口 $PORT 监听正常 (用于 HTTP/3)${NC}"
        else
            echo -e "${RED}[ERR] 未检测到 UDP 端口 $PORT 监听！${NC}"
        fi
    elif command -v netstat >/dev/null; then
        if netstat -uln | grep -q ":$PORT "; then
            echo -e "${GREEN}[OK] UDP 端口 $PORT 监听正常 (用于 HTTP/3)${NC}"
        else
            echo -e "${RED}[ERR] 未检测到 UDP 端口 $PORT 监听！${NC}"
        fi
    else
        echo -e "${YELLOW}[SKIP] 未找到 ss 或 netstat，跳过 UDP 监听检查。${NC}"
    fi

    # 2. 简易防火墙提示
    if command -v ufw >/dev/null; then
        if ufw status | grep -q "Status: active"; then
            echo -e "${YELLOW}[WARN] UFW 防火墙已开启。请确保放行以下端口：${NC}"
            echo -e "      UDP: ${GREEN}sudo ufw allow $PORT/udp${NC} (WebTransport 核心)"
            echo -e "      TCP: ${GREEN}sudo ufw allow $PORT/tcp${NC} (HTTPS 访问)"
            if [ "$PORT" = "443" ]; then
                echo -e "      TCP: ${GREEN}sudo ufw allow 80/tcp${NC} (HTTP 自动跳转)"
            fi
        fi
    fi
    echo -e "${YELLOW}提示: 客户端无法连接通常是因为 UDP 端口被云厂商防火墙(安全组)拦截。${NC}"
}

check_time_sync() {
    echo -e "\n${YELLOW}--- 时间同步检查 ---${NC}"
    
    # 获取宿主机时间戳
    HOST_TIME=$(date +%s)
    
    # 获取容器时间戳（如果容器未启动则跳过）
    CONTAINER_TIME=$(docker exec aether-gateway-core date +%s 2>/dev/null || echo "0")
    
    if [ "$CONTAINER_TIME" = "0" ]; then
        echo -e "${YELLOW}容器未启动或无法访问，跳过时间检查。${NC}"
        return
    fi
    
    # 计算时间偏差（绝对值）
    TIME_DIFF=$((HOST_TIME - CONTAINER_TIME))
    TIME_DIFF=${TIME_DIFF#-}  # 移除负号取绝对值
    
    if [ $TIME_DIFF -gt 5 ]; then
        echo -e "${RED}[WARN] 容器时间偏差: ${TIME_DIFF}秒${NC}"
        echo -e "${YELLOW}建议操作:${NC}"
        echo -e "  1. 同步宿主机时间: ${GREEN}sudo ntpdate -u pool.ntp.org${NC}"
        echo -e "  2. 重启容器: ${GREEN}docker restart aether-gateway-core${NC}"
    else
        echo -e "${GREEN}[OK] 容器时间同步正常 (偏差 ${TIME_DIFF}秒)${NC}"
    fi
}

view_logs() {
    echo -e "\n${YELLOW}查看 Aether 核心服务日志...${NC}"
    docker compose -f deploy/docker-compose.yml logs -f --tail 100
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
    echo "3) 修改 监听端口 (PORT)"
    read -p "请输入 [1/2/3]: " CFG_OPT
    if [ "$CFG_OPT" = "1" ]; then
        read -p "新 PSK: " NEW_PSK
        [ -n "$NEW_PSK" ] && sed -i "s/^PSK=.*/PSK='$NEW_PSK'/" "$ENV_FILE"
    elif [ "$CFG_OPT" = "2" ]; then
        read -p "新域名: " NEW_DOM
        [ -n "$NEW_DOM" ] && sed -i "s/^DOMAIN=.*/DOMAIN='$NEW_DOM'/" "$ENV_FILE"
    elif [ "$CFG_OPT" = "3" ]; then
        read -p "新端口 (如 443 或 8443): " NEW_PORT
        if [ -n "$NEW_PORT" ]; then
            sed -i "/^CADDY_PORT=/d" "$ENV_FILE"
            echo "CADDY_PORT=$NEW_PORT" >> "$ENV_FILE"
            # 同步更新 CADDY_SITE_ADDRESS
            sed -i "/^CADDY_SITE_ADDRESS=/d" "$ENV_FILE"
            if [ "$NEW_PORT" = "443" ]; then
                echo "CADDY_SITE_ADDRESS=" >> "$ENV_FILE"
            else
                echo "CADDY_SITE_ADDRESS=:$NEW_PORT" >> "$ENV_FILE"
            fi
        fi
    fi
    echo -e "${GREEN}配置已更新。${NC}"
    read -p "是否同步重启容器以应用配置? [y/N]: " RESTART_CONFIRM
    if [[ "$RESTART_CONFIRM" =~ ^[Yy]$ ]]; then
        docker compose -f deploy/docker-compose.yml up -d
    fi
}

stop_service() {
    echo -e "\n${YELLOW}正在暂停服务...${NC}"
    if [ -f "deploy/docker-compose.yml" ]; then
        docker compose -f deploy/docker-compose.yml stop
        echo -e "${GREEN}服务已暂停。${NC}"
    else
        echo -e "${RED}错误: 未找到部署文件。${NC}"
    fi
}

remove_service() {
    echo -e "\n${YELLOW}正在准备卸载 Aether-Realist 服务...${NC}"
    
    # 1. 基础清理：停止并删除容器 (保留数据)
    if [ -f "deploy/docker-compose.yml" ]; then
        docker compose -f deploy/docker-compose.yml down -v
        echo -e "${GREEN}[OK] Docker 容器、网络与临时卷已清理。${NC}"
        echo -e "${YELLOW}提示: 您的配置文件 (.env) 和证书目录 (certs/) 仍然保留，方便下次快速重新部署。${NC}"
    else
         echo -e "${YELLOW}[SKIP] 未找到 docker-compose.yml，跳过容器清理。${NC}"
    fi

    # 2. 彻底清除选项 (可选)
    echo -e "\n${RED}!!! 危险操作警告 !!!${NC}"
    read -p "是否【彻底粉碎】所有数据? (包括域名配置、PSK密钥、SSL证书)? [y/N]: " DESTROY_ALL
    if [[ "$DESTROY_ALL" =~ ^[Yy]$ ]]; then
        echo -e "${RED}正在执行深度清理...${NC}"
        rm -rf deploy/certs deploy/decoy deploy/.env
        # 也尝试删除 deploy 目录本身（如果为空）
        rmdir deploy 2>/dev/null
        echo -e "${GREEN}[OK] 所有配置与数据已永久删除。环境已重置为初始状态。${NC}"
    else
        echo -e "${GREEN}[INFO] 已保留配置文件，您可以随时运行 ./deploy.sh 恢复服务。${NC}"
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
