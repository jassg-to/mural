DELAY_SECONDS=30
URL='https://docs.google.com/presentation/d/e/2PACX-1vQ7LGi9WeOpcex-d2VXgQeT4pfHqd9h3YXWkDr9iReuKIIQMzPNBVZ5-J5xEh6wqvyO_aK858H4nQto/pub?start=true&loop=true&delayms='$DELAY_SECONDS'000'

# Disable screensaver
xset s noblank
xset s off
xset -dpms

# Hide mouse cursor
unclutter &

# Launch browser
chromium-browser --kiosk --incognito "$URL" &
