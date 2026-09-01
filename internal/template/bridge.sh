#!/usr/bin/env bash
set -euo pipefail

# Usage: bridge.sh <name> <port> <host_port> [env_var]

NAME="${1:?bridge name required}"
PORT="${2:?listen port required}"
HOST_PORT="${3:?host port required}"
ENV_VAR="${4:-}"

HOSTS_FILE="${LID_HOSTS_FILE:-/etc/hosts}"
ENV_FILE="${CLAUDE_ENV_FILE:-/etc/sandbox-persistent.sh}"
LOG_FILE="${LID_BRIDGE_LOG:-/tmp/lid-bridge.log}"

log() {
    printf '%s %s\n' "$(date -Is 2>/dev/null || true)" "$1" >>"$LOG_FILE" 2>/dev/null || true
}

if pgrep -f "socat.*TCP-LISTEN:${PORT}," >/dev/null 2>&1; then
    log "= bridge already up ${NAME}:${PORT} -> host:${HOST_PORT}"
elif ! command -v socat >/dev/null 2>&1; then
    log "! socat is not installed; add it to 'packages:' in your lid config"
    exit 1
elif setsid -f socat "TCP-LISTEN:${PORT},bind=127.0.0.1,reuseaddr,fork" \
    "TCP:host.docker.internal:${HOST_PORT}" </dev/null >/dev/null 2>&1; then
    log "+ bridge up ${NAME}:${PORT} -> host:${HOST_PORT}"
else
    log "! bridge failed ${NAME}:${PORT} — is localhost:${HOST_PORT} allowed on the host?"
    exit 1
fi

if grep -qE "^[^#]*[[:space:]]${NAME}([[:space:]]|\$)" "$HOSTS_FILE" 2>/dev/null; then
    log "= ${HOSTS_FILE}: ${NAME} -> 127.0.0.1 (already there)"
elif echo "127.0.0.1  ${NAME}" | sudo tee -a "$HOSTS_FILE" >/dev/null 2>&1; then
    log "+ ${HOSTS_FILE}: ${NAME} -> 127.0.0.1"
elif [ -z "$ENV_VAR" ]; then
    log "! ${HOSTS_FILE} is read-only and no env fallback for ${NAME}; use 127.0.0.1:${PORT}"
elif grep -q "LID_BRIDGE_${ENV_VAR}" "$ENV_FILE" 2>/dev/null; then
    log "= ${ENV_VAR} override already in ${ENV_FILE}"
else
    sudo tee -a "$ENV_FILE" >/dev/null <<EOF

if [ -z "\${LID_BRIDGE_${ENV_VAR}:-}" ]; then
  export ${ENV_VAR}=127.0.0.1
  export LID_BRIDGE_${ENV_VAR}=1
fi
EOF
    log "+ ${ENV_VAR}=127.0.0.1 via ${ENV_FILE} (${HOSTS_FILE} is read-only)"
fi
