# Mural

Simple digital signage player that cycles through images in a `content/` subdirectory. Optimized for Raspberry Pi.

## Tech Stack

- **Language:** Go
- **GUI:** Fyne v2
- **Build:** CGo required (GCC)
- **Tooling:** mise for Go version management

## Project Structure

- `main.go` — minimal entrypoint; wires CEC, schedule, and slideshow
- `slideshow.go` — `Slideshow` struct; image loading, display, pause/resume
- `cec.go` — `CEC` struct; wraps `cec-client` CLI for HDMI display control
- `schedule.go` — `Schedule` struct; TOML-driven daily on/off scheduler
- `install.sh` — one-line installer for Raspberry Pi (downloads binary, installs deps, writes dotfiles)
- `docs/INSTALL.md` — step-by-step Raspberry Pi setup guide (from imaging the SD card to running)
- `docs/kit.jpg` — photo of recommended hardware kit
- `.github/workflows/build.yaml` — CI: cross-compiles linux/amd64, arm64, arm on tag push; publishes GitHub Release.
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

- Images are stored as `[]Slide` (path, thumbnail, size, mtime). On `Reload`, unchanged files are reused without re-decoding.
- Tiny thumbnails (default 80px wide, configurable via `thumb_width` in `config.toml`) are pre-loaded for instant keyboard navigation.
- Full images are decoded and scaled to the window size on demand (`decodeAndFit`), never held at full resolution. This decode always runs off the Fyne main goroutine (in `show`'s background goroutine), whether triggered by manual nav or auto-advance.
- A generation counter (`atomic.Int64`) prevents stale background image decodes from overwriting newer slides.
- All off-main-thread UI updates go through `fyne.Do()`.
- Supported formats: JPG, JPEG, PNG. A corrupt image is still included in the rotation with a nil thumbnail rather than being excluded (pre-existing behaviour, unchanged).
- Auto-advance timing: a single global `time.Ticker` was replaced with a per-slide `time.Timer` (`advanceTimer`), armed by `scheduleAdvance` every time `show` displays a slide, always with the configured `interval`.
- `Schedule` sleeps until each event; at turn-on it calls `ss.Reload` then `cec.TurnOn`; at turn-off it calls `ss.Pause` then `cec.TurnOff`.
- `Slideshow.Pause()` blacks the screen and stops the advance timer. Any nav key resumes (calls `onResume` → CEC TurnOn in a goroutine, then `show` re-arms the timer). Delete key manually pauses (simulates schedule off).
- `cec.go` wraps `cec-client -s -d 1`; graceful no-op if `cec-client` is not in `$PATH`.
- The config file (`config.toml`) is auto-reloaded daily at a configurable time (default 01:00). Day configs support Nth-weekday-of-month occurrence fields that union their windows with the regular weekday windows.
- Video playback is intentionally out of scope for this Go codebase. A hardware spike found mural's in-process ffmpeg-frame-streaming-into-canvas approach has real frame-jitter problems on Raspberry Pi 3B+-class hardware, while a standalone Kodi process talking directly to DRM/KMS played the same content cleanly. The planned (not yet implemented) path for video is a future "Kodi launcher mode," where mural hands off to an externally-managed Kodi process rather than decoding video itself.

- Pre-built Linux binaries (amd64, arm64, armv7) are published as GitHub Releases on every tag push.
- `install.sh` is a curl-pipe-bash installer: installs system packages, downloads the latest release binary, writes X11/ratpoison dotfiles, creates `~/mural/content/` with a sample schedule, and optionally configures systemd autologin for kiosk mode and Samba file sharing (`content` share restricted to `valid users`, not anonymous — Samba setup prompts for a password for the installing user).
- `docs/INSTALL.md` covers the full Raspberry Pi journey from hardware purchase through first boot.
- A NixOS deployment path was attempted for the Pi 3B+ and abandoned: on-device `nixos-rebuild` reliably OOM-kills during evaluation on the board's 1GB RAM, and building the closure elsewhere added enough friction that it wasn't worth it for a single sign. The Debian/`install.sh` path above is the only supported deployment target. Should a future large-display host revisit NixOS, `specs/nixos-deployment/Analyst.md` records the prior problem-definition phase; the flake, module, and architecture/task docs that followed it were deleted as stale rather than kept dormant.

## Conventions

- Keep it simple — this is a single-purpose signage player.
- Target platform is Linux but we want to support Windows too.
- Images are loaded from the content directory at runtime (default `content/`, passed as a positional argument).
