#!/bin/bash

SESSION_NAME="uepb-bot"
SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

"$SCRIPT_DIR/stop-bot.sh" || true

echo "Updating..."
DEFAULT_BRANCH="$(git -C "$REPO_ROOT" symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null | sed 's|^origin/||')"
if [ -z "$DEFAULT_BRANCH" ]; then
  DEFAULT_BRANCH="master"
fi

git -C "$REPO_ROOT" fetch origin --prune
git -C "$REPO_ROOT" reset --hard "origin/$DEFAULT_BRANCH"
git -C "$REPO_ROOT" clean -fd

echo "Building..."
if ! (cd "$REPO_ROOT" && go build -o uepb-bot .); then
    echo "Error!"
    exit 1
fi

"$SCRIPT_DIR/start-bot.sh"
