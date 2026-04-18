# syntax=docker/dockerfile:1.7
#
# ContractPro Frontend — production image. Реализует §13.1 high-architecture.
#
# Multi-stage:
#   1. build    — node:20-alpine; ставит deps, запускает typecheck/lint/test:ci/build.
#                 Результат: /app/dist (статика Vite).
#   2. runtime  — nginx:1.27-alpine; отдаёт SPA + reverse-proxy API/SSE на
#                 Orchestrator (см. nginx.conf + §13.2). Runtime-env инжектится
#                 entrypoint.sh в /config.js (§13.5).
#
# Build-args копируют ЗНАЧЕНИЕ VITE_* в bundle (compile-time). Они ДОЛЖНЫ
# оставаться безопасными для публичной раздачи — приватные секреты сюда не
# попадают, для динамических значений используйте runtime-env через
# docker/entrypoint.sh (window.__ENV__).
#
# Build-context: Frontend/ (sibling-директория ApiBackendOrchestrator/ не
# нужна — src/shared/api/openapi.d.ts уже в коммите, prepare-hook
# `npm run gen:api` пропускается через --ignore-scripts).

# ---- build stage -----------------------------------------------------------
FROM node:20-alpine AS build
WORKDIR /app

# Сначала только манифесты — даёт Docker layer-cache для `npm ci`.
COPY package.json package-lock.json ./

# --ignore-scripts: prepare-hook (`npm run gen:api`) ссылается на файл вне
# build-context (`../ApiBackendOrchestrator/architecture/api-specification.yaml`).
# В коммите уже лежит сгенерированный src/shared/api/openapi.d.ts, его
# актуальность проверяется отдельной CI-задачей `gen:api:check` (FE-TASK-010).
RUN npm ci --no-audit --no-fund --ignore-scripts

COPY . .

ARG VITE_API_BASE_URL=/api/v1
ARG VITE_SENTRY_DSN=
ARG VITE_OTEL_ENDPOINT=
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL \
    VITE_SENTRY_DSN=$VITE_SENTRY_DSN \
    VITE_OTEL_ENDPOINT=$VITE_OTEL_ENDPOINT

RUN npm run typecheck \
 && npm run lint \
 && npm run test:ci \
 && npm run build

# ---- runtime stage ---------------------------------------------------------
FROM nginx:1.27-alpine AS runtime

# Статика + конфиги nginx (snippets подключаются из default.conf через include).
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY docker/security-headers.conf /etc/nginx/snippets/security-headers.conf
COPY docker/entrypoint.sh /entrypoint.sh

# Non-root hardening:
#   - pid переносится в /tmp (default /var/run/nginx.pid недоступен nginx user);
#   - server_tokens off — скрыть версию nginx в заголовках и error-страницах;
#   - mkdir snippets ДО chown — иначе include в default.conf не находит файл;
#   - chown runtime-директорий: /var/cache/nginx (proxy/fastcgi/scgi/uwsgi_temp +
#     client_body_temp) и /var/lib/nginx (используется alpine-дефолтом для
#     client_body_temp на 25MB-аплоадах; без chown — EACCES при upload);
#   - статика html тоже под nginx user — entrypoint.sh перезаписывает config.js.
RUN chmod +x /entrypoint.sh \
 && sed -i 's|^pid .*|pid /tmp/nginx.pid;|' /etc/nginx/nginx.conf \
 && sed -i '/^http {/a \    server_tokens off;' /etc/nginx/nginx.conf \
 # Fail-fast: если апгрейд base-image изменит форматирование `http {` (напр.
 # пробелы перед фигурной скобкой), sed тихо не вставит директиву. grep по
 # главному конфигу подтверждает, что строка присутствует. `nginx -t` тут не
 # используется — в build-time DNS для upstream `orchestrator` не резолвится.
 && grep -q '^    server_tokens off;' /etc/nginx/nginx.conf \
 && touch /tmp/nginx.pid \
 && chown -R nginx:nginx \
      /usr/share/nginx/html \
      /var/cache/nginx \
      /var/lib/nginx \
      /var/log/nginx \
      /etc/nginx/snippets \
      /tmp/nginx.pid

EXPOSE 8080
USER nginx

# HEALTHCHECK: index.html доступен локально 200 — контейнер жив. Nginx-wget
# (busybox) идёт в base-image alpine. Upstream /api/ проверяется отдельной
# health-пробой в compose (Orchestrator).
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null --tries=1 --timeout=2 http://127.0.0.1:8080/ || exit 1

ENTRYPOINT ["/entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
