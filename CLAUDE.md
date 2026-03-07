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
- `.github/workflows/release.yaml` — CI: cross-compiles linux/amd64, arm64, arm on tag push; publishes GitHub Release
- `content/` — runtime image directory and `schedule.toml` (not committed; `.gitignore`d)
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
- Tiny thumbnails (default 80px wide, configurable via `-thumb-width` flag) are pre-loaded for instant keyboard navigation.
- Full images are decoded and scaled to the window size on demand (`decodeAndFit`), never held at full resolution.
- A generation counter (`atomic.Int64`) prevents stale background loads from overwriting newer slides.
- All off-main-thread UI updates go through `fyne.Do()`.
- Supported formats: JPG, JPEG, PNG.
- `Schedule` sleeps until each event; at turn-on it calls `ss.Reload` then `cec.TurnOn`; at turn-off it calls `ss.Pause` then `cec.TurnOff`.
- `Slideshow.Pause()` blacks the screen and stops the ticker. Any nav key resumes (calls `onResume` → CEC TurnOn in a goroutine, then restarts the ticker). Delete key manually pauses (simulates schedule off).
- `cec.go` wraps `cec-client -s -d 1`; graceful no-op if `cec-client` is not in `$PATH`.
- The schedule file is auto-reloaded daily at a configurable time (default 01:00). `[[special]]` rules match an Nth weekday of the month and union their windows with the regular weekday windows.

## Deployment

- Pre-built Linux binaries (amd64, arm64, armv7) are published as GitHub Releases on every tag push.
- `install.sh` is a curl-pipe-bash installer: installs system packages, downloads the latest release binary, writes X11/ratpoison dotfiles, creates `~/mural/content/` with a sample schedule, and optionally configures systemd autologin for kiosk mode and Samba file sharing (anonymous read/write `content` share).
- `docs/INSTALL.md` covers the full Raspberry Pi journey from hardware purchase through first boot.

## Conventions

- Keep it simple — this is a single-purpose signage player.
- Target platform is Linux but we want to support Windows too.
- Images are loaded from the content directory at runtime (default `content/`, configurable via `-content` flag).
