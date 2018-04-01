# Fix environment
export DISPLAY=:0

# Turn off screen
echo 'standby 0' | cec-client RPI -s -d 1
killall chromium-browser
