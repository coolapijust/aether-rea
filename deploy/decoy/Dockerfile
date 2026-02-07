# 第一阶段：构建环境
FROM node:18-alpine AS builder

WORKDIR /app

# 初始化一个简单的 VitePress 文档站 (最轻量)
RUN npx -y create-vitepress-site --docs . --title "Aether Cloud Native" --description "Network Telemetry Docs"

# 注入一份正经的文档内容 (模拟网络遥测)
RUN echo '# Cloud Native Network Telemetry\n\nWelcome to the official developer documentation for Aether Network Telemetry.\n\n## Overview\nThis project focuses on high-performance, low-latency stream processing for next-generation cloud infrastructure.' > docs/index.md

# 构建静态资源
RUN npm install && npm run docs:build

# 第二阶段：运行环境 (Nginx)
FROM nginx:alpine

# 复制构建产物
COPY --from=builder /app/docs/.vitepress/dist /usr/share/nginx/html

# 复制 Nginx 配置
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
