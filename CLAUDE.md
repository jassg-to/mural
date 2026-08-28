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
- `flake.nix` — Nix flake: `packages`, `overlays.default`, `nixosModules.mural`/`kiosk-x11`, `nixosConfigurations.sign`, `checks`, `devShells`
- `nix/package.nix` — `buildGoModule` derivation for Mural (filtered source, GL/X11 `buildInputs`)
- `nix/modules/mural.nix` — `services.mural.*` NixOS module: kiosk user, content dir + seeding, CEC, Samba share, graphics assertion
- `nix/modules/kiosk-x11.nix` — `services.mural.session = "x11"`: greetd, guarded relaunch, tty2 operator console
- `nix/session/xinitrc.sh`, `nix/session/ratpoisonrc` — store-resident X session scripts
- `nix/config.sample.toml` — seed `config.toml`, copied into a fresh content dir on first activation
- `nix/hosts/sign/` — the one deployed Pi 3B+: `default.nix` (hostname, wireless, SSH, timezone), `boot.nix` (extlinux, firmware, fileSystems)
- `nix/tests/` — `nixosTest`s for the kiosk session, content provisioning, and the Samba share (module logic only; see `services.mural.enable` doc comments)
- `docs/INSTALL.md` — NixOS setup guide: flashing the stock SD image through first rebuild
- `docs/MIGRATION.md` — what happens to a sign still running the retired Raspberry Pi OS install
- `docs/kit.jpg` — photo of recommended hardware kit
- `.github/workflows/build.yaml` — CI: `nix build .#mural` on `x86_64` and native `aarch64` runners
- `content/` — runtime image directory and `config.toml` (not committed; `.gitignore`d)
- `go.mod` / `go.sum` — Go module dependencies
- `mise.toml` — mise tool versions (dev-loop convenience; `nix develop` is the reproducible equivalent)

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
- Video playback is intentionally out of scope for this Go codebase. A hardware spike found mural's in-process ffmpeg-frame-streaming-into-canvas approach has real frame-jitter problems on Raspberry Pi 3B+-class hardware, while a standalone Kodi process talking directly to DRM/KMS played the same content cleanly. The planned (not yet implemented) path for video is a future "Kodi launcher mode," where mural hands off to an externally-managed Kodi process rather than decoding video itself. See `specs/nixos-deployment/spike-findings.md` for the full findings.

## Deployment

- NixOS is the only supported Linux deployment target; the Raspberry Pi OS / `install.sh` path is retired (see `docs/MIGRATION.md`). Windows is unaffected and still built from source (`go build`), not via Nix.
- No pre-built Linux binaries are published. `nix build .#mural` (or `nix build github:jassg-to/mural#mural`) is the way to get a running binary; `.github/workflows/build.yaml` only verifies it still builds, on `x86_64` and native `aarch64` runners.
- The `services.mural` NixOS module (`nix/modules/mural.nix`) is the option contract: `enable`, `package`, `user`, `contentDir`, `seedConfig`, `session` (`"x11"` or `"none"`), `restartDelay`, `cec.enable`, `share.*`. `session = "none"` provides the package, user, content dir, and CEC access with no display — the hook point for a future presentation layer (e.g. `specs/drm-renderer/`) without touching the core module.
- The content directory (`contentDir`, default `/var/lib/mural/content`) is runtime state, never declarative — Nix seeds `config.toml` only if it's absent (`systemd.tmpfiles.rules` `C` directive) and never overwrites an operator's edits on rebuild.
- Residual manual state that no rebuild reproduces: the Wi-Fi PSK (`/etc/mural/wifi.env`, device-local, never committed — the repo is public), and the Samba password (`smbpasswd -a`, one-time). See `docs/INSTALL.md`'s residual-state section.
- Updates are `nixos-rebuild switch --flake .#sign` on-device (over SSH as the `admin` user). Rollback is two mechanisms, not one: `nixos-rebuild switch --rollback` over SSH (remote, soft) versus selecting a prior generation from the U-Boot extlinux menu (requires a keyboard physically at the sign, hard). Don't conflate them in documentation — only the second survives a sign that won't boot at all.
- greetd hardcodes its session to VT1 on the pinned nixpkgs channel (`services.greetd.vt` was removed upstream), so the always-available operator console lives on **tty2**, not tty1 — reach it with Ctrl+Alt+F2 at the physical sign.
- `docs/INSTALL.md` covers the full NixOS journey: flashing the stock upstream SD image through first rebuild.

## Conventions

- Keep it simple — this is a single-purpose signage player.
- Target platform is Linux but we want to support Windows too.
- Images are loaded from the content directory at runtime (default `content/`, passed as a positional argument).
