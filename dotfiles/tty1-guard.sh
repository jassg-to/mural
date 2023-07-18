#!/usr/bin/env bash

if [ "$(tty)" != "/dev/tty1" ]; then
    exit 1
fi

cd "$(dirname "$0")"/..

git reset --hard HEAD
git pull --ff-only

PYTHONPATH=. startx -- -nocursor

cat <<EOF
    ****************************************************************
    ****************************************************************
    ***                                                          ***
    ***  Waiting 10 seconds before restarting...                 ***
    ***  Press Ctrl+C to enter the system shell.                 ***
    ***                                                          ***
    ****************************************************************
    ****************************************************************
EOF
sleep 10
