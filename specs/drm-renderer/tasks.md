# Tasks: Native Rendering Layer (DRM/KMS + evdev)

> Phase 2 output — implementation checklist.
> Each step is atomic: one file created or one coherent change to one file.
> Dependencies and parallelisation opportunities are documented in Architect.md.

### Layer: Compositor (pure, hardware-independent)

- [ ] Step 1: Create `compositor.go` with `compositeLetterboxed(img image.Image, w, h int) *image.RGBA`. `img == nil` produces a solid black frame.
- [ ] Step 2: Add `TestCompositeLetterboxed` in `slideshow_test.go` covering: image narrower than frame, image wider than frame, exact aspect match, and `img == nil` (black frame, defines the corrupt-image case).

### Layer: Renderer Interface

- [ ] Step 3: Create `renderer.go` with the `Renderer` interface (`Present(*image.RGBA) error`, `Close() error`).

### Layer: DRM Backend

- [ ] Step 4: Create `drm_ioctl.go` — raw ioctl request constants and C-struct-layout Go structs for `SET_MASTER`/`DROP_MASTER`, `GET_RESOURCES`, `GET_CONNECTOR`, `GET_ENCODER`, `CREATE_DUMB`, `MAP_DUMB`, `ADD_FB`, `SET_CRTC`, `PAGE_FLIP`.
- [ ] Step 5: In `drm_ioctl.go`, add pure helper `pickPreferredMode(modes []drmModeInfo) drmModeInfo` (index-0 selection, matching the board's own `modetest -c` ordering).
- [ ] Step 6: In `drm_ioctl.go`, add pure helper `rgbaToXRGB8888(dst []byte, pitch int, src *image.RGBA)`.
- [ ] Step 7: Add `TestPickPreferredMode` and `TestRGBAToXRGB8888` in `slideshow_test.go` — no hardware required.
- [ ] Step 8: Create `drm.go` — `DRMRenderer` struct and `OpenDRMRenderer(devicePath string) (*DRMRenderer, width, height int, err error)`: acquire master, enumerate the connected connector, pick its mode via `pickPreferredMode`, allocate and map two dumb buffers. Each acquisition (fd open, `SET_MASTER`, buffer 1 create/map, buffer 2 create/map) gets its cleanup deferred immediately after it succeeds — not batched at the end — so a failure partway through (e.g. buffer 2's map fails after buffer 1 already succeeded) unwinds everything acquired so far via a named-error return path, per `CLAUDE.md`'s "use `defer` for cleanup immediately after acquiring the resource." A partial failure that leaks DRM master is exactly the failure mode this design exists to avoid.
- [ ] Step 9: In `drm.go`, implement `(*DRMRenderer) Present(frame *image.RGBA) error` — `nil` frame composites black via `compositeLetterboxed`, converts via `rgbaToXRGB8888` into the back buffer, page-flips, waits for the flip event, swaps front/back. A page-flip failure because no display is attached (HDMI unplugged) is returned as an ordinary error like any other page-flip failure — no special detection, no panic, no retry loop inside `Present` itself; the caller (run loop) logs it and moves on, and a later reconnect is picked up reactively the next time `Present` is called for a slide change.
- [ ] Step 10: In `drm.go`, implement `(*DRMRenderer) Close() error` — unmap and destroy both dumb buffers, drop master.
- [ ] Step 11: Create `vt.go` — install `SIGUSR1`/`SIGUSR2` handlers (`VT_SETMODE(VT_PROCESS)` on the controlling tty) that drop/reacquire `DRMRenderer`'s master and re-set the mode on VT switch-away/back.
- [ ] Step 12: In `vt.go`, install `SIGTERM`/`SIGINT` handling that calls `DRMRenderer.Close()` and restores the console to text mode (`KDSETMODE(KD_TEXT)`) before exit.

### Layer: Headless Backend

- [ ] Step 13: Create `headless.go` — `HeadlessRenderer` struct, `NewHeadlessRenderer() (*HeadlessRenderer, width, height int)` (fixed 1920x1080), `Present` (PNG-encodes the composited frame to a fixed path, logging the path once at construction), `Close` (no-op).

### Layer: Input

- [ ] Step 14: Create `input.go` — `NavKey` type (`NavLeft`, `NavRight`, `NavHome`, `NavSleep`, `NavQuit`, `NavWake`) and a pure `parseInputEvent(raw []byte) (key NavKey, ok bool)` decoding a raw `input_event` struct: `KEY_LEFT`/`KEY_RIGHT`/`KEY_HOME`/`KEY_DELETE`/`KEY_ESC` map to their named `NavKey` on either `value == 1` (initial press) or `value == 2` (kernel autorepeat) — both are key-down states and must be treated identically; any other key on `value == 1`/`value == 2` maps to `NavWake`; `value == 0` (key-up) and non-`EV_KEY` events return `ok == false`.
- [ ] Step 15: Add `TestParseInputEvent` in `slideshow_test.go` covering each of the five named keys at `value == 1`, the same five at `value == 2` (autorepeat — asserting identical `NavKey` output to `value == 1`), an arbitrary other key-down (→ `NavWake`), and a key-up (→ `ok == false`) — no hardware required.
- [ ] Step 16: In `input.go`, add device enumeration and hotplug: open all readable `/dev/input/event*` nodes, watch `/dev/input` via `inotify` for new nodes, forward parsed `NavKey`s from every open device into one `Events() <-chan NavKey`, and drop a device's watcher quietly on read error (unplug) without affecting the others. Accept a `context.Context` so every enumeration/watcher goroutine exits on `ctx.Done()` instead of leaking past `Run()` returning (per `CLAUDE.md`'s context-for-cancellation convention).

### Layer: Slideshow Core Rewrite

- [ ] Step 17: Rewrite `slideshow.go`: remove all `fyne`/`canvas`/`app` imports and usage; add a `renderer Renderer` field (replacing `img *canvas.Image`); replace `winSize` with plain `width, height int` fields set once in `NewSlideshow`; update `decodeAndFit` to take `(path string, width, height int)` and use `golang.org/x/image/draw` instead of `github.com/nfnt/resize`; **also update `loadThumbnail` to use `golang.org/x/image/draw` instead of `resize.Resize(width, 0, ...)`** — compute the aspect-preserving target height manually (same scale-factor math as `decodeAndFit`), since `x/image/draw` has no width-only auto-height convenience; route the corrupt-image case through `compositeLetterboxed(nil, ...)` via the renderer.
- [ ] Step 18: In `slideshow.go`, replace all `fyne.Do` marshalling with a single run-loop goroutine in `Run()`: `select` over the advance timer, `input.go`'s `NavKey` channel, a new internal command channel that `Pause`/`Reload` post into (replacing their direct `fyne.Do` calls), a `context.Context`'s `Done()` channel for shutdown, and (when present) `vt.go`'s event channel. `pause`/`resume`/`show`/`scheduleAdvance`/`advanceToNext` keep their existing bodies and locking-by-single-goroutine discipline, now enforced by running only inside this loop.
- [ ] Step 19: In `slideshow.go`'s run loop, handle the `NavKey` channel with today's exact precedence: `NavQuit` and `NavSleep` are checked first and handled unconditionally (quit calls the run loop's `cancel()`; sleep calls `pause()`), neither triggers a resume; every other value (`NavLeft`, `NavRight`, `NavHome`, `NavWake`) resumes a paused sign first if one is paused, then `NavLeft`/`NavRight`/`NavHome` perform their navigation. This is a direct port of `main.go`'s current `SetOnTypedKey` switch, not new logic.
- [ ] Step 20: Add `TestSlideshowEscapeAndDeleteBypassResume` in `slideshow_test.go`: with the sign paused, deliver `NavQuit` and (separately) `NavSleep` through the run loop and assert neither called `resume()` — guards the precedence Step 19 depends on.
- [ ] Step 21: Rewrite `main.go`: create the top-level `context.Context`/`cancel` pair; add `-headless` bool flag; on absence, call `OpenDRMRenderer("/dev/dri/card0")` and exit with a diagnosable error on failure (no fallback); on presence, call `NewHeadlessRenderer()`; construct `input.go`'s watcher with the context; when `DRMRenderer` was chosen, install `vt.go`'s signal handling, wiring `SIGTERM`/`SIGINT` to call `cancel()`; after the run loop returns (on `ctx.Done()`), call the renderer's `Close()` (and, for `DRMRenderer`, restoring the console) before exiting; remove all `fyne/app` window setup; pass the discovered `width, height` into `NewSlideshow`.

### Layer: Tests

- [ ] Step 22: In `slideshow_test.go`, replace `newHeadlessSlideshow` — drop `test.NewApp()`/`canvas.Image`, add a `fakeRenderer` implementing `Renderer` (records each `Present` call's frame, including `nil`-for-blank, and whether `Close` was called).
- [ ] Step 23: Adapt `TestSlideshowPauseStopsTimerAndBlanksImage` to assert `fakeRenderer` received a `Present(nil)` call instead of checking `img.Image == nil`.
- [ ] Step 24: Adapt `TestSlideshowResumeRearmsTimer` and `TestSlideshowNavKeysRearmTimer` to construct `Slideshow` with `fakeRenderer` instead of the Fyne headless harness; assertions on `advanceTimer` are unchanged.
- [ ] Step 25: Adapt `TestSlideshowImageAdvanceTimingUnchanged` the same way. Its existing long comment explains a `test.NewApp()`/synchronous-`fyne.Do` race-avoidance rationale that no longer applies once `fyne` is gone — rewrite or remove that comment rather than leaving Fyne-specific reasoning describing code that no longer exists.

### Layer: Dependencies

- [ ] Step 26: Update `go.mod`: remove `fyne.io/fyne/v2` and its full indirect closure and `github.com/nfnt/resize`; run `go mod tidy` so `golang.org/x/image` and `golang.org/x/sys` become direct requires; commit the resulting `go.sum`.

### Layer: Build & CI

- [ ] Step 27: Update `.github/workflows/build.yaml`: remove the "Install native GL/X11 headers" step and the entire dpkg-multiarch cross-compilation step; set `CGO_ENABLED: "0"` in the build step's env; remove the now-unused `CC`/`PKG_CONFIG_PATH` env and matrix fields (`cross_cc`, `cross_pkg`, `pkg_config_path`, `dpkg_arch`) from the matrix definitions.

### Layer: Packaging & Docs

- [ ] Step 28: Update `install.sh`: remove `apt install`s of `xinit`, `ratpoison`, `cec-utils`'s GL/X11 siblings (`libgl1`, `unclutter`, `x11-xserver-utils`), and the `.xinitrc`/`.ratpoisonrc` writing; add the kiosk user to the `video` and `input` groups (or equivalent udev rule); **in `tty1-guard.sh` specifically**, replace the `startx` line with a direct exec of the Mural binary — keep the "wait 30 seconds before restarting" banner/sleep and the `.bashrc` re-invocation exactly as-is, since that crash-loop guard applies identically regardless of whether the guarded process is `startx` or `mural`.
- [ ] Step 29: Update `docs/INSTALL.md`: remove the "type `startx`" step and any X11-era instructions; document the new direct-autologin flow and the `-headless` developer flag.
- [ ] Step 30: Update `CLAUDE.md`: rewrite Tech Stack (drop Fyne/CGo/GCC lines, add DRM/evdev/pure-Go), Build Notes (drop the CGo first-build note), Architecture Notes (replace the `fyne.Do`/`canvas.Image`/generation-counter description with the `Renderer`/run-loop/evdev/DRM description, keeping the parts that still hold — e.g. the generation counter's role), and Conventions (drop the "we want to support Windows too" line).
- [ ] Step 31: Update `README.md`: remove the Windows/MSYS2/TDM-GCC build prerequisite section and any cross-platform claim; remove or rewrite the "first build takes a long time (10-20 min on Windows) due to Fyne's CGo compilation" note — CGo itself is gone project-wide (`CGO_ENABLED=0`), so this note is obsolete regardless of platform, not just a Windows-specific claim. Also fix these now-stale lines found by inspection: the opening tagline "built with [Fyne](https://fyne.io/)"; the Controls table's `Home | Rescan content directory and show first slide` row (`Home` no longer rescans — jump-to-first only); "Ratpoison will automatically fit it to screen" (no ratpoison); the "scaled to the window" / `fyne.Do()` lines in How It Works (replace with the `Renderer`/run-loop description); and the `go run .` development instructions, which now need the `-headless` flag mentioned or they fail loudly on a desktop machine with no free VT.

### Layer: Hardware Validation

- [ ] Step 32: **Gate: confirm the board-recovery plan** (spare SD card, serial console, or otherwise) before proceeding — do not run Step 33 without it, per Analyst.md's carried prerequisite.
- [ ] Step 33: On `pi3b.local`, run the built binary through: normal DRM `Present`/blank cycle, a real VT switch-away/switch-back (`chvt`), `SIGTERM` and `SIGKILL` console-recovery, and key input from the real three-key device or a USB keyboard as a stand-in (Left/Right/Home nav, Delete sleep, Escape quit, any-key-wakes) — recording actual keycodes seen (spike item 3) and RSS footprint (spike item 4).
