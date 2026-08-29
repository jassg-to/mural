# Analyst: Native Rendering Layer (DRM/KMS + evdev)

> Phase 1 — Problem definition. Approved before architecture begins.

*The directory name `drm-renderer` is shorthand. The feature is the replacement
of Mural's entire presentation layer — display output and input handling — not
a display backend in isolation. Slide-to-slide animation is explicitly out of
scope; see* Scope *below.*

## Goal / Outcome

**Scope classification: Complex**

Mural is a Fyne v2 application. What it actually asks of Fyne is one fullscreen
window, one black rectangle, one `canvas.Image` whose contents it swaps, and one
key handler. That is the whole of it — `slideshow.go` is the only file that
imports the toolkit at all.

What that costs, on the hardware this project actually runs on, is
disproportionate: a CGo build requiring a C toolchain and GL/X11 development
headers, an OpenGL 2.1 context, an X server, a window manager (`ratpoison`), a
cursor-hider (`unclutter`), a dynamically linked binary that cannot run on a
distribution it was not built against, and **a measured ~218MB RSS on the
deployed Pi 3B+** — a board with 905MB usable RAM. Mural is paying for a
general-purpose GUI toolkit and using approximately none of it.

**The user has decided to stop paying.** Mural drops Fyne, X11, `ratpoison`,
`unclutter`, and CGo entirely, and replaces them with a hand-rolled pure-Go
presentation layer: direct DRM/KMS (or framebuffer) output and a hand-written
`evdev` input reader, with no GUI toolkit of any kind. The direction is explicit
and is not re-opened here — *"I'm not married to X or Fyne. I'm looking for
aggressive optimization since resources are so scarce."* This phase defines what
that replacement must achieve and what it must not break; it does not design it,
and it does not re-argue it.

Three separate pressures converge on this decision, and it is worth recording
all three, because a plan that serves only the first will build the wrong thing:

1. **Resource scarcity, as direct user instruction.** The board is a Pi 3 Model
   B Plus: quad-core Cortex-A53, 905MB usable RAM, VC4 V3D 2.1, running full KMS
   (`dtoverlay=vc4-kms-v3d`, `vc4-drm` bound across hvs/hdmi/txp/pixelvalve/v3d
   — confirmed on the actual deployed board, not inferred). Every megabyte and
   every watt spent running a windowing system to display one image is a
   megabyte and a watt that buys nothing.

2. **Transitions are a known future want, and are explicitly not being built
   here.** Mural briefly supported video playback and it was just removed
   wholesale (`video.go` and its tests deleted; `slideshow.go` and `main.go`
   reverted to images only), because a hardware spike found Mural's in-process
   frame-streaming had severe frame jitter on this board. The removal exposed
   what that feature was *for*: the video files were never general-purpose
   video. They were **slide transitions** — *"just a fancy slideshow with rich
   transitions"*, in the user's own words. That want is real, but the user has
   since decided to keep it out of this feature: this rewrite replaces the
   rendering and input layers on a like-for-like basis, with the auto-advance
   and manual-navigation swap staying exactly as instantaneous as it is today.
   Animation of any kind — crossfade included — is a separate future feature,
   scoped on its own once this layer exists to build it on.

3. **Navigability is a hard project goal, and the old approach broke it.**
   There is a physical three-key input device, and it — not a keyboard — is the
   real interface to a deployed sign for day-to-day navigation. A
   video-as-transition was an opaque stream that played through; you could not
   navigate out of the middle of it the way you can step off an image slide.
   That is part of why animation is being kept out of this feature rather than
   rebuilt inside it — whenever transitions are eventually designed, they must
   not reintroduce a state that navigation cannot interrupt. For now, with no
   transition in play, **the sign must be navigable at every instant** simply
   means what it does today: a key press takes effect immediately, slide to
   slide, with nothing in between.

   The three keys on the physical remote have been defined by the user: **Left
   = previous slide, Right = next slide, Home = jump to the first slide.**
   The user has since decided to also keep the rest of the existing keyboard
   bindings rather than treat them as debug scaffolding to be dropped — see the
   navigation-interface rule in *Rules & Constraints* for the full, revised set
   of five bindings.

Reframed honestly, Mural is a fullscreen image compositor with a timer, a
schedule, and a three-key remote. That job does not need a GUI toolkit, and on
this hardware it cannot afford one.

**Target outcome:** the same visible behaviour a viewer sees today — no
animation added, none removed — with no windowing system, no C toolchain, a
single static binary in the neighbourhood of 5MB (against today's ~30-40MB
dynamically linked), and a steady-state footprint in the 25-40MB RSS range
against the current stack's ~218MB.

## Scope

**Included:**

- **Replacing the display output path.** Presenting a fullscreen image directly
  to the display hardware, with no X server, no Wayland compositor, no window
  manager, and no cursor-hiding helper anywhere in the stack.
- **Replacing the input path.** Reading the physical navigation device directly
  from the kernel. This is net-new code: Fyne supplies input today and there is
  no input handling in the repository at all.
- **Implementing the defined input interface** — Left, Right, Home, Delete, and
  Escape, plus any-key-wakes, as settled by the user. `Home`'s second job of
  triggering a `Reload` is the one binding still deliberately retired rather
  than ported. See *Rules & Constraints*.
- **Removing Fyne, X11, `ratpoison`, `unclutter`, and CGo from the project** —
  which reaches past the Go source into `install.sh`, `docs/INSTALL.md`,
  `README.md`, `CLAUDE.md`, and `.github/workflows/build.yaml`.
- **Replacing `github.com/nfnt/resize`**, which is unmaintained since 2018 and
  is used by both `loadThumbnail` and `decodeAndFit`.
- **Console and display ownership**: what happens on VT switch, on process exit,
  and on crash, now that no windowing system is managing any of it.
- **Device access permissions**, while preserving the unprivileged-kiosk
  invariant already established by `specs/nixos-deployment`.
- **The test suite consequences.** Four of the six test functions in
  `slideshow_test.go` are Fyne-coupled and cannot survive as written.
- **A resource budget expressed as measurable acceptance criteria**, since
  resource reduction is the feature's primary justification.
- **The developer's ability to run and iterate on Mural at all**, which the
  toolkit currently provides for free and which this change removes.

**Excluded (non-goals):**

- **Video playback, in any form.** Deliberately removed. Neither this feature
  nor the future transitions feature it defers to is a re-entry vector for it —
  a transition, whenever it is eventually built, is a computed animation
  between two decoded still images. If any part of any future plan starts
  decoding a video file, scope has slipped.
- **Kodi launcher mode.** The user has stated plainly that this is speculative —
  *"maybe someday"*, not a committed plan. It must not be treated as a
  load-bearing future requirement, and no interface, abstraction, or hook may be
  introduced in this feature to accommodate it. *If it is ever built, it will be
  scoped on its own evidence.*
- **GPU acceleration, 3D, EGL/GBM/GLES, or Wayland.** The pure-Go constraint
  rules out the GPU stack; composition is on the CPU. This feature does no
  animation, so the constraint costs nothing here — it becomes load-bearing
  again whenever a future transitions feature is built on top of this layer.
- **Any on-screen text, menu, status overlay, error message, or configuration
  UI.** Mural draws images on black and nothing else. This is consistent with
  `specs/usb-stick-hotplug`, which independently declares on-screen UI a
  non-goal.
- **Changing the schedule, CEC behaviour, config file format, or content
  directory semantics.** `cec.go` and `schedule.go` are not touched.
- **Changing what a slide is.** JPG, JPEG, PNG. No new formats, no subdirectory
  recursion, no ordering options, no per-slide metadata.
- **Multi-display, display rotation, or user-selectable video modes.** One
  connected display, its preferred mode.
- **Windows, and cross-platform portability in general.** Settled by the user:
  the aspiration is dropped outright. Mural is a Linux program for this class of
  board. No dual backend, no build-tagged toolkit fallback, no portability seam
  maintained on spec. *This is a removal of an unbuilt intention rather than of a
  working capability — see* Rules & Constraints.
- **Slide-to-slide animation, including a simple crossfade.** Deliberately
  moved out of scope. This feature replaces display output and input handling
  only; the auto-advance swap and manual navigation remain instantaneous,
  exactly as they are today. Transitions — whatever form they eventually take,
  crossfade included — are a separate future feature, to be scoped and
  measured on their own once the rendering layer exists to build them on.
- **Audio.** Mural has never had it and does not gain it.
- **The NixOS migration.** Related, mutually beneficial, and separately scoped in
  `specs/nixos-deployment`. See the independence rule below.

## Behaviour

Declarative statements of what the system must do. "Nav key" means an input from
the physical three-key device, whatever its final semantics turn out to be.

**Display output**

- When Mural starts on the sign, it must display the first slide fullscreen with
  no windowing system, window manager, or cursor-hiding helper present anywhere
  in the running system.
- When a slide is displayed, nothing else may be visible — no mouse cursor, no
  console text, no kernel messages, no boot log, no border, no window
  decoration.
- When a slide's aspect ratio differs from the display's, it must be letterboxed
  against black, preserving today's behaviour exactly.
- When Mural cannot obtain the display, it must fail with a diagnosable error in
  its log, and must not present as a silent black screen.
- When the display resolution is determined, it must be discovered from the
  connected display rather than configured, and the slide decode must target it.

**Navigation and input**

- When Left is pressed, the system must show the previous slide; when Right is
  pressed, the next; when Home is pressed, the first. Each key does exactly one
  thing and has no secondary effect.
- When a nav key is pressed, the newly selected slide must appear without
  perceptible delay, even if its full-resolution decode has not completed —
  preserving today's show-something-immediately behaviour in intent.
- When the slide set is stepped past either end, it must wrap, preserving
  today's behaviour.
- When Delete is pressed, the system must sleep the display exactly as the
  schedule's off transition does: blank the display, stop the advance timer,
  and send CEC standby when CEC is available. Unchanged from today.
- When Escape is pressed, Mural must exit. Unchanged from today.
- When the system is paused and any key is pressed, that key must first resume
  it — display power on via CEC, current slide redisplayed, auto-advance
  re-armed — the same as today. Escape and Delete are the exception: they act
  immediately regardless of pause state and do not themselves trigger a resume.
- When the navigation device is absent at startup, Mural must still start and
  still run the schedule-driven rotation. A sign with no remote is a working
  sign.
- When the navigation device is disconnected and reconnected while Mural is
  running, navigation must work again without restarting Mural.

**Ownership of the console and the display**

- When another process takes the display — a VT switch to a text console —
  Mural must relinquish it cleanly, and must restore its own output correctly
  when the VT returns, without a restart and without a corrupted mode.
- When Mural exits, whether normally or by crash or by signal, the console must
  be left in a state an operator can use: a working text console, not a frozen
  final frame and not a black screen with no way back in.

**Preserved behaviour**

- When a slide changes — by auto-advance or by navigation — the change is an
  instantaneous swap, exactly as it is today. This feature adds no transition
  or animation of any kind.
- When the schedule turns the sign off, the display must black out, the advance
  timer must stop, and CEC standby must be sent — unchanged from today.
- When the content directory is rescanned and the slide set is replaced, the
  display must not blank, flicker, or re-initialise; the sign must continue
  showing content across the swap.
- When a content file is unchanged since the last scan, it must not be
  re-decoded — the existing size/mtime reuse behaviour is preserved.
- When a content file is corrupt, it must remain in the rotation rather than be
  excluded, preserving pre-existing behaviour. *What is displayed in its place is
  newly Mural's own decision — see the constraint below.*
- Schedule, CEC, and config-file behaviour must be indistinguishable from today
  in every observable respect.

**Build and footprint**

- When the binary is built, it must build without a C toolchain and without any
  X11 or GL development headers, for every target the project ships — all of
  which are Linux.
- When Mural runs on the target board, its sustained resident memory must be
  substantially below the current stack's measured baseline, and this must be
  demonstrated by measurement rather than asserted.
- When Mural runs on the sign, it must run as an unprivileged user, with no
  root, no setuid helper, and no elevated capabilities.

## Rules & Constraints

- **The rewrite surface is small and precisely known. The risk is not in the
  amount of code being replaced — it is in what replaces it.**

  Verified by reading the repository: `cec.go` (47 lines, stdlib only) and
  `schedule.go` (350 lines, stdlib plus `BurntSushi/toml`) survive completely
  untouched — `schedule.go` is already decoupled from the player by plain
  `func()` callbacks. `main.go` (47 lines) imports no Fyne and is wiring only.
  Only `slideshow.go` has real toolkit coupling, and only about half of it:
  `isImageExt`, `Slide`, `loadThumbnail`, `scanSlides`, and `decodeAndFit` (~120
  lines) already import nothing but `image`, `os`, and a resize library. **The
  genuinely Fyne-coupled logic is roughly 180 lines**: `Run()`, `show()`,
  `pause()`/`resume()`, and the `fyne.Do` marshalling.

  *That number is reassuring in the wrong direction and must not be allowed to
  set expectations. Those ~180 lines are replaced by an estimated 500-700 lines
  of hand-written kernel-ABI plumbing with no library to lean on. This is to be
  sized as writing a small display driver's worth of ioctl code, not as swapping
  out a rendering call.*

- **No mature Go DRM library exists, so the ioctl layer is hand-written.**
  Investigation found `beoran/drm` unproven and effectively unused (0 stars); the
  ecosystem has nothing production-grade. A DRM dumb-buffer backend means
  hand-writing roughly 300-400 lines of ioctl structures covering
  `SET_MASTER`/`DROP_MASTER`, mode enumeration and mode-setting, buffer
  allocation and mapping, and page-flip. This is the single largest source of
  implementation risk in the feature and it is unavoidable given the pure-Go
  constraint.

  *Which backend to build is Phase 2's decision, not this phase's. What Phase 1
  records is that the apparently simpler alternative is not simpler where it
  matters: raw `/dev/fb0` on this board carries Pi-specific pixel-format hazards
  (RGB565 versus XRGB8888 depending on overlay configuration), writes that
  silently no-op while another process holds DRM master, and no mechanism to
  restore the mode on exit. "Simpler to write" and "simpler to debug on the only
  board that exists" are not the same property, and the second one is the one
  that is scarce here.*

- **VT-switch handling is required work, not polish.** A DRM application must
  drop master when the VT goes away and reacquire it and re-set the mode when it
  returns (`VT_SETMODE` plus `SIGUSR1`/`SIGUSR2` handlers) — roughly 80-120
  lines. Without it, a `Ctrl-Alt-F<n>`, a `getty` waking up, or a kernel message
  produces a permanently corrupt display.

  *This is not a cost of any particular design choice; it is the general cost of
  running with no windowing system. It was being done for Mural, invisibly, by X.
  It must be planned for explicitly rather than discovered on the board.*

- **Exit and crash must not leave the sign without a console.** Today X restores
  the VT when it exits, so a Mural crash leaves a usable machine. Under direct
  DRM, a process that held master and died without restoring the mode can leave
  an operator facing a dead screen with no local way in.

  This compounds badly with the access reality recorded in
  `specs/nixos-deployment/spike-findings.md`: Tailscale on the board was logged
  out, its LAN IP had drifted, and one automation user's SSH key had stopped
  authenticating. **A dead screen on this board can mean a physical trip.**
  Signal handling and mode restoration are therefore hard requirements, and their
  interaction with the guarded restart loop must be designed rather than assumed.

- **Windows support is dropped. RESOLVED by the user — Mural is Linux-only.**
  `CLAUDE.md` states *"Target platform is Linux but we want to support Windows
  too."* Fyne provided that for free; DRM and `evdev` have no Windows equivalent.
  Of the three available futures — drop the aspiration, keep a build-tagged
  toolkit backend, or write a native Windows backend — **the user chose to drop
  it outright.** No dual backend, no fallback path.

  *This costs less than it appears to, and it is worth recording why so nobody
  later mistakes it for a sacrifice. Windows was an aspiration in the
  documentation, never a shipped capability: `.github/workflows/build.yaml` was
  inspected during this phase and its build matrix contains exactly three
  targets — `linux/amd64`, `linux/arm64`, and `linux/arm` — with `GOOS: linux`
  hardcoded. **No Windows artifact has ever been built, published, or tested.**
  What is being retired is an intention recorded in `CLAUDE.md` and a set of
  MSYS2/TDM-GCC build instructions in `README.md`, not a platform anyone runs on.*

  **The consequence that matters is not portability but the architecture.** With
  no second platform to serve, the design needs no portability seam and no
  backend abstraction on that account. Phase 2 should not build one speculatively.
  *One caveat, immediately below: the developer's need to run Mural off the sign
  is a separate requirement that survives this decision untouched, and it is now
  the only thing that could justify a non-DRM output path. The two questions were
  entangled; they are now cleanly separated, and dropping Windows must not be
  read as answering the other one.*

- **Losing the toolkit also loses the developer's ability to run Mural at all.**
  Today `go run .` on the x86 development machine opens a window. Under DRM and
  `evdev` it needs a free VT and device permissions and cannot run inside a
  desktop session — so the natural inner development loop disappears on the same
  day the code becomes harder than it has ever been.

  That is a serious problem on a project whose only test hardware is one remote
  board with unreliable access. **Some way to run and exercise Mural off the sign
  is not a convenience here; it is how the work gets done at all**, and Phase 2
  must not produce a plan that leaves the developer with no way to run the
  program. *What form it takes — a headless sink that writes frames to disk for
  inspection, a development-only windowed backend, something else — is Phase 2's
  design.*

  **This requirement survives the Windows decision unchanged, and is now the only
  thing that could justify a second output path.** The two were previously
  entangled — a Windows backend might have doubled as the development one — and
  the entanglement is now gone. That simplifies the question rather than
  answering it: dropping Windows removes a *reason* to build a portability seam,
  not the developer's need to run the program. *A headless frame-dumping sink is
  the cheap answer and does not drag a toolkit back in; Phase 2 should reach for
  something of that shape before anything larger.*

- **Four of the six test functions are Fyne-coupled and will not survive as
  written; rewriting them is in scope and deleting them is not.**
  `TestIsImageExt` and `TestScanSlidesImages` are toolkit-agnostic and carry
  over unchanged. `TestSlideshowPauseStopsTimerAndBlanksImage`,
  `TestSlideshowResumeRearmsTimer`, `TestSlideshowNavKeysRearmTimer`, and
  `TestSlideshowImageAdvanceTimingUnchanged` all build a `canvas.Image` through a
  headless Fyne harness (`newHeadlessSlideshow`).

  Those four encode real behavioural guarantees — the timer re-arms on
  navigation, pause blanks and stops the timer, advance timing is what the config
  says. Those guarantees are exactly what a rendering rewrite is most likely to
  break quietly. They must be re-expressed against the new layer, not lost in
  translation. *And the new layer needs coverage of its own: compositing and
  letterboxing should be testable without hardware. That is an argument about
  where the seam goes, which is Phase 2's to settle.*

- **Device access is new, and must not be paid for with privilege.** `/dev/dri/
  card*` and `/dev/input/event*` require group membership (conventionally `video`
  and `input`) or equivalent `udev` rules. The unprivileged-kiosk invariant
  established in `specs/nixos-deployment` — no root, no setuid, no capabilities —
  is reaffirmed here without exception.

  *This introduces a new instance of a failure mode that specification already
  documents for CEC: a deployment that looks entirely correct and silently does
  nothing, because a device node is unreadable. It deserves the same treatment —
  an explicit verification item, not an assumption.*

- **The input interface is five single-purpose bindings, not three. REVISED by
  the user after this phase's initial approval.** **Left = previous slide.
  Right = next slide. Home = jump to the first slide. Delete = sleep the
  display. Escape = quit.** Each key still does exactly one thing — nothing is
  overloaded — but the user has decided to keep the full existing keyboard
  arrangement rather than treat everything but Left/Right/Home as debug
  scaffolding to be dropped.

  **This is a reversal of this phase's original resolution, not an oversight,
  and the reasoning behind the original call is worth keeping on record rather
  than silently erasing.** The initial decision dropped `Escape` specifically
  because *"allowing the remote to terminate an unattended sign was a
  hazard."* That concern is still true — Escape gives anyone at the keyboard
  the ability to stop the sign — and the user has decided to accept it anyway,
  in exchange for keeping a familiar, already-tested input arrangement. `Home`'s
  second job of triggering a `Reload` is the **one** binding from the original
  arrangement that stays dropped; the user did not ask for it back, and nothing
  here reopens that specific consequence (see below).

  **Only one of the three keys on the physical remote can reach `Escape` or
  `Delete` at all** — the remote is still Left/Right/Home only, per its
  physical layout. `Escape` and `Delete` are reachable only from an attached
  keyboard, exactly as they are today.

  **Precedence must match today's exactly**, since that is the behaviour being
  kept, not redesigned: `Escape` and `Delete` act immediately regardless of
  pause state and do not themselves count as the key that resumes a paused
  sign. Every other key — including `Left`, `Right`, `Home`, and any unbound
  key — resumes a paused sign first if one is paused, then (for `Left`/`Right`/
  `Home`) performs its navigation.

  **Two consequences from the original resolution are unaffected by this
  reversal and are worth stating so they are not re-litigated.**

  *First, resume-from-pause still works exactly as before* for `Left`, `Right`,
  and `Home` — nothing about this reversal changes that.

  *Second, there is still no manual gesture to pick up new content.* `Home`
  currently triggers a `Reload` and will no longer do so — this part of the
  original decision stands. Content refresh therefore happens at scheduled
  turn-on, on the daily config reload, and — once `specs/usb-stick-hotplug` is
  built — on stick insertion. **That remains a real reduction in what an
  operator standing at the sign can do**, with SSH remaining the answer for
  anyone who needs to force a reload. *If it turns out to matter in practice,
  restoring it is a small follow-up, not a redesign.*

  Separately and empirically: what the physical remote actually emits —
  whether it presents as a USB HID keyboard, and whether its three keys really
  send `KEY_LEFT`, `KEY_RIGHT`, and `KEY_HOME` — must still be answered by
  plugging it in and reading raw events. **The semantics are settled; the
  keycodes are not.** Spike item 3 (renumbered after the transitions spike item
  was removed).

- **The role of thumbnails changes and must be re-examined rather than carried
  across.** Their purpose — put *something* on screen instantly while the full
  decode runs — survives intact. Their sizing does not obviously survive:
  `thumb_width` (default 80px) was chosen against a Fyne canvas, and the output
  dimensions are now known and fixed at startup from the display mode rather than
  discovered after a layout pass.

  *Two artifacts of the toolkit disappear along the way and should not be
  reimplemented out of habit: the `winSize func() fyne.Size` indirection, and the
  deferred first `show` that exists solely because Fyne reports a pre-layout
  placeholder size on the first frame.*

- **"Corrupt image stays in the rotation" is preserved, but what it looks like
  becomes Mural's decision for the first time.** Today a corrupt file yields a
  `nil` thumbnail and the toolkit renders nothing. With no toolkit, `nil` means
  whatever Mural's compositor decides it means. Phase 2 must define it — a black
  frame being the obvious candidate — rather than inherit an accident and
  discover it on the sign.

- **This feature reaches past the Go source into deployment and CI.**
  `install.sh` `apt install`s `xinit`, `ratpoison`, `cec-utils`, `libgl1`,
  `unclutter`, and `x11-xserver-utils`, and writes `.xinitrc` and `.ratpoisonrc`;
  `docs/INSTALL.md` instructs the operator to type `startx`;
  `.github/workflows/build.yaml` builds with `CGO_ENABLED=1` against
  `libgl1-mesa-dev`, `libx11-dev`, and friends across three architectures, using
  `dpkg` multiarch for the cross builds.

  All of that changes. *Most of it changes for the better: a pure-Go static
  binary makes cross-compilation trivial — no C cross-toolchain, no multiarch
  package juggling, no per-architecture dependency lists — and turns the release
  workflow into three `GOARCH` values and nothing else. This is a real
  simplification to claim, not merely churn to absorb.*

- **This work is independent of the NixOS migration, but strictly helps it. Do
  not conflate the two specs.** `specs/nixos-deployment` found that the published
  release binaries cannot run on NixOS *specifically because* CI links them
  against Ubuntu's GL and X11 libraries through CGo. A static pure-Go binary
  removes that constraint at the root. It also makes the `hardware.graphics.
  enable` / Mesa / `/run/opengl-driver/lib` trap recorded in that spike
  irrelevant, and should cut both the 5,184-derivation system closure and the
  12m32s incremental on-device rebuild substantially.

  It cuts the other way too: that spec's kiosk-session design (autologin plus X
  plus `ratpoison`) is invalidated by this feature, and its Criterion 2 finding —
  that VC4 V3D 2.1 exactly matches Fyne's GL 2.1 binding — stops being load-
  bearing.

  **Neither specification gates the other. Whichever ships second must re-read
  the first's assumptions before implementing.** In particular both intend to
  change or retire `install.sh`, and they must not do it twice or in conflict.

- **`specs/usb-stick-hotplug` interlocks with the paths being rewritten.** That
  feature is unimplemented, so nothing breaks today, but it explicitly requires
  that the display **wake on stick insertion, exactly as a nav key does**, and
  that **the currently playing content continue uninterrupted throughout the
  copy, with no blank and no flicker**, switching to the new set only once the
  ingest completes. Both land squarely on the resume and reload paths this
  feature replaces. The new layer must be able to swap the slide set without
  re-initialising the display. *Helpfully, that specification independently
  declares on-screen UI a non-goal, so it adds no rendering requirements — only
  timing and continuity ones.*

- **Validation is constrained by exactly one board, and this change can take the
  display away.** The same caution that governed the NixOS hardware spike applies
  here, and one factor is strictly worse: a NixOS boot failure left a board that
  would not come up, but a DRM bug can leave a board that is *running fine and
  unreachable*, with no console and no working mode.

  There is one Pi 3B+, no spare SD card confirmed to exist (one was recommended
  for the separate NixOS boot-test gate and may or may not have been bought),
  Tailscale is logged out, and the LAN IP drifts. **A recovery path must be
  settled before the first on-hardware DRM test, not during it.** Acceptable
  answers include a spare SD card holding the known-good Debian/X install so
  recovery is a card swap, a serial console on the UART pins, or restored and
  verified remote access. *An unstated recovery plan is not one of the acceptable
  answers, and this is exactly the kind of thing that gets skipped once the code
  finally compiles.*

- **The resource targets are the justification for the feature, so they are
  acceptance criteria and need an honest measurement method.** The ~218MB
  baseline is a point-sample of the whole Xorg + `ratpoison` + `unclutter` +
  Mural stack. The target is ~25-40MB RSS and a ~5MB static binary.

  For that comparison to gate anything, it must be like-for-like: same board,
  same content directory, same display mode, sustained over a real rotation
  rather than sampled once at startup, and counted across every process the sign
  runs rather than just Mural's own. The target figure must also honestly include
  the framebuffer mapping itself — a 1080p XRGB8888 buffer is roughly 8MB.

- **Sizing input, offered as an order of magnitude and not as a commitment.**
  Prior analysis put this at roughly 10-18 working days: DRM backend 3-5d, evdev
  and VT handling 2-3d, event-loop rewrite 1-2d, scaling and format conversion
  1d, tests 1-2d, packaging and documentation 1d, and 2-4d of buffer for
  on-hardware integration. *The buffer is the part most likely to be wrong, and
  it is wrong in the expensive direction: one board, flaky access, and a class of
  bug that is only reproducible on the hardware it breaks.*

## Spike — required before Phase 2 designs the rendering and input layer

Modelled on the hardware spike in `specs/nixos-deployment`, and for the same
reason: the decisive unknowns here are empirical, and no amount of specification
resolves them. Phase 2 should carry this as task 1.

**It must answer, on the real board:**

1. **DRM bring-up.** Can a pure-Go process acquire DRM master, enumerate the
   connector's preferred mode, allocate and map a dumb buffer, and put a single
   correct fullscreen image on the HDMI output? Including: what pixel format is
   actually negotiated, and does it match assumptions.
2. **Console recovery.** After that process exits normally, is killed with
   `SIGKILL`, and panics, is the board left with a usable console each time?
   *This is the criterion that protects the only board, and it should be answered
   before criterion 3 rather than after.*
3. **Input identity.** With the real three-key device plugged in: which
   `/dev/input/event*` node does it appear as, and do its three keys emit
   `KEY_LEFT`, `KEY_RIGHT`, and `KEY_HOME` or something else entirely? Does it
   survive unplug/replug with the same identity? *The semantics are settled; this
   establishes what to bind them to.*
4. **Footprint.** RSS of a bare pure-Go DRM process displaying one static image,
   measured the way the acceptance criterion will be measured.

**Exit criteria.** Items 1 and 2 are pass/fail and gate the whole approach; a
failure returns the decision to the user rather than being designed around.
Items 3 and 4 are information-gathering: 3 supplies the keycodes to bind the
settled semantics to, and 4 establishes whether the stated footprint target is
realistic before it becomes a gate.

*If the spike cannot be run — no recovery path, no reliable access to the board —
that is itself a finding to escalate before Phase 2 proceeds, not a reason to
proceed on optimism.*

## Edge Cases

| Scenario | Expected behaviour |
|----------|--------------------|
| Nav key held down and auto-repeating | System stays responsive; each repeat advances one slide immediately. Must not accumulate a backlog or lag behind the operator's finger |
| Only one slide in the content directory | Redisplaying the same slide is a no-op — never a flicker, blank, or visible reload |
| Content directory empty at startup | Same as today: Mural exits with an error rather than starting. Not silently a black screen |
| Content directory becomes empty on reload | Same as today: reload is refused and logged, existing rotation continues |
| Corrupt image reached in the rotation | Stays in rotation per pre-existing behaviour; the frame shown in its place is a deliberate choice (black being the obvious one), not an accident of a nil image |
| Reload swaps the slide set mid-rotation | Display must not blank, flicker, or re-initialise. Required by `specs/usb-stick-hotplug`'s continuity rule as well as by ordinary use |
| VT switched away to a text console | Mural drops DRM master and stops drawing. No crash, no busy-looping, no attempt to keep writing to a framebuffer it no longer owns |
| VT switched back to Mural | Master reacquired, mode re-set, current slide redrawn. No restart, no corrupted mode, no need for an operator to intervene |
| Mural killed with `SIGKILL` | Console recoverable. This is the case that cannot be handled by a signal handler and therefore constrains what state may be left behind |
| Mural panics | Console recoverable, panic visible in the log. A dead screen with no diagnostic is the worst outcome available and must be designed out |
| Mural crash-loops under the guarded restart | Must not produce a display that flashes between mode-sets, and must not hold the console hostage. Recovery to a text console must remain possible |
| Another process already holds DRM master | Mural fails loudly with a diagnosable error, not silently. *This is the exact failure that makes raw `/dev/fb0` treacherous — writes appear to succeed and nothing reaches the screen* |
| No `/dev/dri` device present at all | Startup fails with a clear message naming the cause. A developer running on a machine without DRM must be told why, not left guessing |
| Mural lacks permission on the DRM or input device | Clear, specific error naming the device and the likely group. Not a generic permission failure, and not silence |
| Nav device absent at startup | Mural starts normally and runs the schedule. A sign with no remote is a working sign |
| Nav device unplugged and replugged while running | Navigation works again without restarting Mural |
| An ordinary USB keyboard is plugged in | Its Left/Right/Home navigate, Delete sleeps the display, Escape quits, and any other key wakes the display if paused — binding is by keycode rather than by device, so the keyboard and the physical remote share the same Left/Right/Home behaviour. *An on-site engineer with a keyboard can stop the sign (Escape) or force it to sleep (Delete), but still cannot force a content reload — that gesture stays gone regardless of input device* |
| Operator wants to force a content reload while standing at the sign | Not possible from the remote — `Home` no longer triggers a `Reload`. Content refreshes at scheduled turn-on, on the daily config reload, and (once built) on USB insertion. SSH is the answer otherwise. An accepted consequence of the pure-navigation decision, not an oversight |
| Display unplugged and replugged (HDMI hotplug) | Behaviour must be defined rather than discovered. Realistic on a sign, and interacts with CEC power control, which already power-cycles the display deliberately |
| Display's preferred mode differs from 1920x1080 | Mode is discovered, not assumed |
| Schedule turns the sign off | Display blacks and CEC standby is sent immediately — no animation to interrupt |
| Schedule turns the sign on | Reload then CEC on, unchanged from today, with the first slide appearing without an initialisation flash |
| Developer runs Mural on an x86 laptop inside a desktop session | Must not be a dead end. Whatever the answer is, the developer must have a way to run the program, or the feature cannot be built |
| Windows build attempted after this change | Fails, by decision. Windows support is dropped and the documentation must say so plainly rather than leave a build that silently stopped working. *No Windows artifact was ever built or published, so nothing that worked is lost* |

## Open Questions

**The three questions that blocked this gate have been answered by the user and
are recorded as RESOLVED in the settled table below.** What remains is listed
here. One item — the board-recovery plan — has been **elevated**: it does not
block Phase 2's design work, but it blocks Phase 2's *first task*, because the
spike cannot safely run without it. The rest can be carried into Phase 2 as
decisions to make with the user in the loop. None should be silently defaulted.

| Question | Status |
|----------|--------|
| How is the board recovered if a DRM bug takes the display? | **OPEN — ELEVATED. Blocks the spike, and the spike is Phase 2's task 1.** Spare SD card with the known-good Debian/X install, serial console on the UART pins, or restored and verified remote access. One board, no confirmed spare card, Tailscale logged out, drifting LAN IP. *This was already the most operationally dangerous item; settling the three design questions promoted it to the front of the queue simply by clearing everything ahead of it. It is now the single thing standing between Phase 2 and its first task* |
| What keycodes does the physical device emit? | **OPEN — empirical, not a decision.** The semantics are settled (Left/Right/Home); whether the device enumerates as a USB HID keyboard and actually sends `KEY_LEFT`/`KEY_RIGHT`/`KEY_HOME` must be read off the device. Spike item 4 |
| Where does this sit relative to the NixOS work? | **OPEN — ELEVATED slightly, sequencing rather than design.** Both features are now at or near their implementation gate, both intend to change or retire `install.sh`, and both touch the kiosk session. *Previously this could wait; with both specs approaching Phase 2 it should be decided before either starts implementing, or the collision happens in exactly one file* |
| Is the resource target a hard gate or a direction? | **OPEN.** If the result lands at 60MB rather than 40MB, is that a failed feature or a successful one? It matters, because it decides how much optimisation effort is justified once the thing works at all |
| Is a change in scaler output quality acceptable? | **OPEN, minor.** Today both thumbnail and full-size paths use `nfnt/resize` with Lanczos3. Moving to `golang.org/x/image/draw` most naturally means `CatmullRom`, which is not pixel-identical. Almost certainly imperceptible on photographic signage content, but it is a visible-output change and should be acknowledged rather than slipped in |
| What happens on HDMI hotplug? | **OPEN.** Realistic on a sign, and it interacts with CEC, which already deliberately power-cycles the display. Behaviour should be defined rather than discovered |
| Should the release binaries keep shipping three Linux architectures? | **OPEN, low stakes.** Pure-Go static builds make this nearly free, so the answer is probably yes — but `specs/nixos-deployment` separately concluded those artifacts lose their documented consumer, and the two should not answer it differently |

**Already settled — recorded so they are not re-opened:**

| Question | Answer |
|----------|--------|
| **What are the keys, and what does each one do?** | **REVISED by the user after this phase's initial approval.** Left = previous slide, Right = next slide, Home = jump to the first slide, Delete = sleep the display, Escape = quit. Each key still does exactly one thing. The original resolution had dropped `Delete` and `Escape` as debug scaffolding; the user has since decided to keep them, accepting the kiosk-hazard tradeoff `Escape` reopens. `Home`'s second job of triggering a `Reload` is the only binding that stays dropped. *Full precedence rules — Escape/Delete bypass pause state, every other key resumes first — recorded in* Rules & Constraints |
| **Does the project still intend to support Windows?** | **RESOLVED by the user: no. Dropped entirely.** Mural is Linux/Pi-only. No dual backend, no build-tagged toolkit fallback, no portability seam maintained on spec — Phase 2 should not build one. *Cheaper than it looks: `.github/workflows/build.yaml` was inspected and ships exactly three targets, all `GOOS: linux`. No Windows artifact was ever built, published, or tested, so what is retired is an intention in `CLAUDE.md` and some MSYS2/TDM-GCC instructions in `README.md`, not a working platform.* **Note this does not answer the developer's need to run Mural off the sign** — that requirement survives independently and is now the only thing that could justify a second output path |
| **What transitions are actually wanted?** | **SUPERSEDED — animation is now out of scope for this feature entirely.** Earlier resolved as "a simple crossfade, chosen over pan/zoom and richer effects to sit comfortably inside the Cortex-A53 budget." That decision is set aside — not because the crossfade choice was wrong, but because the user has since decided no transition ships in this feature at all. Display output and input handling ship on their own; the auto-advance and manual-nav swap stay exactly as instantaneous as they are today. Transitions, crossfade included, are a separate future feature, scoped on their own evidence once this rendering layer exists |
| Does the sign need any content-reload gesture? | **RESOLVED by implication of the keymap decision: no.** `Home` no longer triggers a `Reload`, and none of the other four bindings (Left, Right, Delete, Escape) does either. Forcing a reload is done over SSH; content otherwise refreshes at scheduled turn-on, on the daily config reload, and (once built) on USB insertion per `specs/usb-stick-hotplug`. *Recorded rather than silently dropped, because it is a genuine reduction in what an operator standing at the sign can do. Note this is now the only surviving reduction from the original keymap decision — stopping the sign (Escape) and sleeping it (Delete) are both back* |
| Should Mural drop Fyne, X11, `ratpoison`, `unclutter`, and CGo? | **Yes — decided by the user before this phase, on the strength of a hardware spike and an explicit optimisation directive. Not re-litigated here** |
| Should video playback come back? | **No.** Removed deliberately. What video was actually being used for was slide transitions, and that want is now deferred to a separate future feature — not a route back to video |
| Is Kodi launcher mode a requirement this design must accommodate? | **No — explicitly speculative per the user.** No abstraction, hook, or interface may be introduced for it in this feature |
| Which files survive the rewrite? | **Settled by inspection.** `cec.go` and `schedule.go` untouched; `main.go` is wiring only; roughly half of `slideshow.go` is already toolkit-agnostic. The Fyne-coupled surface is ~180 lines |
| Is `nfnt/resize` retained? | **No.** Unmaintained since 2018. `golang.org/x/image/draw` is already an indirect dependency and can scale, letterbox, and blit in one call without an intermediate allocation |
| Is a third-party Go DRM library available? | **No.** The ioctl layer is hand-written; `beoran/drm` is unproven and effectively unused |
| Does the board support this at all? | **Yes.** Full KMS confirmed on the deployed board — `dtoverlay=vc4-kms-v3d`, `vc4-drm` bound across hvs/hdmi/txp/pixelvalve/v3d. Not the older fkms path |
| Do the four Fyne-coupled tests get deleted? | **No.** They encode behavioural guarantees a rendering rewrite is most likely to break quietly. Re-expressing them against the new layer is in scope |
| Does Mural gain any privilege? | **No.** Unprivileged kiosk user, no root, no setuid, no capabilities — reaffirming the invariant from `specs/nixos-deployment` |
| Scope classification | **Complex — unchanged by the three resolutions.** New domain (kernel DRM ioctl ABI, `evdev`, VT signalling — none of it with in-house or third-party precedent), cross-cutting across rendering, input, build, CI, packaging, documentation, and tests. *Settling the keymap, Windows, and moving animation out of scope narrowed the feature without simplifying what remains: the hand-written ioctl layer and the VT and console-recovery work stand exactly as they did. Full pipeline including `sdd-harden`* |

## Analyst Checklist

- [x] Goal is tied to a specific user need
- [x] Scope boundaries are explicit — what's in and what's out
- [x] **All ambiguities resolved — yes.** The three blocking questions were put
      to the user and answered: navigation is Left/Right/Home plus Delete
      (sleep) and Escape (quit) — the latter two revised back in after this
      phase's initial approval — Windows is dropped entirely, and slide-to-slide
      animation is moved out of this feature's scope entirely. What remains
      outstanding is not specification ambiguity but empirical unknowns (what
      the input device emits, what the footprint measures) and one operational
      prerequisite (the board-recovery plan) — none of which specification can
      answer
- [x] Behaviour is declarative, not prescriptive
- [x] Edge cases are identified and handled
- [x] Non-goals prevent scope creep

**Gate status: READY for Phase 2, with one carried prerequisite.**

All three blocking questions have been answered by the user and are recorded as
RESOLVED above:

1. **Navigation and input** — Left = previous, Right = next, Home = first
   slide, Delete = sleep the display, Escape = quit. Revised after this phase's
   initial approval to keep Delete and Escape rather than drop them; `Home`'s
   `Reload` trigger remains the one binding that stays dropped.
2. **Windows** — dropped entirely. Linux-only. No portability seam.
3. **Transitions** — moved out of scope entirely. This feature replaces display
   output and input handling only; slide changes stay exactly as instantaneous
   as they are today. Animation is a separate future feature, scoped on its own
   once this layer exists.

The architect may proceed. Two things to carry forward rather than rediscover:

**The board-recovery plan is a prerequisite for the spike, and the spike is task
1.** It does not block design work, so Phase 2 can begin — but no instruction
should be sent to the hardware until it is settled. One board, no confirmed
spare SD card, Tailscale logged out, a drifting LAN IP, and a class of bug that
can leave the board running but with no display and no console. *This was always
the most operationally dangerous open item; it reached the front of the queue
because everything ahead of it was cleared.*

**The Windows decision does not answer the development-loop question.** The two
were entangled — a Windows backend might have doubled as the developer's way to
run Mural off the sign — and they are now cleanly separated. Dropping Windows
removes a reason to build a portability seam; it does not remove the need to run
the program somewhere other than the sign, which on this project is how the work
gets done at all. Phase 2 must still answer it, and should reach for the cheap
answer (a headless frame-dumping sink) before anything that drags a toolkit back
in.

Everything else in the open-questions table can be carried into Phase 2 as
decisions to be made with the user in the loop. None should be defaulted
silently.
