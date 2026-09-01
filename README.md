# Mural

A digital signage player that renders directly to Linux DRM/KMS — no GUI toolkit, no X11, no Wayland. Cycles through images in a content directory, with a daily schedule for display on/off times and HDMI CEC control. Optimized for Raspberry Pi.

## Quick Install (Raspberry Pi)

```bash
curl -fsSL https://raw.githubusercontent.com/jassg-to/mural/main/install.sh | bash
```

This downloads the latest pre-built binary, installs dependencies, and sets up the display environment. The installer also offers to configure autologin for kiosk mode and Samba file sharing so you can manage content from any computer on your network. See [docs/INSTALL.md](docs/INSTALL.md) for the full step-by-step guide starting from hardware setup.

## Prerequisites

- Go 1.25+ (only needed if building from source)
- `cec-client` on the PATH for HDMI CEC control (optional; no-op if absent)
- Linux — Mural talks to DRM/KMS and evdev directly, which have no Windows equivalent

## Usage

```bash
go build .
mkdir -p content
# Place your .jpg / .jpeg / .png images in content/
# Create a schedule (see below)
./mural
```

```bash
./mural /var/mural
```

The optional argument specifies the content directory (default: `content`).

### Controls

| Key | Action |
|-----|--------|
| Right arrow | Next slide |
| Left arrow | Previous slide |
| Home | Jump to the first slide |
| Delete | Sleep the display (black screen, CEC standby) |
| Esc | Quit |

Binding is by keycode, not by device, so a USB keyboard and the physical three-key remote (Left/Right/Home) behave the same way. When the display is paused (scheduled off-time or Delete), any other recognized key wakes it immediately.

## Configuration

Create a `config.toml` inside your content directory:

```toml
[slideshow]
interval = "30s"       # time between slides (e.g. "30s", "1m", "2m30s")
thumb_width = 80       # thumbnail width in pixels for keyboard navigation

[schedule]
reload_time = "01:00"  # reload this file daily at this time (HH:MM; default: "01:00")

[schedule.monday]
all = [ "08:00-12:00", "13:30-22:00" ]

[schedule.tuesday]
all = [ "08:00-12:00", "13:30-22:00" ]

[schedule.saturday]
all = [ "10:00-18:00" ]
last = [ "18:00-22:00" ]  # extra hours on the last Saturday of the month

# sunday: off all day (no section needed)
```

- Each day has a list of `"HH:MM-HH:MM"` windows (local time).
- Day sections support occurrence fields (`first`, `second`, `third`, `fourth`, `last`) that match an Nth weekday of the month and add extra on-time (union with `all` windows).
- Overlapping windows are merged automatically.
- The config file is re-read from disk daily at `reload_time` — edit it without restarting.
- At each turn-on event, the content directory is rescanned for new or changed images.

## USB Stick Content Updates

On Linux (e.g. the Pi), plugging in a USB stick updates the sign's content without a restart, a network connection, or shell access:

- The stick must carry a `config.toml` at its top level, or it is ignored entirely — this is the marker that tells Mural the stick is meant for it, so a random flash drive, camera card, or phone doesn't silently replace the sign's content.
- If accepted, the stick's images are copied into the content directory and become the entire rotation; its `config.toml` is adopted as the running schedule and slideshow settings, live. The display wakes immediately as visual confirmation, even outside scheduled hours.
- The images and config the stick replaces are kept, not deleted, in `content/previous/` — the single most recently displaced set, overwritten by the next accepted stick with images.
- The stick is never written to, and can be removed once the sign has picked up the new content; it keeps playing indefinitely, including across reboots.
- A stick with only a `config.toml` and no images updates settings without touching the current rotation.

Mount detection reads `-media-dir` (default `/media/mural`) via `/proc/self/mountinfo`; Mural itself never mounts anything — `install.sh` sets up a udev rule that automounts USB volumes read-only under this path. An empty `-media-dir` disables the feature. See [docs/INSTALL.md](docs/INSTALL.md#update-content-by-usb-stick) for the full workflow, recovery steps, and the security note on physical USB port access.

## How It Works

- Images are loaded from the content directory in filename order. Only changed files are re-decoded on reload.
- Tiny thumbnails (default 80px, configurable via `thumb_width` in config) are pre-loaded for instant navigation; full images are decoded on demand and scaled to fit the display's native resolution, then letterboxed onto a black frame.
- A generation counter prevents stale background loads from overwriting a newer slide.
- Mural runs a single-goroutine event loop: key events, the advance timer, background decode results, and pause/reload requests are all handled serially, with no locks.
- Display output is direct DRM/KMS — double-buffered dumb buffers, page-flip — via a hand-written ioctl layer, since no mature Go DRM library exists. Input is read directly from the kernel via evdev.
- The scheduler sleeps until the next event each day; CEC commands run via `cec-client -s`.

> Video playback is intentionally out of scope for this Go codebase. A future "Kodi launcher mode" is the planned path for video — mural handing off to an externally-managed Kodi process rather than decoding video itself — not yet implemented.

## Development

```bash
go run . -headless
```

> Mural needs a free VT and DRM/evdev device permissions, so it can't run inside a desktop session. `-headless` swaps in a renderer that PNG-dumps each composited frame to disk instead of driving real display hardware, so you can develop and test without a Pi and a monitor in front of you.
