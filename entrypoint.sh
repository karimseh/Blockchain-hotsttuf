#!/bin/bash
set -e

SHARED_DIR="/shared"
PEER_ID_FILE="${SHARED_DIR}/bootstrap_peer_id"

if [ "$ROLE" = "bootstrap" ]; then
    echo "Starting bootstrap node on port ${PORT}..."
    mkdir -p "$SHARED_DIR"
    WRITE_PEER_ID="$PEER_ID_FILE" exec ./sharehr --port "${PORT}" --rendezvous sharehr
else
    echo "Waiting for bootstrap peer ID..."
    while [ ! -f "$PEER_ID_FILE" ] || [ ! -s "$PEER_ID_FILE" ]; do
        sleep 1
    done
    BOOTSTRAP_ID=$(cat "$PEER_ID_FILE")
    BOOTSTRAP_ADDR="/dns4/bootstrap/tcp/${BOOTSTRAP_PORT}/p2p/${BOOTSTRAP_ID}"
    echo "Connecting to bootstrap: ${BOOTSTRAP_ADDR}"
    exec ./sharehr --port "${PORT}" --rendezvous sharehr --peer "${BOOTSTRAP_ADDR}"
fi
