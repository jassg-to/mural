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
- `media.go` — `MediaWatcher`: polls `/proc/self/mountinfo` for mount points appearing under `-media-dir`, emitting new ones on a channel; pure `parseMountinfo`/`newMountPoints`/`underRoot`
- `ingest.go` — `classifyVolume` (the three volume dispositions), `imageFilesIn`/`availableBytes`, and `ingestVolume`: the stage-then-commit USB stick ingest transaction
- `install.sh` — one-line installer for Raspberry Pi (downloads binary, installs deps, adds `video`/`input` group membership, installs the USB-stick automount udev rule)
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
- Tiny thumbnails (default 80px wide, configurable via `thumb_width` in `config.toml`) are pre-loaded for instant navigation — but not at startup. `Run()`'s initial scan (`scanSlidePaths`) discovers the slide list (path/size/mtime only) without decoding any thumbnails, so it stays fast no matter how many images are in the content directory; slide 0 is then decoded straight to full resolution and presented as soon as that one decode finishes, with no thumbnail flash (there is nothing to flash yet). Thumbnails for every slide, slide 0 included, are generated afterward by a background job (`startThumbnailLoad`) and merged into `[]Slide` on the run-loop goroutine (`applyLoadedThumbs`, matched by path/size/mtime so a `Reload` or USB ingest racing the same background job can never clobber a newer slide list) — the same command-channel pattern as every other background result. `Reload` and ingest still use the eager `scanSlides`, which decodes thumbnails for every changed file up front, since neither is on the critical path to the first frame the way startup is.
- Navigating (`NavLeft`/`NavRight`/`NavHome`) to a slide whose thumbnail hasn't finished loading yet is not a special case: `show`'s existing nil-thumbnail handling (`compositeLetterboxed(nil, ...)`, the same solid-black placeholder a corrupt image gets) covers it, followed a moment later by that slide's own full decode landing normally.
- Full images are decoded and scaled to fit the display's discovered resolution on demand (`decodeAndFit`), never held at full resolution; `compositeLetterboxed` then centers the result onto a black `width×height` frame at present time. This decode always runs off the run-loop goroutine (in `show`'s background goroutine), whether triggered by manual nav, auto-advance, or startup.
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

- **USB stick hotplug** lets an operator update a sign's content and settings by plugging in a USB stick — no network, no shell access. `media.go`'s `MediaWatcher` detects a newly mounted volume by polling `/proc/self/mountinfo` (not inotify on the mount directory — a mount point is created before it is mounted, so `IN_CREATE` fires too early and would scan an empty directory) and sends its mount point on a channel into the run loop, which serializes ingests one at a time via a small deduplicated queue (`Slideshow.handleMediaMount`).
- **A volume has exactly three dispositions** (`ingest.go`'s `classifyVolume`): **ignored** (no top-level `config.toml` — not addressed to Mural, e.g. a personal flash drive; nothing changes, nothing logged as a failure), **rejected** (a `config.toml` is present but unparseable or defines no on-window anywhere — addressed to Mural and broken; nothing changes, logged as an error), or **accepted** (a usable `config.toml` — the ingest proceeds). `config.toml` is a marker of intent, not a security control; physical USB port access is the same trust boundary as the keyboard already wired to the Pi.
- **Ingest is a stage-then-commit transaction** confined to the content directory: images and `config.toml` are copied into `content/.ingest-staging/` and verified byte-for-byte before anything active is touched, then the commit (`ingestVolume`'s final phase) renames the active image set into `content/previous/` and the staged set into `content/`, and atomically replaces `content/config.toml` (link-then-rename-over, so a concurrent `Schedule.reload()` never observes a missing config). This is what makes "the display never blanks or shows a partial result mid-ingest" and "a stick pulled out mid-copy changes nothing" both hold. `scanSlides` already skips directories, so `previous/` and `.ingest-staging/` need no scanner changes.
- **Retention is bounded to the single most recently displaced set**, and only for the payload actually displaced: an accepted volume with images reclaims (deletes) `previous/`'s old images before staging, but a config-only volume — one with no images — reclaims nothing, so a settings-only stick can never destroy the operator's only recovery copy of a previous image library. `previous/config.toml` is always replaced independently of whether images were displaced.
- **Mounting is the installer's job, not Mural's.** Mural runs unprivileged and only watches `/proc/self/mountinfo` and copies files as itself; `install.sh` installs a udev rule that automounts USB block devices read-only under `-media-dir` (default `/media/mural`) via `systemd-mount`, with FAT/exFAT/NTFS-specific ownership options (`uid=`/`gid=`/`umask=`) confined to their own rule since ext4/btrfs/xfs reject those as unknown options and fail the mount outright otherwise.
- **An empty content directory is a legal, permanent state**, not a startup error: `Slideshow.waiting()` is true whenever `len(s.slides) == 0`, and every slide-indexing site (`show`, `advanceToNext`, `handleNavKey`, `handleVTEvent`) is guarded to present black and arm no timer rather than dividing by zero or indexing out of range. This is what lets a freshly deployed sign boot with no images and be provisioned entirely by handing it a USB stick.
- **An accepted volume applies its `[slideshow]` and `[schedule]` settings live, without a restart.** The ingest-result handler (on the run-loop goroutine) calls `Slideshow.applySlideshowConfig` directly — never the posting `ApplyConfig`, which would deadlock the run loop sending to itself over its own unbuffered command channel — and invokes `Schedule.ApplyConfig`, which swaps the config under its mutex and signals the schedule's event goroutine to recompute today's events immediately via a non-blocking `replan` channel, rather than sleeping through the change until the next event or midnight.

## Conventions

- Keep it simple — this is a single-purpose signage player.
- Images are loaded from the content directory at runtime (default `content/`, passed as a positional argument).
