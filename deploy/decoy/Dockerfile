FROM nginx:alpine
RUN echo "<h1>Aether-Realist Documentation</h1><p>High-performance WebTransport Gateway.</p>" > /usr/share/nginx/html/index.html
EXPOSE 8081
# Custom port for host mode compliance
RUN sed -i 's/listen\(.*\)80;/listen 8081;/' /etc/nginx/conf.d/default.conf
