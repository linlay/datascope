#!/bin/sh
set -eu

umask 077

mkdir -p \
  "$(dirname "${MANAGEMENT_DB_PATH}")" \
  "$(dirname "${AUDIT_DB_PATH}")" \
  "$(dirname "${SQLITE_PATH}")" \
  "$(dirname "${OAUTH_KEY_PATH}")" \
  "${DATABASE_CONFIG_DIR}"

if [ ! -e "${SQLITE_PATH}" ]; then
  if [ "${INITIALIZE_DEMO_DATA:-true}" = "true" ]; then
    echo "Initializing demo database at ${SQLITE_PATH}"
    gen-data -db "${SQLITE_PATH}" -schema /app/sql/schema.sqlite.sql -dictionary ""
  else
    echo "Database does not exist: ${SQLITE_PATH}" >&2
    exit 1
  fi
fi

admin-api &
admin_pid=$!
mcp-server &
mcp_pid=$!
nginx -g 'daemon off;' &
nginx_pid=$!
pids="${admin_pid} ${mcp_pid} ${nginx_pid}"

stop_services() {
  trap - EXIT TERM INT
  kill -TERM ${pids} 2>/dev/null || true
  wait ${pids} 2>/dev/null || true
}
trap stop_services EXIT TERM INT

while :; do
  for pid in ${pids}; do
    if ! kill -0 "${pid}" 2>/dev/null; then
      if wait "${pid}"; then
        status=0
      else
        status=$?
      fi
      echo "A DataScope service exited (pid=${pid}, status=${status}); stopping container" >&2
      exit "${status}"
    fi
  done
  sleep 1
done
