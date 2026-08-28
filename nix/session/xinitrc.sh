#!/bin/sh
set -eu

xset s off
xset -dpms
xset s noblank

ratpoison -f "$MURAL_RATPOISONRC" &
unclutter -idle 0 -root &

exec "$MURAL_BIN" "$MURAL_CONTENT_DIR"
