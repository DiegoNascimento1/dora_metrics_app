# syntax=docker/dockerfile:1.7

# ---------- Builder ----------
FROM node:24-alpine AS builder
WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm ci --no-audit --no-fund

COPY . .
RUN npm run build

# ---------- Runtime (hardened) ----------
# nginxinc/nginx-unprivileged — imagem oficial que roda como uid 101 (nginx)
# sem precisar de CAP_NET_BIND_SERVICE; escuta em :8080 por default. Bom
# trade-off entre tamanho (slim baseado em alpine) e ausência de root.
FROM nginxinc/nginx-unprivileged:1.27-alpine AS runtime
USER root
COPY --from=builder --chown=nginx:nginx /app/dist/dora-metrics/browser /usr/share/nginx/html
COPY --chown=nginx:nginx nginx.conf /etc/nginx/conf.d/default.conf
# Permissões mínimas pra nginx escrever cache/tmp como uid 101.
RUN chmod -R g=u /var/cache/nginx /var/run /etc/nginx/conf.d
USER 101
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -qO- http://127.0.0.1:8080/index.html >/dev/null || exit 1
CMD ["nginx", "-g", "daemon off;"]
