#!/bin/sh
# ContractPro Frontend — runtime entrypoint (§13.5 high-architecture).
#
# Инжектирует window.__ENV__ в /usr/share/nginx/html/config.js из ENV-переменных.
# Тот же образ гоняется в stage/prod с разными значениями — build-time VITE_*
# зафиксированы в bundle, а всё, что должно меняться per-environment, приходит
# сюда.
#
# Значения читаются как getRuntimeEnv() в src/shared/config/runtime-env.ts.
# Пустые строки (`""`) — допустимы: это явный «не задано», SENTRY/OTEL SDK
# сами делают no-op при отсутствии DSN/endpoint.
set -eu

CONFIG_FILE="/usr/share/nginx/html/config.js"

# JSON/JS-escape: значение инлайнится в "..."-строку на клиенте. Экранируем
# обратный слэш (первым), двойную кавычку, управляющие символы, U+2028/U+2029
# (обрывают строковый литерал в до-ES2019 парсерах) и `<` (защита на случай
# inline-варианта <script>: '</script>' в значении сломает парсинг HTML).
# Это defense-in-depth — env-переменные приходят из доверенного compose, но
# один пропущенный символ в DSN превратится в XSS.
js_escape() {
  # shellcheck disable=SC2039
  printf '%s' "$1" \
    | sed 's/\\/\\\\/g; s/"/\\"/g; s/</\\x3c/g; s/\r//g' \
    | sed $'s/\xe2\x80\xa8/\\\\u2028/g; s/\xe2\x80\xa9/\\\\u2029/g' \
    | tr '\n' ' '
}

API_BASE_URL="$(js_escape "${VITE_API_BASE_URL:-/api/v1}")"
SENTRY_DSN="$(js_escape "${VITE_SENTRY_DSN:-}")"
OTEL_ENDPOINT="$(js_escape "${VITE_OTEL_ENDPOINT:-}")"

cat > "${CONFIG_FILE}" <<EOF
window.__ENV__ = {
  API_BASE_URL: "${API_BASE_URL}",
  SENTRY_DSN: "${SENTRY_DSN}",
  OTEL_ENDPOINT: "${OTEL_ENDPOINT}"
};
EOF

exec "$@"
