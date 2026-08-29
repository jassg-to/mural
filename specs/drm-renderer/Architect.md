# Architect: Native Rendering Layer (DRM/KMS + evdev)

> Phase 2 — Design decisions. Approved before coding begins.
> Implementation checklist is in tasks.md.

## Approach

Mural stays a single `package main`, flat-file Go program — the existing
convention (`main.go`, `slideshow.go`, `cec.go`, `schedule.go`, all `package
main`, no `internal/` split) is followed rather than introduced. Seven new
files are added; `slideshow.go` and `main.go` are rewritten in place;
`cec.go` and `schedule.go` are untouched, exactly as Analyst.md predicted.

**The central seam is a two-method `Renderer` interface**, consumer-defined in
`slideshow.go` per the project's own "interfaces at the consumer side, 1-2
methods" convention:

```go
// Renderer is the display sink Slideshow presents composited frames to.
// Present(nil) blanks the display to black.
type Renderer interface {
    Present(frame *image.RGBA) error
    Close() error
}
```

Two implementations satisfy it: `*DRMRenderer` (real hardware, `drm.go`) and
`*HeadlessRenderer` (dev-loop frame dump, `headless.go`). This is the "second
output path" Analyst.md explicitly reserved for the developer's-need-to-run-
Mural-off-the-sign requirement — not a portability seam, since both
implementations are Linux-only and neither is chosen by `GOOS`.

**Renderer selection is an explicit flag, not hardware auto-detection.** A new
`-headless` bool flag on `main.go` picks `HeadlessRenderer`; its absence
always attempts `DRMRenderer` and fails loudly if that fails. This is a
deliberate correction against the more obvious "fall back to headless if
`/dev/dri` is missing" design: Analyst.md's edge-case table is explicit that
**a missing or unopenable `/dev/dri` on the sign must be a loud, diagnosable
startup failure, not a silent fallback** ("No `/dev/dri` device present at
all", "Mural lacks permission on the DRM or input device", "Another process
already holds DRM master" — all specified as hard failures, not degraded
modes). Auto-detecting off device presence would silently convert every one
of those into a quiet headless run instead of the loud failure the spec
requires. An explicit flag makes the two situations — "the sign, DRM must
work" vs. "a developer's machine, DRM is not expected" — a decision the
invoker states, not one Mural guesses.

**Output size is discovered once at startup, not queried per frame.** Both
constructors return the display's pixel size directly:

```go
func OpenDRMRenderer(devicePath string) (r *DRMRenderer, width, height int, err error)
func NewHeadlessRenderer() (r *HeadlessRenderer, width, height int)
```

This replaces the `winSize func() fyne.Size` indirection and the deferred
first-`show` workaround outright — `Slideshow` receives `width, height` once
in `NewSlideshow` and never asks again, matching Analyst.md's "output
dimensions are now known and fixed at startup" behaviour note.
`HeadlessRenderer` has no real connector to discover a mode from; it reports a
fixed `1920x1080` — arbitrary and dev-only, documented as such at the call
site rather than made configurable, since nothing in this feature needs it to
be.

**Compositing is a pure, hardware-independent function**, split out precisely
because Analyst.md calls it out as needing coverage without hardware:

```go
// compositeLetterboxed returns a w×h RGBA frame with img centered and
// letterboxed against black. img == nil (corrupt source, or a blank
// request) produces a solid black frame — this is Mural's own definition
// of "corrupt image's frame", settled here per Analyst.md's open item.
func compositeLetterboxed(img image.Image, w, h int) *image.RGBA
```

Both renderers consume its output: `DRMRenderer.Present` converts the RGBA
frame into the dumb buffer's `XRGB8888` layout and page-flips it;
`HeadlessRenderer.Present` PNG-encodes it directly. `Present(nil)` is defined
to mean "blank" at the interface level; each renderer turns that into
`compositeLetterboxed(nil, w, h)` internally, so `Slideshow.pause()` becomes
one call — `s.renderer.Present(nil)` — with no separate `Blank` method needed.

**The concurrency model is a single run-loop goroutine**, replacing every
`fyne.Do` marshalling point named in Delta.md as "the quietest risk in the
feature." `Slideshow.Run()` becomes one `for { select { ... } }` over:
advance-timer fires, key events from `input.go`'s channel, pause/reload
commands (schedule.go's callbacks now post into a small command channel
instead of calling `fyne.Do` directly), and VT-switch/shutdown signals from
`vt.go`. Every field `fyne.Do` used to protect (`current`, `paused`,
`advanceTimer`) is now touched only inside this one goroutine by construction
— no lock, no atomic, no dispatcher, mirroring the old "only ever touched on
the Fyne main goroutine" rule but enforced by a single loop instead of by
convention. The background-decode generation counter (`atomic.Int64`) is kept
exactly as-is: its job — discarding a stale decode result — is unchanged by
the concurrency rewrite, and its result is delivered back into the run loop
over the same command channel rather than via `fyne.Do`.

**Key-event handling in the run loop preserves today's exact precedence**,
per the user's decision to keep the full five-binding input arrangement
rather than the three-key-only design this phase originally settled on:
`NavQuit` (Escape) and `NavSleep` (Delete) are handled first and
unconditionally — quit tears the run loop down and exits regardless of pause
state; sleep calls the same `pause()` regardless of pause state — and neither
counts as the key that resumes a paused sign. Every other case (`NavLeft`,
`NavRight`, `NavHome`, and the catch-all `NavWake` for any other recognized
key) resumes first if the sign is paused, then — for `NavLeft`/`NavRight`/
`NavHome` only — performs its navigation. This is a straight port of
`main.go`'s existing `SetOnTypedKey` switch structure onto the new channel-
based loop, not a redesign: the ordering (non-nav keys checked and returned
on before the pause check) is what makes `Escape`/`Delete` bypass resume, and
getting that ordering wrong is the easiest way to silently reintroduce the
overloading the original navigation-only decision was trying to avoid.

**DRM ioctls are hand-written**, per Analyst.md's finding that no mature Go
DRM library exists. `drm_ioctl.go` isolates the raw `ioctl` request numbers,
C-struct-layout Go structs, and two pure helpers pulled out specifically for
hardware-independent testing: `pickPreferredMode([]drmModeInfo) drmModeInfo`
(connector mode selection — index 0 per the kernel's own ordering, confirmed
against the real board's `modetest -c` output during the Phase 1 spike) and
`rgbaToXRGB8888(dst []byte, pitch int, src *image.RGBA)` (the pixel-format
conversion). `drm.go` holds the actual `DRMRenderer` — `SET_MASTER`, connector
enumeration, dumb-buffer create/map ×2, `SET_CRTC`, page-flip-and-wait — built
directly on top of those two files, following the same open→master→mode→dumb-
buffer→page-flip sequence already proven working on `pi3b.local` by the
`kmsmove.c` spike (300 flipped frames in 5.00s at 59.95fps, clean release
after).

**VT-switch and console-recovery handling is its own file** (`vt.go`) because
it is orthogonal to rendering itself: it owns `SIGUSR1`/`SIGUSR2` (VT
release/acquire, via `VT_SETMODE(VT_PROCESS)` on the controlling tty) and
`SIGTERM`/`SIGINT` (graceful shutdown — `Close()` then restore the console to
text mode). It is wired up only when `DRMRenderer` is selected;
`HeadlessRenderer` has no VT to own. `SIGKILL` cannot be handled by design —
Analyst.md is explicit that this is the case a signal handler structurally
cannot cover — and the Phase 1 spike already observed the fallback this
depends on empirically: after both a normal exit and a killed backgrounded
process, `/dev/dri/card0` came back unheld and `vc4` logged no errors, with no
explicit cleanup code running. That is the kernel's own fd-close teardown of
DRM master, not application code, and `vt.go`'s job is only to make the
*graceful* paths (`SIGTERM`, `VT_PROCESS` release) restore the console
proactively rather than relying on that same implicit teardown.

**Input is a self-contained subsystem** (`input.go`) with no dependency on
rendering: it enumerates `/dev/input/event*`, decodes raw `input_event`
structs, and watches `/dev/input` via `inotify` for hotplug — on any device,
not a specific one, matching the "an ordinary USB keyboard's Left/Right/Home
navigate" edge case, now extended to Escape/Delete/any-key-wakes as well.
`input.go` itself only recognizes keys and names them; it holds no pause-state
logic. `NavKey` has six values: `NavLeft`, `NavRight`, `NavHome`, `NavSleep`
(Delete), `NavQuit` (Escape), and a catch-all `NavWake` for every other
recognized key-down, which exists solely so the run loop can implement "any
key wakes a paused sign" without `input.go` needing to know what "paused"
means. Parsing a raw byte buffer into a `NavKey` is a pure
function, tested without a real device; the enumeration/inotify/goroutine-
per-device plumbing around it is not, and is exercised on-hardware in the
final validation step.

## Package Contracts

| Contract | Shape | Defined in | Consumed by |
|---|---|---|---|
| `Renderer` | `Present(*image.RGBA) error`, `Close() error` | `renderer.go` | `slideshow.go` |
| `OpenDRMRenderer` | `(devicePath string) (*DRMRenderer, width, height int, err error)` | `drm.go` | `main.go` |
| `NewHeadlessRenderer` | `() (*HeadlessRenderer, width, height int)` | `headless.go` | `main.go` |
| `compositeLetterboxed` | `(img image.Image, w, h int) *image.RGBA` | `compositor.go` | `drm.go`, `headless.go` |
| `NavKey` + `Events()` | `type NavKey int` (`NavLeft`/`NavRight`/`NavHome`/`NavSleep`/`NavQuit`/`NavWake`); `Events() <-chan NavKey` | `input.go` | `slideshow.go` (run loop) |
| VT/shutdown signals | internal channel of a small `vtEvent` enum, fed into the run loop | `vt.go` | `slideshow.go` (run loop) |

No HTTP/RPC contract applies — this is a single embedded process, not a
client/server split.

## Data Model Changes

No schema or config-file changes. `config.toml`'s `[schedule]` and
`[slideshow]` sections are untouched, per Analyst.md's non-goal.

New in-memory types only:
- `NavKey int` (`input.go`) — `NavLeft`, `NavRight`, `NavHome`, `NavSleep`, `NavQuit`, `NavWake`
- `drmModeInfo`, `drmBuffer` and the raw ioctl request/struct types
  (`drm_ioctl.go`) — internal to the DRM backend, not exposed past `drm.go`
- `Renderer` (`renderer.go`) — the interface itself

`Slide.thumb` and `decodeAndFit`'s return type stay `image.Image`; nothing
about `Slide`, `scanSlides`, or the config-driven fields on `Slideshow`
changes shape.

## Files to Create

| File | Purpose |
|---|---|
| `compositor.go` | `compositeLetterboxed` — pure letterbox/black-frame compositing, hardware-independent |
| `renderer.go` | The `Renderer` interface definition only |
| `drm_ioctl.go` | Raw DRM ioctl constants/structs; pure helpers `pickPreferredMode`, `rgbaToXRGB8888` |
| `drm.go` | `DRMRenderer`: `OpenDRMRenderer`, `Present`, `Close`, double dumb-buffer management, page-flip |
| `vt.go` | VT-switch (`SIGUSR1`/`SIGUSR2`) and graceful-shutdown (`SIGTERM`/`SIGINT`) handling for `DRMRenderer` |
| `headless.go` | `HeadlessRenderer`: PNG-dumps composited frames to a fixed path for dev-loop inspection |
| `input.go` | evdev enumeration, raw `input_event` decode into six-value `NavKey` (`NavLeft`/`NavRight`/`NavHome`/`NavSleep`/`NavQuit`/`NavWake`), hotplug via `inotify`, `NavKey` channel |

## Files to Modify

| File | Change |
|---|---|
| `slideshow.go` | Drop all Fyne imports/usage; adopt `Renderer`, single-goroutine run loop with today's exact key precedence (`NavQuit`/`NavSleep` act unconditionally and don't resume; `NavLeft`/`NavRight`/`NavHome`/`NavWake` resume a paused sign first), int-sized `decodeAndFit` via `golang.org/x/image/draw`, corrupt-image black-frame behaviour via `compositeLetterboxed(nil, ...)` |
| `main.go` | Add `-headless` flag; construct `DRMRenderer` (default, fails loudly) or `HeadlessRenderer` (on flag); wire `input.go` and, for `DRMRenderer` only, `vt.go`; handle `NavQuit` as a clean shutdown (renderer `Close()`, VT/console restore, process exit); remove all `fyne/app`/window setup |
| `slideshow_test.go` | Replace `newHeadlessSlideshow`'s Fyne `test.NewApp()`/`canvas.Image` with a `fakeRenderer` implementing `Renderer`; adapt the four Fyne-coupled tests to assert against it; add tests for `compositeLetterboxed`, `pickPreferredMode`, `rgbaToXRGB8888`, `input.go`'s raw-event parser, and the `NavQuit`/`NavSleep` bypass-resume precedence |
| `go.mod` / `go.sum` | Remove `fyne.io/*` and its indirect closure and `github.com/nfnt/resize`; promote `golang.org/x/image` and `golang.org/x/sys` to direct requires; `go mod tidy` |
| `install.sh` | Stop installing `xinit`, `ratpoison`, `libgl1`, `unclutter`, `x11-xserver-utils`; stop writing `.xinitrc`/`.ratpoisonrc`; add `video`/`input` group membership for the kiosk user; kiosk autologin execs the Mural binary directly instead of `startx` |
| `docs/INSTALL.md` | Remove the `startx` step and X11-era instructions; document the direct-autologin flow and the `-headless` dev flag |
| `.github/workflows/build.yaml` | Drop the "Install native GL/X11 headers" and dpkg-multiarch cross-compilation steps entirely; set `CGO_ENABLED: "0"`; drop `CC`/`PKG_CONFIG_PATH` |
| `CLAUDE.md` | Rewrite Tech Stack (drop Fyne/CGo), Build Notes (drop the CGo first-build note), Architecture Notes (replace `fyne.Do`/`canvas.Image` rules with the `Renderer`/run-loop/evdev/DRM description), Conventions (drop the Windows aspiration line) |
| `README.md` | Remove the Windows/MSYS2/TDM-GCC build section and cross-platform claim |

## Dependencies Between Steps

- `compositor.go` has no dependencies — build and test it first.
- `renderer.go` depends only on the frame type (`*image.RGBA`) `compositor.go` produces.
- `drm_ioctl.go` is independent of both — pure constants/structs/helpers.
- `drm.go` depends on `drm_ioctl.go` (primitives) and `renderer.go` (interface it implements).
- `headless.go` depends on `renderer.go` and `compositor.go`; independent of `drm.go`/`drm_ioctl.go`.
- `vt.go` depends on `drm.go` (operates on `*DRMRenderer` specifically).
- `input.go` is independent of every rendering file.
- `slideshow.go`'s rewrite depends on `renderer.go`, `compositor.go`, and `input.go`'s `NavKey` type.
- `main.go`'s rewrite depends on everything above — it is the wiring point, done last among code files.
- `slideshow_test.go`'s rewrite depends on the rewritten `slideshow.go` (needs its new shape to write `fakeRenderer` against) and on `compositor.go`/`drm_ioctl.go`/`input.go` for their pure-function tests.
- `go.mod`/`go.sum` cleanup depends on `slideshow.go` and `main.go` no longer importing Fyne or `nfnt/resize` — done after both.
- Packaging/docs files (`install.sh`, `docs/INSTALL.md`, `.github/workflows/build.yaml`, `CLAUDE.md`, `README.md`) have no compile-time dependency on the Go code, but should follow it so they describe what was actually built rather than what was planned.
- On-hardware validation is last, and per Analyst.md's carried prerequisite, must not run until the board-recovery plan is confirmed — see Risks below.

## Parallelisation Opportunities

- `compositor.go`, `drm_ioctl.go`, and `input.go` touch disjoint files and have no dependency on each other — safe to build concurrently.
- `drm.go` and `headless.go` both depend on `renderer.go`/`compositor.go` but not on each other — safe to build concurrently once those two exist.
- `vt.go` must wait for `drm.go` — not parallelisable with it.
- The five packaging/docs files (`install.sh`, `docs/INSTALL.md`, `.github/workflows/build.yaml`, `CLAUDE.md`, `README.md`) touch entirely disjoint files — safe to run as a parallel batch, ideally after the Go-side design is settled so they describe the real thing.
- **File lock:** `slideshow.go` is touched by exactly one step (the rewrite) and `slideshow_test.go` by exactly one step (its own rewrite) — no two steps touch either concurrently.

## Risks & Open Questions

- **Key-precedence ordering is a small, easy-to-get-wrong piece of logic with
  an outsized consequence.** The whole point of keeping `Escape`/`Delete`
  unconditional and excluded from resume-triggering is to reproduce today's
  exact behaviour; a coder who instead runs every key through a uniform
  "resume then act" path would silently reintroduce dual-purpose behaviour
  the navigation-only decision spent real effort avoiding. Worth a specific
  look in Phase 3 and a specific test in Phase 4 (paused sign, press Escape,
  assert no resume occurred; same for Delete).
- **Hand-written ioctl correctness is still the single largest risk**, as Analyst.md flagged. The Phase 1 spike (`kmsmove.c`) proved the *sequence* works on real hardware in C; translating it to Go via `golang.org/x/sys/unix` raw ioctls carries its own transcription risk (struct layout/alignment, ioctl request-number encoding) that the spike does not retire.
- **VT-switch handling is unverified.** The spike exercised normal-exit and killed-process console recovery, not an actual `Ctrl-Alt-F<n>` switch-away/back cycle. `vt.go`'s `SIGUSR1`/`SIGUSR2` path needs its own on-hardware pass before it can be trusted.
- **The board-recovery plan is still open.** Today's fresh Trixie image on `pi3b.local` is a known-good *working* image, not a demonstrated *spare* — there is still one board and no confirmed second SD card. Per Analyst.md's carried prerequisite, this must be settled (spare card, serial console, or restored remote access) before any VT-switch or crash-path testing that risks leaving the board in an unreachable state. The user has stated Tailscale is out of scope for this project, which narrows the realistic options to a spare card or a serial console.
- **Spike items 3 (input identity) and 4 (footprint) have not run yet.** Only item 1 (DRM bring-up) and part of item 2 (console recovery on normal exit / kill) have empirical results so far. `input.go`'s design assumes standard `KEY_LEFT`/`KEY_RIGHT`/`KEY_HOME` codes from the real three-key device; this is unconfirmed.
- **The explicit `-headless` flag (vs. auto-detection) is a judgement call Critic should re-examine.** It is chosen specifically to satisfy the loud-failure edge cases in Analyst.md, but it does mean a developer must remember to pass the flag — an easy thing to forget and then be confused by a loud DRM failure on a laptop. Mitigation considered and rejected: detecting "known dev machine" some other way is more complexity for less clarity than one documented flag.
- **`x/image/draw`'s `CatmullRom` vs. `nfnt/resize`'s `Lanczos3`** is a visible, if likely imperceptible, output change — carried over from Analyst.md/Delta.md, not newly introduced here.
- **The single-goroutine run loop removes the need for `fyne.Do`, but is itself unverified under real concurrent load** (nav key arriving in the same instant as an advance-timer fire, a VT-switch signal arriving mid-decode) until exercised on hardware.

## Architect Checklist

- [x] Approach fits existing project patterns — flat `package main`, no new packages, consumer-side interfaces at 1-2 methods, exported types / unexported helpers matching existing style
- [x] Package contracts defined before any code
- [x] No schema changes — explicitly none, config file untouched
- [x] Device-permission and loud-failure requirements carried into the design (`-headless` flag decision exists specifically because of them)
- [x] No step requires modifying multiple unrelated files
- [x] Parallelisation opportunities and file locks declared
- [x] No mutation/event-notification concerns apply — single local process, no external consumers of state changes
