# Mural

Simple digital signage player that cycles through images in a `content/` subdirectory. Optimized for Raspberry Pi.

## Tech Stack

- **Language:** Go
- **GUI:** Fyne v2
- **Build:** CGo required (GCC)
- **Tooling:** mise for Go version management

## Project Structure

- `main.go` — minimal entrypoint; wires CEC, video, schedule, and slideshow
- `slideshow.go` — `Slideshow` struct; image/video loading, display, pause/resume
- `video.go` — `Video` struct; wraps `ffmpeg`/`ffprobe` CLIs for video metadata, first-frame thumbnails, and frame-streaming playback
- `cec.go` — `CEC` struct; wraps `cec-client` CLI for HDMI display control
- `schedule.go` — `Schedule` struct; TOML-driven daily on/off scheduler
- `install.sh` — one-line installer for Raspberry Pi (downloads binary, installs deps, writes dotfiles)
- `docs/INSTALL.md` — step-by-step Raspberry Pi setup guide (from imaging the SD card to running)
- `docs/kit.jpg` — photo of recommended hardware kit
- `.github/workflows/build.yaml` — CI: cross-compiles linux/amd64, arm64, arm on tag push; publishes GitHub Release
- `content/` — runtime image directory and `config.toml` (not committed; `.gitignore`d)
- `go.mod` / `go.sum` — Go module dependencies
- `mise.toml` — mise tool versions

## Build Notes

- First Fyne build on Windows is very slow (~20 min) due to CGo compilation. Subsequent builds use the cache.
- Use `go build -buildvcs=false .` if building from a repo with no commits.

## Go Best Practices

- Use `gofmt`/`goimports` for formatting — never manually style code.
- Handle all errors explicitly; never discard with `_`. Prefer `fmt.Errorf("context: %w", err)` for wrapping.
- Use `context.Context` for cancellation and timeouts in long-running or concurrent operations.
- Prefer returning errors over panicking. Reserve `panic` for truly unrecoverable states.
- Keep functions short and focused. If a function needs a comment explaining what it does, consider renaming it or splitting it.
- Use named return values sparingly — only when they improve clarity.
- Group imports in standard library, external, and internal blocks (enforced by `goimports`).
- Use `defer` for cleanup (closing files, unlocking mutexes) immediately after acquiring the resource.
- Prefer struct embedding over inheritance-style patterns.
- Use interfaces at the consumer side, not the producer side. Keep interfaces small (1-2 methods).
- Run `go vet` and `staticcheck` to catch common issues.

## Architecture Notes

- Images and videos are stored as `[]Slide` (path, kind, thumbnail, duration, vidW/vidH, size, mtime). On `Reload`, unchanged files are reused without re-decoding/re-probing.
- Tiny thumbnails (default 80px wide, configurable via `thumb_width` in `config.toml`) are pre-loaded for instant keyboard navigation; a video slide's thumbnail is its first frame.
- Full images are decoded and scaled to the window size on demand (`decodeAndFit`), never held at full resolution. This decode always runs off the Fyne main goroutine (in `show`'s background goroutine), whether triggered by manual nav or auto-advance.
- A generation counter (`atomic.Int64`) prevents stale background loads (image decodes and video frames alike) from overwriting newer slides.
- All off-main-thread UI updates go through `fyne.Do()`.
- Supported formats: JPG, JPEG, PNG, and MP4 (H.264 only). An `.mp4` that fails to probe/decode is excluded from the rotation and logged, never halting the slideshow; a corrupt image, by contrast, is still included with a nil thumbnail (pre-existing behaviour, unchanged).
- Auto-advance timing: a single global `time.Ticker` was replaced with a per-slide `time.Timer` (`advanceTimer`), armed by `scheduleAdvance` every time `show` displays a slide. Image slides arm it with the configured `interval`, unchanged from before. Video slides ignore `interval` entirely and advance at `max(end-of-stream, probed duration)` — ffmpeg's `-re` pacing measurably finishes slightly early, so the probed duration (not raw EOS) governs, with `duration + 2s` armed as a watchdog in case `ffmpeg` wedges. A lone video slide re-showing itself is floor-limited to 1 consecutive playback start per second, without clamping the clip's own length.
- Video playback streams raw RGBA frames from an `ffmpeg` subprocess into the same `canvas.Image` used for static images (never a separate player window), through a single-slot latest-frame-wins handoff so hardware that can't keep up drops frames instead of queuing unboundedly. Playback is owned by a `context.CancelFunc` (`playerCancel`) and torn down via `stopPlayback()`, called from `show`, `pause`, `Reload`, and the nav key handler. `playerCancel` and the negative-result (rejected-file) cache are written only on the Fyne main goroutine or passed as immutable snapshots — `Reload` runs on a background goroutine, so any main-goroutine state it touches is marshalled through `fyne.Do`.
- `Schedule` sleeps until each event; at turn-on it calls `ss.Reload` then `cec.TurnOn`; at turn-off it calls `ss.Pause` then `cec.TurnOff`.
- `Slideshow.Pause()` blacks the screen, stops any active video playback, and stops the advance timer. Any nav key resumes (calls `onResume` → CEC TurnOn in a goroutine, then `show` re-arms the timer). Delete key manually pauses (simulates schedule off). A video slide always restarts from the beginning on resume/redisplay, never resuming mid-playback.
- `cec.go` wraps `cec-client -s -d 1`; graceful no-op if `cec-client` is not in `$PATH`.
- `video.go` wraps `ffmpeg`/`ffprobe -v error -select_streams v:0 ...`; graceful no-op (video files silently skipped, one log line) if either is not in `$PATH`. Filenames are always resolved with `filepath.Abs` before being passed to a subprocess, since `content/` is a network-writable Samba share.
- The config file (`config.toml`) is auto-reloaded daily at a configurable time (default 01:00). Day configs support Nth-weekday-of-month occurrence fields that union their windows with the regular weekday windows.

## Deployment

- Pre-built Linux binaries (amd64, arm64, armv7) are published as GitHub Releases on every tag push.
- `install.sh` is a curl-pipe-bash installer: installs system packages (including `ffmpeg`), downloads the latest release binary, writes X11/ratpoison dotfiles, creates `~/mural/content/` with a sample schedule, and optionally configures systemd autologin for kiosk mode and Samba file sharing (`content` share restricted to `valid users`, not anonymous — Samba setup prompts for a password for the installing user).
- `docs/INSTALL.md` covers the full Raspberry Pi journey from hardware purchase through first boot, including video playback troubleshooting (missing-ffmpeg log visibility, underpowered-hardware frame dropping, and the `sudo apt remove ffmpeg` kill switch).

## Conventions

- Keep it simple — this is a single-purpose signage player.
- Target platform is Linux but we want to support Windows too.
- Images are loaded from the content directory at runtime (default `content/`, passed as a positional argument).
