# Mural

A digital signage player built with [Fyne](https://fyne.io/). Cycles through images (and MP4 videos) in a content directory, with a daily schedule for display on/off times and HDMI CEC control. Optimized for Raspberry Pi.

## Quick Install (Raspberry Pi)

```bash
curl -fsSL https://raw.githubusercontent.com/jassg-to/mural/main/install.sh | bash
```

This downloads the latest pre-built binary, installs dependencies, and sets up the display environment. The installer also offers to configure autologin for kiosk mode and Samba file sharing so you can manage content from any computer on your network. See [docs/INSTALL.md](docs/INSTALL.md) for the full step-by-step guide starting from hardware setup.

## Prerequisites

- Go 1.25+ (only needed if building from source)
- GCC (for CGo/Fyne) — on Windows, install via [MSYS2](https://www.msys2.org/) or [TDM-GCC](https://jmeubank.github.io/tdm-gcc/)
- `cec-client` on the PATH for HDMI CEC control (optional; no-op if absent)
- `ffmpeg`/`ffprobe` on the PATH for MP4 video slides (optional; `.mp4` files are silently skipped, with a log line, if absent — Windows: `ffmpeg.exe`/`ffprobe.exe` on PATH work the same way)

**Existing installs:** if you installed Mural before video support was added, run `sudo apt install ffmpeg` to pick it up — `install.sh` only adds it for new installs, and the running binary won't complain about missing videos anywhere you'll see it (see *Video playback* below).

## Usage

```bash
go build .
mkdir -p content
# Place your .jpg / .jpeg / .png images and/or .mp4 (H.264) videos in content/
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
| Home | Rescan content directory and show first slide |
| Delete | Pause (black screen) |
| Esc | Quit |

When the display is paused (scheduled off-time or Delete key), any nav key wakes it immediately.

The window defaults to 800x450 and is resizable. Ratpoison will automatically fit it to screen.

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

## How It Works

- Images and videos are loaded from the content directory in filename order. Only changed files are re-decoded on reload.
- Tiny thumbnails (default 80px, configurable via `thumb_width` in config) are pre-loaded for instant keyboard navigation; full images are decoded on demand and scaled to the window.
- A generation counter prevents stale background loads from overwriting a newer slide.
- All UI updates from background goroutines go through `fyne.Do()`.
- The scheduler sleeps until the next event each day; CEC commands run via `cec-client -s`.

### Video playback

- Supported: MP4 (H.264) only. A video's thumbnail is its first frame; its on-screen duration is its own exact playback length, not the configured `interval` — `interval` continues to govern image slides only.
- Playback is always muted; there is no audio configuration.
- A video plays once per showing and always restarts from the beginning — there's no mid-playback resume after a pause or interrupt, matching how image slides are redisplayed.
- An `.mp4` that fails to decode (wrong codec, corrupt file, no video stream) is excluded from the rotation and logged; it never halts the slideshow.
- **Kill switch:** if video playback misbehaves on a running kiosk, `sudo apt remove ffmpeg` (or otherwise removing `ffmpeg`/`ffprobe` from PATH) reverts Mural to exactly its image-only behaviour with no redeploy needed — every `.mp4` is then skipped at scan time.
- **Underpowered hardware:** frame delivery is designed to degrade — on hardware that can't keep up, frames are dropped and playback stays real-time rather than falling behind or crashing. If a device still struggles (e.g. an older Pi with 1080p clips), re-encode the source video at a lower resolution; there's no in-app quality setting.
- **Reading the logs:** `.xinitrc` runs `./mural` with no output redirection, so log lines (including "video skipped" messages) go to a tty nobody watches during normal kiosk operation. To see them, log in over SSH and run `./mural` directly from `~/mural`.

## Development

```bash
go run .
```

> **Note:** The first build takes a long time (10-20 min on Windows) due to Fyne's CGo compilation. Subsequent builds are fast thanks to the build cache.
