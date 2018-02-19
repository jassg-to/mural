# Fix environment
export DISPLAY=:0

# Turn HDMI off
/opt/vc/bin/tvservice -o

# Re-enable screensaver
xset s blank
xset s on
xset +dpms

# Kill kiosk things
killall unclutter
killall /usr/lib/chromium-browser/chromium-browser
