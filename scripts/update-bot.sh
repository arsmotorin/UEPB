#!/bin/bash

SESSION_NAME="uepb-bot"

echo "Stopping..."
./stop-bot.sh

echo "Updating..."
git stash
git pull origin main
git stash pop

echo "Building..."
go build -o uepb-bot .

if [ $? -ne 0 ]; then
    echo "Error!"
    exit 1
fi

echo "Starting..."
./start-bot.sh

echo "Update complete."
