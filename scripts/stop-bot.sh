#!/bin/bash

SESSION_NAME="uepb-bot"

if ! screen -list | grep -q "$SESSION_NAME"; then
    echo "Bot is not running."
    exit 1
fi

echo "Stopping..."
screen -S "$SESSION_NAME" -X stuff "^C"
sleep 2

if screen -list | grep -q "$SESSION_NAME"; then
    screen -S "$SESSION_NAME" -X quit
fi

pkill -f "uepb-bot" 2>/dev/null

echo "Bot stopped."
