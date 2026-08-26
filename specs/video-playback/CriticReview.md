# Critic Review: Video Playback

> Phase 3 — Plan stress-test. Approved before coding begins.
> This review went through two rounds plus an empirical verification pass.
> Round 1 = self-critique. Round 2 = fresh-eyes adversarial pass by an independent agent.
> Between them, ffmpeg 8.1.2 was obtained temporarily and every command line was executed.
>
> **Totals: 13 blockers + 18 warnings + 8 notes. All applied. Steps 25 → 28.**
>
> ⚠️ **One item is NOT resolved and gates Phase 4: the cross-spec collision with
> `usb-stick-hotplug` (first section below).** The spec is internally implementation-ready;
> it is not safe to *start* coding until that sequencing decision is made, because both
> features rewrite `Reload`, `scanSlides`, and `Run()`'s empty-directory contract.

---

## 🚨 UNRESOLVED — cross-spec collision with `usb-stick-hotplug`

**This is the one item Phase 3 could not close, and it blocks Phase 4 for both features.**
It is recorded here rather than fixed because resolving it would require editing
`specs/usb-stick-hotplug/`, which this spec does not own.

`specs/usb-stick-hotplug/` (Analyst + Delta complete, no Architect yet) modifies the **same
functions** this plan modifies, for overlapping reasons:

| Surface | video-playback | usb-stick-hotplug |
|---|---|---|
| `Reload` | Step 16 adds playback teardown | Delta requires rescan be **split** from un-pause/power-on |
| `scanSlides` | Step 8 rewrites the entry loop | Delta gives `content/` internal structure; a displaced-content subdir must stay out of rotation |
| `Run()` empty-dir path | Analyst assumes "no images found" stays **fatal** | Delta requires it become **non-fatal** |
| `main.go` wiring | Step 9 touches `main.go:36-37` | touches `main.go:27-34` for live-reload of `interval` |
| `s.interval` | `advanceDuration` reads it on the main goroutine | becomes **runtime-mutable** under hotplug |

Two consequences neither spec currently owns:

1. **A new cross-feature race.** If `interval` becomes runtime-mutable while `scheduleAdvance`
   reads it on the main goroutine, that is a data race introduced by the *combination*, invisible
   to either spec reviewed alone.
2. **A direct semantic contradiction.** `usb-stick-hotplug` currently treats a video-only USB
   stick as "no supported images → ignored". The moment video-playback ships, that is silently
   wrong — an MP4-only stick should presumably work. Neither spec reconciles this.

Related, and sharpened by the collision: **video-only content plus missing ffmpeg is a crash
loop.** `Run()` returns a fatal error when zero slides are found (`slideshow.go:243-245`),
`main.go` exits non-zero, and `tty1-guard.sh` restarts it every 30 s forever. This behaviour is
pre-existing and the Analyst explicitly preserves it — but this feature makes it newly *reachable*
for a user whose content is all video and whose ffmpeg is missing. `usb-stick-hotplug` wants that
same path made non-fatal, so the fix belongs to whichever design lands second.

**Recommendation: sequence the two features, or jointly architect ownership of `Reload`,
`scanSlides`, and `Run()`'s empty-directory contract, before either enters Phase 4.**

---

## Empirical verification (ffmpeg/ffprobe 8.1.2)

Between the two rounds, ffmpeg was obtained temporarily (`nix shell nixpkgs#ffmpeg`) and every
command line in the plan was executed. This closed the largest residual-risk category — Round 2
independently listed "all actual ffmpeg/ffprobe runtime behaviour" as unverifiable — and it also
**falsified three fixes Round 1 had just applied.**

| Claim under test | Result |
|---|---|
| Round 1: "passing `-show_entries` twice is not the documented syntax" | **WRONG — retracted.** ffprobe accumulates across repeated flags; both forms emit the same sections. The real defect in Phase 2's command was that it never requested `stream=duration`. |
| Round 1: "pass the path after a `--` terminator" | **WRONG — removed.** `ffmpeg -i -- file` fails with `Error opening input file --.` (exit 254); ffmpeg has no options terminator. `filepath.Abs` alone is the fix. Round 2's V4 reached the same conclusion independently. |
| Round 1: "cap the frame rate with `fps=30`" | **WRONG — corrected to `fps='min(30,source_fps)'`.** The `fps` filter is a rate *converter*: against a 25 fps source, `fps=30` produced 90 frames where the source had 75 — **raising** pipe traffic 20 % on the most common signage frame rate, the exact opposite of the throughput goal. Verified the `min()` form leaves 25 fps untouched and clamps 60 fps to 30. |
| B2: does `format.duration` really overshoot? | **CONFIRMED, and worse than argued.** A real 3 s-video / 5 s-audio clip reports `format.duration` `"5.000000"` against `streams[0].duration` `"3.000000"` — a 67 % overshoot. |
| W2: is a leading-dash filename actually exploitable? | **CONFIRMED.** `-dash.mp4` fails with `Missing argument for option 'dash.mp4'`; `./-dash.mp4` succeeds. |
| Phase 2: "advance on end-of-stream" | **FALSIFIED — timing model inverted.** `-re` finishes reliably early: 2.42 s for a 3.0 s clip (×3 runs), 1.51 s for a 2.0 s clip. ~0.5 s early on *every* clip, not an edge case. Advancing on EOS would cut every video short. The rule is now `max(EOS, probed duration)`. |
| Is `-re` needed at all? | **YES.** Without it the same 3 s clip decodes in 99 ms. |
| Frame-size contract (`w*h*4`, `io.ReadFull`) | **EXACT.** 27 648 000 bytes = 90 × 320×240×4, zero remainder. |
| Rejection cases exit non-zero? | **NO — important.** Audio-only and HEVC both exit **0** and must be rejected at parse time. Only a corrupt file exits 1, and it still prints a valid `{}` on stdout. |

All four real JSON outputs are now embedded in `Architect.md` → *ffprobe contract* and are the
fixtures Step 17 tests against.

---

## Round 2 (fresh-eyes) — findings

An independent agent with a fresh context window found **5 additional blockers and 9 warnings**
that Round 1 missed, plus the cross-spec collision above. All are applied.

The two most valuable — V2 and V3 — are **defects that Round 1's own fixes introduced**, which is
exactly the failure mode a second round exists to catch. Round 1 added a negative-result cache and
a `stopPlayback` teardown without stating goroutine ownership for either; both are reachable from
`Reload` on a background goroutine, and the map case is an unrecoverable Go runtime fatal rather
than a benign race.

### Round 2 Blockers (applied)

| ID | Finding | Where fixed |
|----|---------|-------------|
| V1 | Step 8 calls `s.vid.probe`/`firstFrame` but the step that adds the `vid` field ran *after* it; declared deps were "3, 5, 7". Tree would not compile in the stated order. | Step reordered to **7b** (before Step 8); Architect.md *Dependencies* + *Parallelisation* |
| V2 | `stopPlayback()` is called from `Reload`, which runs on a background goroutine (`go s.Reload()`, `slideshow.go:319`; and from `Schedule`), mutating `playerCancel` with no declared ownership — races `show`'s main-goroutine write and violates CLAUDE.md's `fyne.Do()` rule. | Architect.md new *Goroutine ownership*; tasks.md Steps 12, 16 |
| V3 | Round 1's own negative-result cache is written from `scanSlides`, called from `Run()` (main) **and** `Reload` (background) — a concurrent map write, i.e. an unrecoverable runtime fatal. | Architect.md *Goroutine ownership* + *Data Model Changes*; tasks.md Step 8 |
| V4 | `--` is not a real ffmpeg option terminator; if rejected, every probe fails and the whole feature silently no-ops. Also meaningless where the path is passed via `-i`. | Removed throughout — Architect.md *ffprobe contract*, *Subprocess hygiene*, API table; tasks.md Steps 3, 5, 6. Independently confirmed by measurement. |
| V5 | Step 22's fixture command (`ffmpeg -f lavfi -i testsrc`) has no `-t`, no encoder, no output path — `testsrc` is infinite, so the test never terminates, and the fixture would not be H.264 anyway. | Already rewritten during empirical verification; Step 22 now uses the exact tested command |

### Round 2 Warnings (applied)

| ID | Finding | Where fixed |
|----|---------|-------------|
| V6 | The "no `go.mod` change" claim is false if Step 21b imports `testify` (currently indirect) — it would be promoted to a direct requirement. | Step 21b restricted to stdlib `testing` + `fyne.io/fyne/v2/test`; Architect.md *Files to Modify* note. `go mod tidy` cleanliness is now an acceptance check. |
| V7 | The single-slot handoff rationale was backwards: latest-frame-wins means the reader *never* blocks, so it does not inherit pipe backpressure. Pacing comes entirely from `-re`; the handoff bounds memory, not rate. | Architect.md *Frame delivery* rewritten; the 99 ms-without-`-re` measurement corroborates |
| V8 | No frame-buffer reuse — a fresh `*image.RGBA` per frame is ~250 MB/s of garbage at 1080p30. GC cost hides behind a flat RSS, so Step 22b's check would have missed it. | Architect.md *Frame delivery*; tasks.md Step 6 (free-list of 2–3 buffers) |
| V9 | The first video slide plays at the wrong resolution on every boot: `Run()` shows slide 0 *before* `ShowAndRun()`, so `Canvas().Size()` is the pre-layout 800×450. Images self-correct; video bakes it in for the whole playthrough. | tasks.md Step 11 — defer the initial show onto the event loop, re-read `winSize()` at playback start |
| V10 | Steps 10 and 11 are mutually dependent; only 11 → 10 was declared. Neither half compiles alone. | Architect.md *Dependencies* + *Parallelisation*; tasks.md Steps 10, 11 — must land as one commit |
| V11 | Step 21 had no testable seam — advance-duration selection lived inside `show`, which needs a live canvas. | New pure `advanceDuration` helper in the API contract; tasks.md Steps 10, 21 |
| V12 | Step 21b cannot call `Run()` (blocks in `ShowAndRun`); tests must hand-construct `advanceTimer`/`img`/`winSize`, which nothing said. | tasks.md Step 21b |
| V13 | No rollback story for a brownfield change to an unattended device. | Architect.md new *Rollback* section; tasks.md Step 24 documents the ffmpeg-removal kill switch |
| V14 | `play`'s stderr was unspecified — an undrained `StderrPipe` makes ffmpeg block once 64 KB fills, self-inflicting the wedge the watchdog exists to catch. | tasks.md Step 6 — bounded `cmd.Stderr` writer, drained by `os/exec`'s own copier |

### Round 2 Notes

- **Logs are write-only.** `.xinitrc` runs `exec ./mural` with no redirection or rotation, so
  "excluded from rotation, logged as invalid" means the operator never sees it. **Applied** — Step
  24 documents running `./mural` from an SSH shell to diagnose a silently-missing file.
- **Windows console flash.** Spawning a subprocess per slide from a `-H=windowsgui` build flashes
  a console unless `SysProcAttr{HideWindow:true}` — which contradicts the "no Windows-specific
  code" claim. Latent (Mural is not built that way today, and the release matrix is Linux-only).
  **Applied** as a recorded caveat in the Windows paragraph; no step required.
- **Video-only content + missing ffmpeg is a crash loop.** Pre-existing fatal-on-empty behaviour,
  newly reachable. **Recorded** under the cross-spec collision above, since `usb-stick-hotplug`
  wants the same path made non-fatal.
- **`status.json` was stale** (25 steps vs 28). **Applied.**
- **Lock table omitted Steps 23, 24, 25, 22b.** **Applied.**

### Round 2 overrode Round 1

Per the skill's conflict rule, Round 2 wins by default. Two Round 1 fixes were undone:

- **The `--` terminator (Round 1, W2 fix → Round 2 V4).** Round 1 instructed passing paths after
  `--`. Round 2 flagged it as unverified and risky; measurement then proved it fails outright
  (`exit 254`). **Final state:** `filepath.Abs` only, no `--`, in all three commands.
- **The frame-delivery rationale (Round 1, B3 → Round 2 V7).** Round 1 justified the single-slot
  handoff by claiming it restores pipe backpressure. Round 2 showed the reasoning is inverted.
  The *fix itself stands* — the handoff is still required — but for a different reason: it bounds
  memory, while `-re` alone sets the rate. **Final state:** rationale rewritten; Step 22b's
  acceptance criteria now rest on the corrected model.

A third Round 1 claim was retracted without a Round 2 counterpart: the assertion that duplicated
`-show_entries` flags are invalid syntax (see *Empirical verification*).

### Round 2 residual risks (not closable offline)

- **Verified on ffmpeg 8.1.2 only.** Raspberry Pi OS ships 5.x/7.x. Step 22b re-checks the two
  version-sensitive assumptions (`fps='min(30,source_fps)'` and a populated `stream=duration`),
  each with a documented fallback.
- **Fyne's texture-upload cost at 30 fps on a Pi 3 is still unmeasured.** Playback was verified as
  a byte stream, not on a GL surface. Step 22b is the gate.
- **Fragmented-MP4 `N/A` duration** — the stream-then-format fallback order is reasoned, not
  measured against a fragmented file.

---

## Round 1 (self-critique) — findings

Reviewed against the actual source at HEAD (`slideshow.go`, `cec.go`, `main.go`,
`schedule.go`, `install.sh`, `README.md`, `docs/INSTALL.md`,
`.github/workflows/build.yaml`), not against the spec in isolation.

**Verdict: Needs revision.** 8 blockers + 9 warnings. Every finding is
specification-tightening *within* the chosen design — none of them invalidate the
ffmpeg-subprocess approach, so the spec does **not** go back to `sdd-architect`.

### 🔴 Blockers

| ID | Lens | Finding | Where fixed |
|----|------|---------|-------------|
| B1 | Correctness | **The ffprobe JSON contract is never defined, so Steps 2, 3 and 17 are unimplementable as written.** Step 2 says `parseProbeOutput` "parses ffprobe JSON" without stating the shape; Step 17's "Done when" demands table tests against that shape. A pure function cannot be tested against an undefined input. Separately, Step 3's command never requests `stream=duration`, which is the root cause of B2. *(This finding originally also claimed the duplicated `-show_entries` flag was invalid syntax — **that part is retracted**, see* Verification *below.)* | Architect.md → new *ffprobe contract* subsection with the exact command and four real JSON samples; tasks.md Steps 2, 3, 17 |
| B2 | Correctness | **Duration is read from `format.duration`, which overshoots the video stream and makes playback advance early.** For an MP4 whose audio track is longer than its video track, `format.duration` > video-stream duration. Playback uses `-an`, so end-of-stream fires at the *video* end — before `format.duration` elapses. That directly violates the Analyst rule "must not advance before the video's actual duration has elapsed". | Architect.md *Timing model*; tasks.md Steps 2, 3, 14 — prefer `stream.duration`, fall back to `format.duration`, and add an early-EOS floor |
| B3 | Correctness | **Unbounded frame delivery through `fyne.Do`.** Architect.md flagged this and deferred the ruling to Phase 3. Ruling: it is a blocker. The OS pipe buffer (64 KB) is far smaller than one 1080p RGBA frame (8.3 MB), so ffmpeg does get write-backpressure — but only against the *reader* goroutine, which hands each frame to `fyne.Do` and immediately reads the next. Nothing bounds the `fyne.Do` queue, so on a Pi that cannot keep up the queue grows without limit until OOM. The generation counter makes stale frames cheap to *drop* but does not bound the *queue*. | Architect.md new *Frame delivery* subsection; tasks.md Step 13 — single-slot latest-frame-wins handoff |
| B4 | Architecture Fit | **`onDone` advance is not marshalled onto the Fyne main goroutine, and has no `paused` guard.** Per the API contract, `onDone` fires on the playback goroutine. Step 14 says "advance to the next slide" — advancing calls `show`, which `slideshow.go:208` documents as "must be called from the Fyne main goroutine". Also, the existing ticker goroutine guards with `if s.paused { return }` (`slideshow.go:276`); the video completion path has no equivalent, so a race between `pause()` and a clean ffmpeg exit un-blanks a screen that the schedule just turned off. | tasks.md Step 14 |
| B5 | Security / Ops | **No `cmd.Wait()` on the playback and thumbnail subprocesses → one zombie per slide shown.** `exec.CommandContext` kills the child on cancel but the process stays a zombie until reaped. This is a kiosk explicitly designed to run unattended for months; at one video slide per 30 s that is ~86 k zombies in 30 days, exhausting the process table. Steps 5 and 6 never mention reaping. | tasks.md Steps 5, 6 |
| B6 | Correctness | **Timer lifecycle is underspecified and one existing call site is missed.** Three concrete gaps: (a) `pause()` currently calls `s.ticker.Stop()` (`slideshow.go:150-152`) — nothing in the plan says the replacement must stop `advanceTimer`; (b) `Run()` calls `s.showFast(0)` at `slideshow.go:265` and that call site appears nowhere in tasks.md, so the `showFast` → `show(index, instant)` rename leaves it dangling; (c) Architect.md's dependency graph says "Step 11 depends on Step 10 (`show` calls `scheduleAdvance`)" but Step 11's own text never mentions `scheduleAdvance` — nothing in the plan ever arms the first advance, because `time.NewTicker` self-started and `time.Timer` will not. | tasks.md Steps 10, 11, 15 |
| B7 | Parallelisation Safety | **Step 9 modifies `slideshow.go` but is absent from the `slideshow.go` file-lock list.** Step 9 adds the `vid *Video` field and extends `NewSlideshow` — both in `slideshow.go` — yet Architect.md's lock table lists only "Steps 7, 8, 10, 11, 12, 13, 14, 15, 16", and the *Parallelisation Opportunities* text asserts Step 9 "touches a file nothing else in the plan touches". Acting on that would let Step 9 run concurrently with Step 7 or 8 and clobber the file. This also falsifies the Architect checklist line "No step requires modifying multiple unrelated files". | Architect.md *Parallelisation Opportunities* + checklist; tasks.md Step 9 split |
| B8 | Testing | **Step 20's stated assertion contradicts existing behaviour.** It asks to assert "exclusion-with-log" for "an unreadable/corrupt file" in a `scanSlides` test. But for *images*, `loadThumbnail` returns `nil` on decode failure (`slideshow.go:39-50`) and `scanSlides` appends the slide anyway (`slideshow.go:83-88`) — corrupt images are **included** with a nil thumbnail, not excluded. Exclusion is new *video-only* behaviour. As written the test asserts the opposite of the code for half its cases. | tasks.md Step 20 |

### 🟡 Warnings

| ID | Lens | Finding | Disposition |
|----|------|---------|-------------|
| W1 | Completeness | **The Raspberry Pi throughput risk was explicitly deferred to Phase 3 and needs a ruling, not another flag.** | Ruled — see *Ruling: Raspberry Pi throughput* below. Applied to Architect.md and tasks.md. |
| W2 | Security | **A content filename beginning with `-` is parsed by ffmpeg as an option.** `filepath.Join(s.dir, name)` normally yields `content/foo.mp4`, but Mural takes the content dir as a positional argument, so `./mural .` produces the bare name `-foo.mp4`. `content/` is network-writable (Samba share, `install.sh` §7), so the filename is attacker-influenced. This risk is *new* — the image path only ever passes names to `os.Open`, which is safe. | Applied — Step 3/5/6 must pass an `filepath.Abs`-resolved path |
| W3 | Data Integrity | **Rejected videos are re-probed on every scan.** Excluded files never enter `[]Slide`, so the `path`+`size`+`mtime` reuse cache (`slideshow.go:79`) never covers them. `Reload` runs at every schedule turn-on *and* on every Home keypress, so a few HEVC or corrupt MP4s left in `content/` add a fresh ffprobe timeout to every Home press. | Applied — negative-result cache keyed the same way |
| W4 | Architecture Fit | **No timeout value is specified anywhere, and `firstFrame` has no timeout at all.** Step 3 says "under a `context.Context` timeout" without a number; Step 5 omits it entirely. Both run synchronously inside `scanSlides`, which runs inside `Reload`, which runs at schedule turn-on — a wedged ffmpeg stalls the display coming on. | Applied — explicit constants |
| W5 | Correctness | **A degenerate single-video rotation can spin.** Analyst edge case: a lone video "plays, finishes, then replays itself". With `len(slides) == 1` and a sub-second clip, that is a process spawn every few tens of milliseconds. Analyst forbids a duration *clamp*, but a restart floor is not a clamp on the slide's duration. | Applied — minimum re-arm floor documented |
| W6 | Testing | **Playback/pause/key-handler being manual-only is not sufficient for the timer refactor.** Architect asked Phase 3 to rule on this. Ruling: the ticker→timer refactor is the single highest-regression item in the plan and it changes the *image* path that every existing user depends on. Fyne ships `fyne.io/fyne/v2/test` with `test.NewApp()`, and `testify` is already in `go.mod`. Headless coverage of pause/resume/nav timer state is cheap and directly pins the regression. | Applied — new Step 21b |
| W7 | Scope | **Step 11's "incidental correctness fix" is scope creep relative to Analyst.md**, which never asks for the decode-off-the-UI-thread change. It is nonetheless *necessary* — the ticker goroutine whose body contains the offending `decodeAndFit` call is being deleted wholesale. Keep it, but it must be called out as behaviour-preserving and manually verified. | Kept, with explicit no-visible-change constraint |
| W8 | Completeness | **Existing installs get no upgrade path.** `install.sh` only runs on new installs; Step 23 adds `ffmpeg` there. An existing user who drops in an MP4 gets a log line on a headless kiosk they will never read. Ruling: silent-skip-with-log is the right *runtime* failure mode (an on-screen error would violate "must never crash or block the rotation"), but the docs must tell existing users to run `sudo apt install ffmpeg`. | Applied — Step 24 |
| W9 | Completeness | **`docs/INSTALL.md` has a second place naming the package list** — line 41, "Install required packages (`xinit`, `ratpoison`, `cec-utils`)" — and line 53, "copy JPG or PNG images". Step 24 says "document MP4 support" without naming these lines, so they are easy to miss. | Applied — Step 24 made line-specific |

### 🟢 Notes

- **N1 — `CLAUDE.md` is stale in two places** that Step 25 touches anyway: it names `.github/workflows/release.yaml` (the real file is `build.yaml` — Architect.md gets this right), and it describes the Samba share as "anonymous read/write" when `install.sh` sets `guest ok = no` with `valid users`. Cheap to fix while Step 25 is open. **Applied.**
- **N2 — `image.RGBA` is alpha-premultiplied in Go; ffmpeg's `rgba` is straight alpha.** Harmless here because video has no alpha channel and ffmpeg emits `A=255`, but worth one line in `video.go` so nobody "fixes" it later. **Applied as a comment instruction in Step 6.**
- **N3 — RGBA is the correct output format for Fyne**, despite being the widest. Handing `canvas.Image` an `*image.YCbCr` would make Fyne's painter convert to RGBA in Go on the UI thread — strictly worse than letting ffmpeg's SIMD-optimised swscale do it. The Architect's instinct to reach for `yuv420p` as an optimisation would have been a pessimisation. **Recorded, no change.**
- **N4 — Architect.md's dependency line "Step 21 on Step 10" is incomplete** (advance-duration selection also depends on Steps 13/14). Harmless — 13/14 already precede 21. **Recorded, no change.**

### Ruling: Raspberry Pi throughput (W1)

Architect.md deferred this explicitly: *"Phase 3 should decide whether a documented
fallback belongs in this phase or the next."* The ruling:

**In scope for this phase** — three changes, all zero-config and cheap:
1. **Scale inside the ffmpeg filter graph** (already in the design) — the pipe carries
   window-sized frames, never source-sized ones. Keep.
2. **Explicit output frame-rate cap** appended to the filter graph. Uncapped, a 60 fps
   source at 1080p is ~500 MB/s through the pipe; capped at 30 it is ~250 MB/s and a
   60 fps source degrades to smooth-enough rather than to a stall.
3. **Single-slot latest-frame-wins handoff** (B3) — this is what actually makes the
   system degrade gracefully instead of collapsing. Under-capacity hardware drops
   frames and keeps real-time playback; it does not queue and OOM.

**Out of scope, deliberately** — with reasons recorded so this is not re-litigated:
- **Hardware decode** (`-hwaccel v4l2m2m` / `drm`). Availability varies by Pi model and
  Pi OS release; a missing or broken hwaccel makes ffmpeg *fail* rather than fall back,
  which would break the graceful-degradation constraint that the whole design rests on.
- **A `yuv420p` + shader path.** Requires a custom Fyne painter. See N3 — it is also
  probably slower, not faster, given Fyne uploads RGBA regardless.
- **A `config.toml` playback-quality knob.** Nothing else in Mural asks the operator to
  tune performance, and Architect.md's "No config.toml changes" holds.

**Accepted residual risk:** a Pi 3 at 1080p is expected to drop frames. That is now a
*designed* degradation (item 3) rather than an unbounded failure. The plan gains one
explicit on-hardware verification step (Step 22b) with a documented operator fallback —
re-encode the source clip at a lower resolution — rather than a code-level fallback.
