#!/bin/bash

SESSION_NAME="uepb-bot"
SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

echo "Stopping..."
"$SCRIPT_DIR/stop-bot.sh" || true

echo "Updating..."
git -C "$REPO_ROOT" pull

echo "Building..."
if ! (cd "$REPO_ROOT" && go build -o uepb-bot .); then
    echo "Error!"
    exit 1
fi

echo "Starting..."
"$SCRIPT_DIR/start-bot.sh"

echo "Update complete."
