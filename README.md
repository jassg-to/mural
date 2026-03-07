# Mural Digital

A digital signage player built with [Fyne](https://fyne.io/). Cycles through images in a `content/` subdirectory, with a daily schedule for display on/off times and HDMI CEC control. Optimized for Raspberry Pi.

## Prerequisites

- Go 1.25+
- GCC (for CGo/Fyne) — on Windows, install via [MSYS2](https://www.msys2.org/) or [TDM-GCC](https://jmeubank.github.io/tdm-gcc/)
- `cec-client` on the PATH for HDMI CEC control (optional; no-op if absent)

## Usage

```bash
go build .
mkdir -p content
# Place your .jpg / .jpeg / .png images in content/
# Copy and edit the schedule (see below)
cp schedule.toml.example content/schedule.toml
./mural-digital
```

### Options

| Flag | Default | Description |
|------|---------|-------------|
| `-interval` | `30s` | Time between automatic slide transitions |
| `-content` | `content` | Directory containing images and `schedule.toml` |

```bash
./mural-digital -interval 10s -content /var/mural
```

### Controls

| Key | Action |
|-----|--------|
| Right arrow | Next slide |
| Left arrow | Previous slide |
| Home | First slide |
| Esc | Quit |

When the display is paused (scheduled off-time), any nav key wakes it immediately.

The window defaults to 800x450 and is resizable.

## Schedule

Create a `content/schedule.toml` to control daily on/off windows:

```toml
reload_time = "01:00"  # reload this file daily at this time (HH:MM; default: "01:00")

[weekday]
monday    = [{ on = "08:00", off = "12:00" }, { on = "13:30", off = "22:00" }]
tuesday   = [{ on = "08:00", off = "12:00" }, { on = "13:30", off = "22:00" }]
wednesday = [{ on = "08:00", off = "12:00" }, { on = "13:30", off = "22:00" }]
thursday  = [{ on = "08:00", off = "12:00" }, { on = "13:30", off = "22:00" }]
friday    = [{ on = "08:00", off = "12:00" }, { on = "13:30", off = "22:00" }]
saturday  = [{ on = "10:00", off = "18:00" }]
sunday    = []   # off all day

[[special]]
name       = "Last Sunday"
weekday    = "Sunday"
occurrence = -1   # 1 = first, 2 = second, -1 = last
windows    = [{ on = "09:00", off = "14:00" }]

[[special]]
name       = "First Saturday"
weekday    = "Saturday"
occurrence = 1
windows    = [{ on = "07:00", off = "20:00" }]
```

- Each day has a list of `{ on, off }` windows in `HH:MM` (local time).
- `[[special]]` rules match an Nth weekday of the month and add extra on-time (union).
- At each turn-on event, the `content/` directory is rescanned for new or changed images.

## How It Works

- Images are loaded from `content/` in filename order. Only changed files are re-decoded on reload.
- Tiny thumbnails (48px) are pre-loaded for instant keyboard navigation; full images are decoded on demand.
- A generation counter prevents stale background loads from overwriting a newer slide.
- All UI updates from background goroutines go through `fyne.Do()`.
- The scheduler sleeps until the next event each day; CEC commands run via `cec-client -s`.

## Development

```bash
go run .
```

> **Note:** The first build takes a long time (10–20 min on Windows) due to Fyne's CGo compilation. Subsequent builds are fast thanks to the build cache.
