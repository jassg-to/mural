# Fix environment
export DISPLAY=:0

# Disable screensaver
xset s noblank
xset s off
xset -dpms

# Hide mouse cursor
unclutter &

# Launch browser
DIR=$( dirname ${BASH_SOURCE[0]} )
$DIR/turn-on.sh
