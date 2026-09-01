# Eval Report: Native Rendering Layer (DRM/KMS + evdev)

> Phase 5 — Post-implementation evaluation. Run after sdd-coder completes.
> Evaluator had no access to the coder's reasoning — fresh context only.

---

## ROUND 2 — Re-evaluation after coder fixes

**Verdict: PARTIAL PASS.** All three actionable ❌ code defects are fixed and
verified. F1 and F5 stand by the user's explicit decision. Seven ⚠️ warnings
remain open, none blocking.

The Round 1 findings below are preserved unedited as the record of what was
found; this section states what changed. Round 2 re-ran every sensor and
re-read every changed file — it did not take the coder's `tasks.md` notes on
faith.

### Sensors (re-run)

| Check | Result |
|-------|--------|
| `gofmt -l .` | ✅ clean |
| `go vet ./...` | ✅ 0 errors |
| `go build .` | ✅ clean |
| `GOARCH=arm64` cross-build | ✅ clean (the deployed target) |
| `GOARCH=arm GOARM=7` cross-build | ✅ clean |
| `go test -race -count=1 ./...` | ✅ all pass, race-clean |
| Coverage | 18.5% (was 18.8% — `drm.go` grew by the F3 fix; no tests removed) |
| Headless smoke test | ✅ runs, writes frames, auto-advances |
| Empty content dir | ✅ still exits with `error: no images found in empty` |

### Blockers — all three fixed and verified

**✅ F2 — crash-loop guard restored.** [install.sh:95](install.sh#L95) now calls
`./mural` without `exec`. I extracted the guard body into a standalone script,
substituted a stub exiting 0 (the Escape-quit case that previously trapped the
console), and ran it: control returns, the banner prints, the 30-second throttle
is reached. This is exactly the pre-rewrite `startx` shape Step 28 asked to
preserve. The respawn still happens afterwards — as it always did — but now with
the documented Ctrl+C window in front of it.

**✅ F3 — flip wait is bounded.** [drm.go:319-354](drm.go#L319-L354).
`waitForFlipEvent` now takes a `timeout`, computes an absolute deadline, and
`unix.Poll`s with the remaining budget instead of doing a naked blocking read.
Checked the details that make or break this kind of fix:

- The deadline is **absolute**, so the `EINTR` retry path (`continue`) cannot
  extend the wait — important, since Go's runtime fires `SIGURG` for async
  preemption often enough that `EINTR` is a live path, not a theoretical one.
- Sub-millisecond remainders truncate to `Poll(…, 0)`, which returns
  `nready == 0` and exits with the timeout error rather than spinning.
- A non-flip event (e.g. vblank) loops back to `Poll` and stays inside the same
  deadline — no unbounded inner loop.
- `err == unix.EINTR` compares an `error` interface against a `syscall.Errno`
  constant, which is a valid and conventional comparison here. (`errors.Is`
  would read slightly more idiomatically; not a defect.)

The wedge that mattered — a run loop unrecoverable by nav key, timer, `ctx`, or
`SIGTERM`, needing `SIGKILL` — is gone. `pageFlipTimeout = 2s` is documented with
its reasoning at [drm.go:312](drm.go#L312).

**✅ F4 — race resolved.** `front` is now `atomic.Int32`
([drm.go:39-42](drm.go#L39-L42)), and I grepped every access: exactly three, all
atomic — `Load()` at [drm.go:273](drm.go#L273), `Store()` at
[drm.go:289](drm.go#L289), `Load()` at [vt.go:114](vt.go#L114). No bare accesses
remain.

I also re-checked the other fields `vt.go`'s signal goroutine touches, since my
Round 1 write-up named `r.mode` alongside `front`: `mode`, `crtcID`, `connID`,
and `buf.fbID` are all written once during `OpenDRMRenderer` and never mutated
afterwards, so they are genuinely immutable-after-construction and need no
synchronisation. `front` was the only real race, and it is the one that was
fixed. The commented zero-value note at [drm.go:124](drm.go#L124) correctly
documents why dropping the explicit `front: 0` initialiser is safe.

**✅ F11 — retracted benchmark figure corrected** in both
[compositor.go:20-22](compositor.go#L20-L22) and
[slideshow.go:334](slideshow.go#L334), now citing ~4.1x / 113ms vs 466ms and
naming `bench_test.go` as the source.

### Accepted by the user, standing as-is

**F1** (RSS ~190–200MB vs the 25–40MB target) and **F5** (18.5% coverage vs the
generic rubric's 80%) are accepted. No further action. Both remain documented
below and in `status.json` so the record is not lost.

### ⚠️ F14 — new, minor: a timed-out flip leaves the event queue skewed

**File:** [drm.go:334-354](drm.go#L334-L354) — introduced by the F3 fix, so
flagged here rather than in Round 1.

When `waitForFlipEvent` times out, `Present` returns an error and `front` is
correctly left unchanged. But the kernel may still complete that flip afterwards
and queue its completion event on the fd. Two consequences:

1. The next `Present` computes `back = 1 - front` and writes into the buffer the
   kernel may by then be scanning out — a torn frame.
2. That stale event is what the next `waitForFlipEvent` reads, so it returns
   `nil` early, one event out of step, and stays skewed.

**Why it is minor:** this can only trigger after a display loss, which is already
a degraded state; `Present` is called seconds apart on slide changes, not per
frame; and the visible result is a transient artifact rather than a hang or a
crash. The Analyst requirement F3 existed to satisfy — *"an ordinary logged
`Present` error … reconnection picked up reactively on the next `Present` call"*
— is met. Draining pending events at the top of `waitForFlipEvent`, or on the
error path, would close it if you ever want to.

### Still open (unchanged, non-blocking)

F6 (three tests assert less than their comments claim; the positive half of the
Escape/Delete precedence rule is unguarded), F7 (`KD_GRAPHICS` never set), F8
(VT reacquire wakes a paused sign), F9 (single-slide flicker), F10 (hotplug watch
missing `IN_ATTRIB`), F12 (profiling flags and `bench_test.go` absent from
`Architect.md` and `CLAUDE.md`), F13 (spike item 3 open under a checked box).

Of these, **F10 is the one most likely to bite in the field** — it breaks a
stated requirement ("navigation must work again without restarting Mural" after
replug) on a path Step 33 never tested, and the fix is one added flag.

### One process note

`status.json` was set to `"eval-fail-fixes-applied"`, which is not one of the
sdd lifecycle values. If it is meant to gate anything automated, it should
return to `"complete"` now that the blockers are cleared; if it is a deliberate
human-readable marker, it is accurate and harmless.

### Not re-validated on hardware

The F3 and F4 fixes touch DRM and VT code at 0% test coverage. Both are
reasoned-correct and pass `-race`, `vet`, and all three release cross-builds, but
neither has run on `pi3b.local` — `status.json` says as much
("Not yet redeployed"). The F3 timeout path in particular can only really be
exercised by unplugging HDMI mid-flip on the board.

---

**Evaluation method note.** The `code-reviewer` subagent named in the sdd-eval
skill is not an available agent type in this session, so the semantic pass was
performed inline. The fresh-context requirement is nonetheless met: this is a
new session with no coder transcript, and every verdict below was reached by
reading `Analyst.md`, `Architect.md`, `tasks.md`, and the actual files on disk —
plus `git show 9942bc4:` for the pre-rewrite baseline where a "preserved
behaviour" claim needed checking against what was actually there before.

## Computational Sensors

| Check | Result |
|-------|--------|
| `gofmt -l .` | ✅ 0 files need formatting |
| `go vet ./...` | ✅ 0 errors |
| `go build .` | ✅ clean — 4.76MB static binary (Analyst target: ~5MB) |
| `go test -race -count=1 ./...` | ✅ all pass, race detector clean (1.19s) |
| Test coverage | ❌ **18.8%** of statements — see COVERAGE below |
| Headless smoke test (`./mural -headless <dir>`) | ✅ runs, writes `mural-headless.png`, auto-advances |
| Empty content dir | ✅ exits 1 with `error: no images found in empty` |
| Dependency cleanup (`go.mod`) | ✅ no `fyne.io/*`, no `github.com/nfnt/resize`; `x/image` + `x/sys` are direct requires |

Per-file statement coverage:

```
compositor.go       89.5%  (17/19)     drm.go        0.0%  (0/134)
drm_ioctl.go       100.0%  (19/19)     vt.go         0.0%  (0/48)
slideshow.go        43.9%  (75/171)    headless.go   0.0%  (0/13)
input.go            17.9%  (14/78)     main.go       0.0%  (0/81)
cec.go              62.5%  (10/16)     schedule.go   0.0%  (0/139)   [untouched by this feature]
```

## Semantic Evaluation

| Criterion | Verdict | Detail |
|-----------|---------|--------|
| CONTRACT | ✅ | All 7 new files exist with the described contents; all 9 modified files updated. Two signature deviations, both recorded and reasoned in `tasks.md`. |
| BEHAVIOUR | ❌ | The feature's **stated primary justification — the RSS reduction — is measured as not met** (~190–200MB vs a ~218MB baseline and a 25–40MB target). Two console/display behaviours are also unmet. |
| EDGE CASES | ❌ | Crash-loop guard is dead code; HDMI-unplug can hang the run loop instead of logging; single-slide redisplay still flickers. |
| TESTS | ⚠️ | Every test named in tasks.md exists and passes race-clean, but three of them assert weaker properties than their own comments claim, and the run loop itself is untested. |
| PATTERNS | ✅ | gofmt/vet clean, `%w` wrapping throughout, `context.Context` for cancellation, defer-after-acquire unwind, consumer-side 2-method interface, flat `package main`. One stale-comment nit. |
| COVERAGE | ❌ | 18.8% against the rubric's ≥80%. Context matters here — see the finding. |
| NO EXTRAS | ⚠️ | `-cpuprofile`/`-memprofile` flags and `bench_test.go` are outside Architect.md's contracts and file list. User-sanctioned during the recorded post-deployment investigation, but never folded back into the design docs. |

---

## Findings

### ❌ F1 — BEHAVIOUR: the feature's primary justification is measured as not met

`specs/drm-renderer/tasks.md` (Step 33 results) records steady-state RSS at
**~190–200MB** (195MB at t+4s, 193MB at t+16s, 207MB after a VT cycle) against
Analyst.md's stated target of **25–40MB** and the X11-stack baseline of
**~218MB**. Analyst.md makes this a behavioural requirement, not an aspiration:

> "When Mural runs on the target board, its sustained resident memory must be
> substantially below the current stack's measured baseline, and this must be
> demonstrated by measurement rather than asserted."

A 218 → 195MB move is roughly 10%. That is measured, but it is not
"substantially below." Analyst.md's own open question — *"Is the resource target
a hard gate or a direction?"* — is still listed as **OPEN**, and the coder's own
notes flag this as "the one finding serious enough that [it] should be revisited
before calling the feature's primary justification met."

This is not a code defect and needs no fix here. It is the single item that
decides whether this feature passes on its own terms, and it is a decision only
you can make.

*Related, and worth noting alongside it:* the post-deployment investigation
recorded in `tasks.md` revealed the user's actual primary goal was **keypress
latency**, not footprint — and that goal was met and confirmed on hardware
("Yeahhhhh much better!"). If the real acceptance criterion is latency rather
than RSS, F1 changes character entirely. But Analyst.md as written says RSS.

---

### ❌ F2 — EDGE CASES: `install.sh`'s crash-loop guard is unreachable dead code

**File:** [install.sh:95](install.sh#L95)

```bash
cd ~/mural
exec ./mural          # <-- replaces the shell; nothing below ever runs

cat <<'BANNER'
    ***  Waiting 30 seconds before restarting...             ***
BANNER
sleep 30
```

`exec` replaces the shell process, so the banner and the 30-second throttle are
unreachable. The pre-rewrite script (`git show 9942bc4:install.sh`) called
`startx` **without** `exec`, precisely so control returned to the throttle.

This directly contradicts the step that specified it — tasks.md Step 28:

> "replace the `startx` line with a direct exec of the Mural binary — **keep the
> 'wait 30 seconds before restarting' banner/sleep and the `.bashrc`
> re-invocation exactly as-is**"

and Architect.md:

> "the exact same return-then-30s-banner-then-retry shape is kept unchanged, so
> a Mural crash or panic still surfaces the 'waiting 30 seconds' banner and still
> throttles restarts the same way an X crash does today"

**Failure scenario (concrete).** An operator presses Escape on the sign.
`NavQuit` → `cancel()` → `Run()` returns nil → `main` exits **0**. The `.bashrc`
hook is `if bash tty1-guard.sh; then exit 1; fi` — exit 0 makes the branch true,
the login shell exits, getty respawns, autologin fires, and mural restarts
**immediately**. Forever, with no 30-second window and no way to reach a shell.
That is exactly Analyst.md's forbidden edge case:

> "Mural crash-loops under the guarded restart | Must not produce a display that
> flashes between mode-sets, and **must not hold the console hostage**."

Note the asymmetry: a *crash* (non-zero exit) falls through to a shell prompt and
is recoverable. A *clean Escape quit* is the case that traps the console.

---

### ❌ F3 — BEHAVIOUR: `Present` can block the run loop forever on display loss

**File:** [drm.go:301-318](drm.go#L301-L318) (`waitForFlipEvent`), called from
[drm.go:279](drm.go#L279)

`waitForFlipEvent` is an unbounded `unix.Read(fd, buf)` loop with no timeout, no
`poll`/`select` with a deadline, and no `ctx` awareness. It runs synchronously on
the **single run-loop goroutine**.

**Failure scenario.** The page-flip ioctl is accepted (returns 0), then HDMI is
unplugged — or `vt.go`'s `SIGUSR1` handler issues `DROP_MASTER`
([vt.go:103](vt.go#L103)) — before the flip-complete event is delivered. No
event ever arrives. `unix.Read` blocks indefinitely. The run loop is now wedged:
no nav keys, no advance timer, no VT reacquire, and `ctx.Done()` cannot break it,
so `SIGTERM` will not shut the process down either. The sign is frozen on its last
frame and unreachable except by `SIGKILL`.

Architect.md defines the intended behaviour as the opposite:

> "`Present` returns that error exactly like any other page-flip failure — no
> special-cased detection, no panic, no busy-loop. The run loop logs the error
> and moves on."

A hang is strictly worse than the busy-loop that clause rules out, and it also
breaks Analyst.md's "the sign must be navigable at every instant."

The failure path in which the ioctl *itself* fails is handled correctly
([drm.go:276](drm.go#L276)); it is only the accepted-then-abandoned flip that
hangs.

---

### ❌ F4 — PATTERNS/correctness: data race on `DRMRenderer.front` between vt.go and the run loop

**Files:** [vt.go:114](vt.go#L114) and [drm.go:267](drm.go#L267), [drm.go:283](drm.go#L283)

`vt.go`'s `WatchSwitches` goroutine reads `r.buffers[r.front].fbID` and `r.mode`
on `SIGUSR2`:

```go
if err := setCrtc(r.fd, r.crtcID, r.connID, r.buffers[r.front].fbID, r.mode); err != nil {
```

while `DRMRenderer.Present`, on the run-loop goroutine, reads and writes the same
field:

```go
back := 1 - r.front
...
r.front = back
```

No mutex, no atomic, no channel hand-off. These are two different goroutines
touching an unsynchronized `int`. The race detector cannot see it because
`drm.go` and `vt.go` are at 0% test coverage and were only exercised on hardware.

Architect.md itself lists this as an unverified risk — *"a VT-switch signal
arriving mid-decode"* — and the Step 33 hardware pass did a single quiet
`chvt 1` / `chvt 7` cycle, which will not reliably hit the window. The visible
consequence when it does hit is a `SET_CRTC` pointed at the buffer currently
being written into, i.e. a torn or stale frame on VT reacquire.

Note this composes with F3: a `SIGUSR1` `DROP_MASTER` arriving while the run loop
sits in `waitForFlipEvent` is the concrete path to the permanent hang.

---

### ❌ F5 — COVERAGE: 18.8% against the rubric's ≥80%

Reported as ❌ because the rubric is explicit, but the number needs its context
stated fairly rather than left to imply neglect:

- **No coverage target exists** in `Analyst.md`, `Architect.md`, `tasks.md`, or
  `CLAUDE.md`. The 80% figure comes from the generic sdd-eval rubric, not from
  this project.
- The bulk of the uncovered statements are in `drm.go` (134), `vt.go` (48),
  `main.go` (81), and `schedule.go` (139 — untouched by this feature). Analyst.md
  and Architect.md deliberately route DRM/VT verification to on-hardware
  validation (Step 33) rather than to unit tests, and pulled the two pure helpers
  (`pickPreferredMode`, `rgbaToXRGB8888`) out into `drm_ioctl.go` **specifically**
  so the testable part could be tested — it is at 100%.
- The pure, hardware-independent surface this feature was told to cover is
  genuinely covered: `compositor.go` 89.5%, `drm_ioctl.go` 100%,
  `parseInputEvent` 93.3%.

The real gap is narrower than 18.8% suggests, and F6 names it.

---

### ⚠️ F6 — TESTS: three tests assert less than their names and comments claim

**a. [slideshow_test.go:413-441](slideshow_test.go#L413-L441)** —
`TestSlideshowNavKeysRearmTimer` never calls `handleNavKey`. It re-implements the
navigation arithmetic inline:

```go
{"right", func(s *Slideshow) { s.show((s.current+1)%len(s.slides), true) }},
```

with the comment *"These are exactly the calls handleNavKey makes."* They are —
today. If `handleNavKey`'s switch broke, this test would still pass. The function
under test is never invoked.

**b. [slideshow_test.go:455-464](slideshow_test.go#L455-L464)** —
`TestSlideshowImageAdvanceTimingUnchanged`'s comment states the guarantee as
*"show() must arm the next advance with exactly interval, never any other value"*
— but the body only asserts `s.advanceTimer != nil`. The interval value is never
checked. (Go offers no way to read a `time.Timer`'s duration, so this may be
unfixable as written; the comment overclaims what is verified.) This is one of
the four behavioural guarantees Analyst.md specifically said *"are exactly what a
rendering rewrite is most likely to break quietly."*

**c. [slideshow_test.go:472](slideshow_test.go#L472)** —
`TestSlideshowEscapeAndDeleteBypassResume` covers only the negative half of the
precedence rule. Nothing asserts the positive half: that `NavLeft`/`NavRight`/
`NavHome`/`NavWake` **do** resume a paused sign. Architect.md flagged this exact
logic as *"a small, easy-to-get-wrong piece of logic with an outsized
consequence."* Half of it is guarded.

**d.** `Run()` (0%), `handleCommand` (0%), and `advanceToNext` (0%) are entirely
untested — so wrap-around at either end, "reload with an empty result is refused
and logged," and "advance is a no-op while paused" have no coverage at all,
despite all three being declared behaviours in Analyst.md. `newHeadlessSlideshow`'s
own comment concedes it: *"Run() cannot be called from a test since it blocks in
its run loop's select."*

---

### ⚠️ F7 — BEHAVIOUR: the console is never put into `KD_GRAPHICS`

**File:** [vt.go:160-165](vt.go#L160-L165)

`RestoreConsole()` sets `KDSETMODE(KD_TEXT)` — but nothing ever sets
`KD_GRAPHICS`. The console is restored to a mode it was never taken out of.

Analyst.md requires: *"When a slide is displayed, nothing else may be visible —
no mouse cursor, **no console text, no kernel messages**, no boot log."* With the
VC in `KD_TEXT`, fbcon remains live and a kernel message printed to the console
can be drawn over the display. Holding DRM master covers most of this in
practice, which is why `KD_GRAPHICS` is the conventional belt-and-braces for
DRM applications.

To be fair to the coder: tasks.md Step 12 and Architect.md only ever ask for the
`KD_TEXT` restore, so this is a gap in the design as much as in the
implementation. It could not have been caught in Step 33 — that run had **no
monitor attached** (`edid_size=0`), so nothing about on-screen content was
observable.

---

### ⚠️ F8 — BEHAVIOUR: VT reacquire while paused wakes the display

**Files:** [slideshow.go:441-445](slideshow.go#L441-L445), [slideshow.go:326](slideshow.go#L326)

```go
func (s *Slideshow) handleVTEvent(ev vtEvent) {
	if ev == vtEventAcquired {
		s.show(s.current, true)
	}
}
```

`show()` unconditionally presents a frame **and** calls `scheduleAdvance`. If the
sign is paused — scheduled off-hours, or Delete was pressed — a VT switch back
puts the slide on screen and arms the advance timer, while `s.paused` stays
`true`. The state and the screen disagree.

**Failure scenario.** Sign is in scheduled off-hours (blank, CEC standby sent).
Someone does `Ctrl-Alt-F1` then `Ctrl-Alt-F7` on the attached keyboard. The sign
lights back up outside its schedule. It will not auto-advance (the `advanceMsg`
handler checks `s.paused` and returns at [slideshow.go:430](slideshow.go#L430)),
so it sticks on one frame until the next scheduled event.

---

### ⚠️ F9 — EDGE CASES: single-slide redisplay still flickers

**File:** [slideshow.go:326-344](slideshow.go#L326-L344)

Analyst.md: *"Only one slide in the content directory | Redisplaying the same
slide is a no-op — **never a flicker**, blank, or visible reload."*

`show()` has no `if index == s.current && already displayed` short-circuit. With
one slide, pressing Right calls `show((0+1)%1 = 0, true)`, which presents the
80px thumbnail upscaled to full screen, then re-decodes the full image behind it.

I checked the pre-rewrite baseline (`git show 9942bc4:slideshow.go`) — the old
code did the same thing, so this is inherited rather than introduced. But it is
**more visible now**: the instant path was changed to `xdraw.NearestNeighbor`
during the latency fix, so the placeholder is a blocky ~24x nearest-neighbour
upscale rather than a smooth one. The spec lists it as required behaviour and it
is not implemented.

---

### ⚠️ F10 — EDGE CASES: input hotplug watches only `IN_CREATE`

**File:** [input.go:188](input.go#L188), [input.go:221](input.go#L221)

```go
unix.InotifyAddWatch(fd, "/dev/input", unix.IN_CREATE)
```

udev creates `/dev/input/eventN` root-owned and *then* applies the `input` group
ownership. `watchDevice` fires on `IN_CREATE` and calls `os.OpenFile` immediately;
if it wins that race it gets `EACCES`, logs "permission denied", **returns, and
never retries**. The device is lost until the process restarts.

This lands directly on a stated requirement — *"When the navigation device is
disconnected and reconnected while Mural is running, navigation must work again
without restarting Mural"* — and Step 33 did **not** test unplug/replug (it is
listed under "Not tested / carried forward"). Adding `IN_ATTRIB` to the watch
mask is the conventional fix.

Secondary, lower-severity: `watchHotplug` is called *after* the `ReadDir`
enumeration ([input.go:115-122](input.go#L115-L122)), so a device appearing in
that window is missed by both paths.

---

### ⚠️ F11 — PATTERNS: shipped comments cite a measurement the project itself retracted

**Files:** [compositor.go:20](compositor.go#L20), [slideshow.go:334](slideshow.go#L334)

Both say `NearestNeighbor` is *"~13x faster"* / *"measured ~13x faster"* than
CatmullRom. `tasks.md` explicitly retracts that number:

> "**Corrected finding:** the earlier profile-sample-based '~13x faster' estimate
> ... was wrong — profile sample counts divided by a guessed call count, not a
> real measurement. The controlled benchmark gives ... **a real ~4.1x win, not
> ~13x**."

The retracted figure is what shipped in the source comments. `bench_test.go`
sitting in the same repo produces the correct number.

---

### ⚠️ F12 — NO EXTRAS: profiling flags and `bench_test.go` are outside the contract

- [main.go:16-17](main.go#L16-L17) add `-cpuprofile` and `-memprofile`, plus the
  ~35 lines of `runtime/pprof` plumbing at [main.go:98-146](main.go#L98-L146).
- `bench_test.go` is a new file not in Architect.md's "Files to Create."

Both came from the post-deployment latency investigation, are recorded in
`tasks.md`, and were user-driven — so this is disclosed scope creep, not hidden.
It is flagged only because neither `Architect.md`'s file list/contract table nor
`CLAUDE.md`'s Project Structure was updated to mention them (grep for
`bench_test`/`cpuprofile` across `CLAUDE.md`, `README.md`, `docs/INSTALL.md`
returns zero hits) — and Step 30's own reasoning was that leaving the file list
stale "would misdescribe the codebase this very doc exists to describe."

---

### ⚠️ F13 — process: Step 33 is marked `[x]` but its own body records spike item 3 as open

`tasks.md` Step 33 is checked, and its result text says:

> "**Spike item 3 (the physical three-key remote's exact keycodes) is still
> open** — no dedicated remote was available on this board; only a standard USB
> keyboard (part of a NexDock) was tested"

`input.go` binds `KEY_LEFT`/`KEY_RIGHT`/`KEY_HOME` (105/106/102) on the
assumption the remote emits them. Analyst.md is explicit that *"The semantics are
settled; the keycodes are not"* and that this must be *"answered by plugging it
in and reading raw events."* It has not been. Left/Right/Home were also not
independently confirmed on hardware even with the keyboard — only Delete and
Escape were visually verified.

Honestly disclosed by the coder rather than papered over. Recording it so the
checkbox is not mistaken for confirmation.

---

## What passed cleanly

Worth stating explicitly, since the findings list is long and the bulk of this
feature is sound:

- **All 7 new files exist and do what Architect.md said they would.** The
  `Renderer` seam, the single-goroutine run loop replacing every `fyne.Do`, the
  hand-written ioctl layer with its two pure helpers extracted for testing, the
  headless dev sink — all built as designed.
- **Fyne, X11, ratpoison, unclutter, `nfnt/resize`, and CGo are gone** from
  `go.mod`, source, `install.sh`, CI, and docs. The only surviving mentions are
  historical asides in comments explaining why code looks the way it does.
- **Key precedence is a faithful port.** I diffed `handleNavKey`
  ([slideshow.go:377-400](slideshow.go#L377-L400)) against the old
  `SetOnTypedKey` closure (`git show 9942bc4:slideshow.go:341-366`): the
  Escape/Delete early-return-before-the-pause-check ordering is preserved
  exactly, and `Home`'s `go s.Reload()` is dropped as specified. This was
  Architect.md's #1 named risk and it was gotten right.
- **`OpenDRMRenderer`'s partial-failure unwind** ([drm.go:57-122](drm.go#L57-L122))
  is exactly the immediately-deferred, named-error-guarded pattern Step 8
  demanded — no leaked DRM master on a mid-sequence failure.
- **`parseInputEvent` reads from the end of the buffer** to be correct on both
  32-bit `arm` and 64-bit `arm64`/`amd64` `struct timeval` widths, and the test
  proves it with both prefix lengths. Autorepeat (`value == 2`) is handled.
- **CI is genuinely simpler**: `CGO_ENABLED: "0"`, no GL/X11 header install, no
  dpkg-multiarch step — three `GOARCH` values and nothing else, as promised.
- **Loud-failure requirements are honoured.** No headless fallback; `/dev/dri`
  failures exit 1 with a diagnosable message. I confirmed the input-permission
  message names both the device and the `input` group, as the edge-case table
  requires.
- **Empty content directory** exits 1 with a clear error rather than a black
  screen — verified by running it.
- **Latency fix is real and measured.** `bench_test.go` replaced a bad
  profile-derived estimate with a controlled benchmark, and the finding that
  `thumb_width` is a memory knob rather than a latency one is a genuinely useful
  result for the caching decision still ahead of you.

---

## Verdict (Round 1 — superseded, see ROUND 2 at the top)

- [ ] **PASS** — all criteria ✅, ready to archive
- [ ] **PARTIAL PASS** — only ⚠️ warnings, archive with known gaps noted
- [x] **FAIL** — one or more ❌, specific items must go back to coder

**Do not archive.** Five ❌ findings, of which three are actionable code defects
and two are decisions for you.

> **Superseded 2026-08-29.** F2, F3, F4, and F11 were fixed and independently
> re-verified; F1 and F5 were accepted by the user as standing. Round 2's
> verdict is **PARTIAL PASS** — see the top of this document.

**Send back to the coder:**

| # | File | What is wrong |
|---|------|---------------|
| F2 | [install.sh:95](install.sh#L95) | `exec ./mural` makes the 30s crash-loop guard unreachable; a clean Escape-quit traps the console in an unthrottled respawn loop. Drop `exec`. |
| F3 | [drm.go:301](drm.go#L301) | `waitForFlipEvent`'s unbounded blocking read can wedge the run loop permanently on HDMI unplug or a `DROP_MASTER` race. Needs a timeout/`poll` or ctx-awareness. |
| F4 | [vt.go:114](vt.go#L114) / [drm.go:283](drm.go#L283) | Unsynchronized concurrent access to `DRMRenderer.front` and `.mode` from the VT signal goroutine and the run loop. |

**Decisions for you, not the coder:**

- **F1** — RSS landed at ~190–200MB against a 25–40MB target and a ~218MB
  baseline. Analyst.md's *"hard gate or a direction?"* question is still open and
  now has to be answered. If the real goal was keypress latency (which the
  post-deployment work met and you confirmed on hardware), say so in Analyst.md
  so the record matches the intent.
- **F5** — whether 18.8% coverage is acceptable given that the design
  deliberately routes DRM/VT verification to hardware. If it is, the 80% rubric
  line does not belong in this project's eval criteria.

**Worth fixing but not blocking:** F6 (three tests weaker than they read), F7
(`KD_GRAPHICS` never set), F8 (VT reacquire wakes a paused sign), F9
(single-slide flicker — inherited, but spec'd), F10 (`IN_ATTRIB` missing from the
hotplug watch), F11 (stale "~13x" comments contradicting `bench_test.go`), F12
(profiling flags and `bench_test.go` missing from Architect.md/CLAUDE.md), F13
(spike item 3 still genuinely open under a checked box).
