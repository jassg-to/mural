#!/usr/bin/env bash

[ "$(tty)" != "/dev/tty1" ] && exit 1

startx

cat <<'EOF'
    ************************************************************
    ***  Waiting 10 seconds before restarting...             ***
    ***  Press Ctrl+C to enter the system shell.             ***
    ************************************************************
EOF
sleep 10
