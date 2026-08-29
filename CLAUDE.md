# Mural

Simple digital signage player that cycles through images in a `content/` subdirectory. Optimized for Raspberry Pi.

## Tech Stack

- **Language:** Go
- **Display/Input:** Direct DRM/KMS + evdev, hand-written — no GUI toolkit, no X11, no Wayland
- **Build:** Pure Go, no CGo, no C toolchain required
- **Tooling:** mise for Go version management

## Project Structure

- `main.go` — minimal entrypoint; wires CEC, schedule, renderer, input, VT handling, and slideshow
- `slideshow.go` — `Slideshow` struct; image loading, single-goroutine run loop, pause/resume
- `renderer.go` — the `Renderer` interface (`Present`/`Close`) `Slideshow` presents frames to
- `compositor.go` — `compositeLetterboxed`; pure, hardware-independent letterbox/black-frame compositing
- `drm_ioctl.go` — raw DRM ioctl request numbers and kernel-ABI structs; pure helpers `pickPreferredMode`, `rgbaToXRGB8888`
- `drm.go` — `DRMRenderer`: real-hardware `Renderer`, hand-rolled DRM/KMS dumb-buffer double-buffering
- `vt.go` — VT-switch (`SIGUSR1`/`SIGUSR2`) and graceful-shutdown console recovery (`SIGTERM`/`SIGINT`) for `DRMRenderer`
- `headless.go` — `HeadlessRenderer`: dev-loop `Renderer` that PNG-dumps frames instead of driving hardware (`-headless`)
- `input.go` — evdev enumeration/hotplug, raw `input_event` decoding into `NavKey`
- `cec.go` — `CEC` struct; wraps `cec-client` CLI for HDMI display control
- `schedule.go` — `Schedule` struct; TOML-driven daily on/off scheduler
- `install.sh` — one-line installer for Raspberry Pi (downloads binary, installs deps, adds `video`/`input` group membership)
- `docs/INSTALL.md` — step-by-step Raspberry Pi setup guide (from imaging the SD card to running)
- `docs/kit.jpg` — photo of recommended hardware kit
- `.github/workflows/build.yaml` — CI: cross-compiles linux/amd64, arm64, arm on tag push; publishes GitHub Release.
- `content/` — runtime image directory and `config.toml` (not committed; `.gitignore`d)
- `go.mod` / `go.sum` — Go module dependencies
- `mise.toml` — mise tool versions

## Build Notes

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
- Tiny thumbnails (default 80px wide, configurable via `thumb_width` in `config.toml`) are pre-loaded for instant navigation.
- Full images are decoded and scaled to fit the display's discovered resolution on demand (`decodeAndFit`), never held at full resolution; `compositeLetterboxed` then centers the result onto a black `width×height` frame at present time. This decode always runs off the run-loop goroutine (in `show`'s background goroutine), whether triggered by manual nav or auto-advance.
- A generation counter (`atomic.Int64`) prevents stale background image decodes from overwriting newer slides.
- `Slideshow` presents composited frames through the `Renderer` interface (`Present`/`Close`) — `DRMRenderer` for real hardware, `HeadlessRenderer` (PNG-dump, `-headless`) for development off the sign. Every field that used to be touched from multiple goroutines is now mutated only inside a single run-loop goroutine in `Run()`, driven by a `select` over the advance timer, `input.go`'s `NavKey` channel, an internal command channel (background decode results, `Pause`/`Reload` requests), `vt.go`'s VT-switch events, and `context.Context` cancellation.
- Supported formats: JPG, JPEG, PNG. A corrupt image is still included in the rotation; its frame is a deliberate solid black (`compositeLetterboxed(nil, ...)`), not an accident of a nil image.
- Auto-advance timing: a per-slide `time.Timer` (`advanceTimer`), armed by `scheduleAdvance` every time `show` displays a slide, always with the configured `interval`.
- `Schedule` sleeps until each event; at turn-on it calls `ss.Reload` then `cec.TurnOn`; at turn-off it calls `ss.Pause` then `cec.TurnOff`.
- `Slideshow.Pause()` blacks the screen (`renderer.Present(nil)`) and stops the advance timer. `NavLeft`/`NavRight`/`NavHome`/`NavWake` resume a paused sign first (CEC `TurnOn`, current slide redisplayed, timer re-armed); `NavSleep` (Delete) and `NavQuit` (Escape) act immediately regardless of pause state and never themselves trigger a resume.
- `cec.go` wraps `cec-client -s -d 1`; graceful no-op if `cec-client` is not in `$PATH`.
- The config file (`config.toml`) is auto-reloaded daily at a configurable time (default 01:00). Day configs support Nth-weekday-of-month occurrence fields that union their windows with the regular weekday windows.
- Input is read directly from the kernel: `input.go` enumerates `/dev/input/event*`, decodes raw `input_event` structs into a `NavKey` (`NavLeft`/`NavRight`/`NavHome`/`NavSleep`/`NavQuit`/`NavWake`), and watches for hotplug via `inotify`. Binding is by keycode, not by device — any keyboard-like device sharing the same layout works, including the physical three-key remote.
- Display output is direct DRM/KMS: `drm.go`/`drm_ioctl.go` hand-roll the ioctl layer (no mature Go DRM library exists) to acquire master, discover the connector's preferred mode, and double-buffer via dumb buffers with page-flip. `vt.go` owns VT-switch (`SIGUSR1`/`SIGUSR2`: drop/reacquire master and re-set mode) and graceful-shutdown console recovery (`SIGTERM`/`SIGINT`: restore `KD_TEXT`); `SIGKILL` relies on the kernel's own fd-close teardown instead.
- Video playback is intentionally out of scope for this Go codebase. A hardware spike found mural's in-process ffmpeg-frame-streaming-into-canvas approach has real frame-jitter problems on Raspberry Pi 3B+-class hardware, while a standalone Kodi process talking directly to DRM/KMS played the same content cleanly. The planned (not yet implemented) path for video is a future "Kodi launcher mode," where mural hands off to an externally-managed Kodi process rather than decoding video itself.

- Pre-built Linux binaries (amd64, arm64, armv7) are published as GitHub Releases on every tag push.
- `install.sh` is a curl-pipe-bash installer: installs system packages, downloads the latest release binary, adds the installing user to the `video`/`input` groups for unprivileged DRM/evdev access, creates `~/mural/content/` with a sample schedule, and optionally configures systemd autologin for kiosk mode (execing the `mural` binary directly — no X server) and Samba file sharing (`content` share restricted to `valid users`, not anonymous — Samba setup prompts for a password for the installing user).
- `docs/INSTALL.md` covers the full Raspberry Pi journey from hardware purchase through first boot.
- A NixOS deployment path was attempted for the Pi 3B+ and abandoned: on-device `nixos-rebuild` reliably OOM-kills during evaluation on the board's 1GB RAM, and building the closure elsewhere added enough friction that it wasn't worth it for a single sign. The Debian/`install.sh` path above is the only supported deployment target. Should a future large-display host revisit NixOS, `specs/nixos-deployment/Analyst.md` records the prior problem-definition phase; the flake, module, and architecture/task docs that followed it were deleted as stale rather than kept dormant.

## Conventions

- Keep it simple — this is a single-purpose signage player.
- Images are loaded from the content directory at runtime (default `content/`, passed as a positional argument).
