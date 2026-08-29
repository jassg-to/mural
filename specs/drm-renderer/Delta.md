# Delta: Native Rendering Layer (DRM/KMS + evdev)

> Specification delta — what changes relative to the current system.
> Only exists when this feature modifies existing behaviour.

**Three decisions have since been settled by the user and are written into this
delta throughout:** the input arrangement is **Left = previous, Right = next,
Home = first slide, Delete = sleep the display, Escape = quit** — the same five
bindings as today, minus `Home`'s second job of triggering a `Reload`;
**Windows support is dropped entirely**; and **slide-to-slide animation,
including a simple crossfade, is moved out of this feature's scope entirely**
— deferred to a separate future feature rather than built here. No hedging
against the alternatives remains.

**Decision recorded: full replacement of the presentation layer.** Mural drops
Fyne, X11, `ratpoison`, `unclutter`, and CGo and replaces them with a hand-rolled
pure-Go display and input path. The decision was made by the user before this
phase, on the strength of a hardware spike and an explicit directive — *"I'm not
married to X or Fyne. I'm looking for aggressive optimization since resources are
so scarce."* This delta is written to that decision and does not hedge against
the alternatives.

Two things are worth noticing about the shape of this delta before reading it.

First, **the viewer-facing delta is empty, and the operator- and
developer-facing delta is enormous.** Someone standing in front of the sign
should see the same images, in the same order, letterboxed the same way, swapped
the same instantaneous way, on the same schedule. Every change in this document
happens behind that.

Second, **transitions are not part of this delta.** The removed video feature
was really being used for slide transitions, and that want is real — but the
user has since decided it is a separate future feature, not a capability this
rewrite restores. This delta replaces the rendering and input layers on a
like-for-like basis and adds no viewer-visible capability at all.

## ADDED

- **Direct ownership of the display.** Mural becomes the process that sets the
  video mode and drives the scanout, a responsibility it has never held. Mode
  discovery, buffer allocation, page-flip, and pixel-format handling all become
  Mural's code.

- **Direct ownership of input.** Mural reads the kernel's input devices itself.
  **There is no input handling anywhere in the repository today** — Fyne supplied
  all of it — so this is entirely net-new: device discovery under
  `/dev/input/event*`, decoding raw `input_event` structures, key-state
  handling, and hotplug survival.

- **VT-switch handling.** Dropping DRM master when the console is taken away and
  reacquiring it and re-setting the mode when it returns. *Previously performed
  for Mural, invisibly, by X. It is a cost of having no windowing system rather
  than a cost of any particular design choice, and it does not go away by
  choosing a different backend.*

- **Console restoration on exit, crash, and signal.** Today X restores the VT, so
  a Mural crash leaves a usable machine. That safety net is being removed and
  must be rebuilt. **On a board whose remote access is already known to be
  unreliable, this is the difference between a log line and a car journey.**

- **Device permission requirements as explicit deployment configuration.**
  Membership in `video` and `input`, or equivalent `udev` rules, for the
  unprivileged kiosk user. *A new instance of a failure mode the NixOS spec
  already documents for CEC: everything looks correctly configured and nothing
  happens.*

- **Defined behaviour for a corrupt image's frame.** A corrupt file still stays
  in the rotation, unchanged — but with no toolkit rendering a `nil` image, what
  appears in its place becomes Mural's decision for the first time and must be
  chosen rather than inherited.

- **A measurable resource budget as an acceptance criterion.** Footprint has
  never been a stated requirement of this project. It is now the feature's
  primary justification, which makes it something to measure and gate on rather
  than hope for.

- **A development-time way to run Mural off the sign.** Currently free — `go
  run .` opens a window on the developer's machine. Under DRM and `evdev` that
  stops working inside a desktop session, so the ability to run the program at
  all becomes something the feature must deliberately provide. *Listed under
  ADDED rather than REMOVED because the requirement is new even though the
  capability is old.*

  **This survives the Windows decision untouched, and is now the only thing that
  could justify a second output path.** The two questions were entangled — a
  Windows backend might have doubled as the development one — and separating them
  clarifies rather than resolves: dropping Windows removes a *reason* to build a
  portability seam, not the developer's need to run the program somewhere other
  than the sign. *A headless sink that writes frames to disk for inspection is the
  cheap answer and drags no toolkit back in.*

## MODIFIED

- **Display output goes through a GUI toolkit on top of a windowing system** →
  **Mural writes pixels to the display hardware directly.** The stack
  Xorg → `ratpoison` → `unclutter` → Fyne → OpenGL 2.1 → `canvas.Image` collapses
  to a single process with a mapped buffer. *This is the feature. Everything else
  in this document is a consequence of it.*

- **Input arrives as `fyne.KeyEvent` through a canvas key handler** → **input is
  read as raw kernel events from the device.** The handler in `Run()` is the only
  input code that exists today and it is replaced entirely.

- **Key bindings are `Right`/`Left`/`Home`/`Delete`/`Escape`, with `Home`
  overloaded** → **the same five bindings, minus `Home`'s second job.** Left =
  previous slide, Right = next slide, Home = first slide, Delete = sleep the
  display, Escape = quit — unchanged from today. Only one behaviour is dropped:
  `Home` no longer also triggers a `Reload`.

  *This phase originally planned to drop `Delete` and `Escape` too, on the
  reasoning that they were debug scaffolding and that `Escape` in particular
  let anyone at an attached keyboard kill an unattended sign. The user has
  since revised that: both are kept, and the kiosk-hazard tradeoff `Escape`
  reopens is accepted knowingly rather than avoided. `Home`'s double duty
  (jump to slide 0 **and** trigger a `Reload`) is the one piece of the original
  plan that still stands — the user did not ask for it back.*

  **Precedence is unchanged from today.** `Escape` and `Delete` act
  immediately regardless of pause state and are not themselves a resume
  trigger; every other key — `Left`, `Right`, `Home`, or anything unbound —
  resumes a paused sign first if one is paused.

  **Resume-from-pause survives intact.** Today any non-`Escape`/`Delete` key
  resumes; that is unchanged.

- **The output size is discovered from the toolkit after a layout pass** →
  **the output size is the display's mode, known at startup.** This removes the
  `winSize func() fyne.Size` indirection and the deferred first `show` that
  exists only because Fyne reports a placeholder size on the first frame. *Two
  workarounds disappear rather than being ported.*

- **Off-thread work is marshalled with `fyne.Do`** → **the concurrency model is
  Mural's own.** The `atomic.Int64` generation counter that discards stale
  decodes is sound and its *intent* carries over — but every rule in `CLAUDE.md`
  about which goroutine may touch what is written in terms of the Fyne main
  goroutine, and all of it is rewritten. **This is the quietest risk in the
  feature**: the existing marshalling rules are load-bearing, subtle, and
  documented as prose rather than enforced by types.

- **Scaling uses `github.com/nfnt/resize` with Lanczos3** → **scaling uses
  `golang.org/x/image/draw`.** Already an indirect dependency; can scale,
  letterbox, and blit in one call with no intermediate allocation. `nfnt/resize`
  is unmaintained since 2018 and is dropped regardless of the rest of this
  feature. *Output is not pixel-identical — `CatmullRom` is not Lanczos3 — which
  is almost certainly imperceptible on photographic signage content but is a
  real change to what appears on screen and should be acknowledged, not slipped
  in.*

- **Thumbnails are small previews sized against a toolkit canvas** → **their
  purpose survives, their sizing is re-examined.** Showing *something* instantly
  while the full decode runs is still right. But `thumb_width`'s 80px default was
  chosen against a Fyne canvas, and the target size is now fixed and known at
  startup rather than discovered after a layout pass.

- **The build requires CGo and a C toolchain** → **the build is pure Go and
  static.** `CGO_ENABLED=0`, no GL or X11 development headers, no `dpkg`
  multiarch for cross builds. Binary size goes from ~30-40MB dynamically linked
  to a target of ~5MB static. *The 20-minute first-build penalty documented in
  `CLAUDE.md` disappears along with it — a build-time win that shows up on every
  developer machine and every CI run, not just on the sign.*

- **CI cross-compiles against per-architecture C libraries** → **CI sets
  `GOARCH` and nothing else.** `.github/workflows/build.yaml` currently installs
  `libgl1-mesa-dev`, `libx11-dev`, `libxrandr-dev`, `libxinerama-dev`,
  `libxcursor-dev`, `libxi-dev`, and `libxxf86vm-dev` with `dpkg` multiarch for
  three architectures. **All of it goes.** *One of the clearest unambiguous wins
  in the feature.*

- **The release binary is a dynamically linked ELF tied to its build
  distribution** → **it is a static binary that runs on any Linux of the right
  architecture.** *This is precisely the constraint `specs/nixos-deployment`
  identified as the reason the published binaries cannot run on NixOS. Removing
  it does not resolve that spec — which chose to build from source for other,
  still-valid reasons — but it does remove the hard blocker underneath it. The
  two specifications must be reconciled by whichever ships second.*

- **The kiosk session is autologin → `startx` → `ratpoison` → Mural** →
  **the kiosk session is autologin → Mural.** `install.sh` stops installing
  `xinit`, `ratpoison`, `libgl1`, `unclutter`, and
  `x11-xserver-utils`, and stops writing `.xinitrc` and `.ratpoisonrc`.
  `docs/INSTALL.md` stops telling operators to type `startx`. *This collides
  directly with `specs/nixos-deployment`, which is retiring `install.sh`
  altogether. The two must not edit the same file in conflict, and the sequencing
  is an open question.*

- **Four of the six tests exercise player behaviour through a headless Fyne
  harness** →
  **they exercise it through whatever seam the new layer provides.**
  `TestSlideshowPauseStopsTimerAndBlanksImage`,
  `TestSlideshowResumeRearmsTimer`, `TestSlideshowNavKeysRearmTimer`, and
  `TestSlideshowImageAdvanceTimingUnchanged` construct a `canvas.Image` via
  `newHeadlessSlideshow` and cannot survive as written. **Re-expressing them is
  in scope; dropping them is not** — they assert exactly the guarantees a
  rendering rewrite breaks quietly. *`TestIsImageExt` and `TestScanSlidesImages`
  are toolkit-agnostic and carry over unchanged.*

- **`CLAUDE.md`'s Tech Stack, Build Notes, and Architecture Notes describe a
  Fyne application** → **they describe a direct-rendering one.** Most of the
  Architecture Notes section is invalidated: the `fyne.Do` rules, the
  `canvas.Image` description, the GUI and CGo lines, and the Windows build-time
  note. *Not cosmetic — those notes are how the next person picks this codebase
  up cold, and a stale one is worse than none.*

## REMOVED

- **Fyne v2**, and with it the entire GL and GLFW dependency tree: `fyne.io/
  fyne/v2`, `fyne.io/systray`, `github.com/go-gl/gl`, `github.com/go-gl/glfw`,
  `github.com/fyne-io/*`, `github.com/go-text/*`, and the rest of the toolkit's
  indirect closure. **The `go.mod` shrinks from roughly 35 dependencies to a
  handful**: `BurntSushi/toml`, `golang.org/x/image`, `golang.org/x/sys`.

- **CGo, and the requirement for a C toolchain to build Mural at all.**

- **X11 in every form** — the X server, `ratpoison`, `unclutter`,
  `x11-xserver-utils`, `libgl1`, `xinit`, and `startx` as the documented launch
  gesture. *Four separate processes cease to exist on the sign; three of them
  existed solely to make a fifth one behave like a fullscreen image viewer.*

- **`github.com/nfnt/resize`.** Unmaintained since 2018.

- **The manual content-reload gesture.** `Home` currently triggers a `Reload`
  alongside its jump-to-first-slide behaviour, and under the pure-navigation
  decision it no longer will. Content refresh now happens at scheduled turn-on,
  on the daily config reload, and — once `specs/usb-stick-hotplug` is built — on
  stick insertion.

  *A real reduction in what an operator standing at the sign can do, and a direct
  consequence of the "no dual-purpose behaviour" instruction rather than an
  oversight. Recorded here rather than dropped silently. SSH is the answer for
  anyone who needs to force a reload, and restoring the gesture later would be a
  small follow-up, not a redesign.*

- **The `winSize` indirection and the deferred-first-`show` workaround.** Both
  exist only to work around Fyne reporting a pre-layout placeholder size. With
  the mode known at startup, there is nothing left to work around.

- **The toolkit's implicit console management.** X currently handles VT
  arbitration, cursor hiding, and mode restoration on exit. None of it is
  replaced by something equivalent; all of it becomes Mural's own explicit code.
  *Listed under REMOVED rather than MODIFIED deliberately: what is being lost is
  a safety net that was never noticed because it never failed.*

- **Windows support, and the project's stated cross-platform intent.**
  `CLAUDE.md` says *"Target platform is Linux but we want to support Windows
  too."* Fyne provided that for free; DRM and `evdev` have no Windows equivalent.
  **The user has decided to drop it outright** — no dual backend, no build-tagged
  toolkit fallback, no portability seam maintained on spec. Mural is a Linux
  program for this class of board.

  *This reads as the sharpest thing the feature removes and is in practice one of
  the cheapest. `.github/workflows/build.yaml` was inspected during this phase:
  the build matrix is exactly `linux/amd64`, `linux/arm64`, and `linux/arm`, with
  `GOOS: linux` hardcoded. **No Windows artifact has ever been built, published,
  or tested.** What is retired is an intention in `CLAUDE.md` and a set of
  MSYS2/TDM-GCC build instructions in `README.md` — not a platform anyone runs
  on, and not one line of CI.*

  The consequence that matters is architectural rather than operational: with no
  second platform to serve, **Phase 2 should not build a backend abstraction on
  portability grounds.** *`CLAUDE.md`'s conventions section and `README.md`'s
  build prerequisites both need rewriting to say so plainly, so the next reader
  does not find a stated intention the code cannot honour.*

- **The ability to run Mural inside a normal desktop session.** A consequence of
  the same change, and the one that hurts daily rather than eventually: the
  developer's inner loop disappears at the exact moment the code becomes the
  hardest it has ever been to debug, on a project with one remote test board and
  unreliable access to it. *The compensating requirement is recorded under
  ADDED.*

- **Any dependency on the GPU or on a working GL stack.** Composition is on the
  CPU, but with no animation in this feature, that composition is a single
  letterboxed blit per slide change rather than anything sustained. A clean
  simplification — it makes `specs/nixos-deployment`'s `hardware.graphics.enable`
  / Mesa / `/run/opengl-driver/lib` trap irrelevant, and retires that spike's
  Criterion 2 finding that VC4 V3D 2.1 exactly matches Fyne's GL 2.1 binding.
  *Unlike an animated build of this feature, there is no sustained per-pixel
  blending workload here to measure — the CPU cost this feature actually takes
  on is close to nothing.*
