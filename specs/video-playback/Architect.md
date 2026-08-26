# Architect: Video Playback

> Phase 2 — Design decisions. Approved before coding begins.
> Implementation checklist is in tasks.md.

## Approach

### Chosen design: wrap the `ffmpeg`/`ffprobe` CLIs and stream frames into the existing Fyne `canvas.Image`

Mural already has an established pattern for "capability provided by an external CLI, degrade
gracefully when absent": `cec.go` wraps `cec-client`, probes it with `exec.LookPath` at
construction, and turns every operation into a silent no-op when the binary is missing. Video
support follows exactly that pattern in a new `video.go` with a `Video` struct.

Three responsibilities, all delegated to the CLI:

1. **Scan-time metadata** — `ffprobe` yields codec name, pixel dimensions, and exact duration.
   A file that is not `h264`, has no video stream, or reports a zero/unreadable duration is
   rejected at scan time and never enters the rotation (Analyst edge cases 1 and 8).
   The exact command and the JSON shape it must parse are pinned in *ffprobe contract* below.
2. **Scan-time thumbnail** — `ffmpeg -frames:v 1` writes the first frame as PNG to stdout; it is
   decoded with the stdlib `image` decoder already imported by `slideshow.go` and resized with
   `nfnt/resize`, producing exactly the same kind of `image.Image` thumbnail an image slide has.
   Downstream code cannot tell the difference, so instant-nav preview works unchanged.
3. **Playback** — during display, `ffmpeg` decodes the clip, scales it to the window inside the
   filter graph, and writes raw RGBA frames to stdout. A reader goroutine assembles each frame
   into an `*image.RGBA` and pushes it to the existing `s.img` `canvas.Image` through `fyne.Do`.

**Why frames-into-the-existing-canvas rather than spawning a fullscreen external player.**
Handing playback to `mpv`/`vlc`/`ffplay` in its own X window is less code, but it breaks four
guarantees the Analyst holds fixed: keyboard focus moves to the player so Left/Right/Home/Delete
stop reaching Fyne's `SetOnTypedKey`; `Pause()` can no longer black the screen because a foreign
window is stacked above it; "fit to the window the same way images are fit today" becomes the
player's fit policy, not Mural's; and window stacking under ratpoison is fragile on a kiosk that
must run unattended for months. Streaming frames into the one `canvas.Image` that already exists
keeps every existing mechanism intact — the generation counter, `canvas.ImageFillContain`,
`Pause`'s `s.img.Image = nil`, and the single key handler.

**Why not a pure-Go or CGo-linked decoder.** A pure-Go H.264 decoder cannot sustain 1080p on a
Raspberry Pi. A CGo binding (libmpv, libvlc, gstreamer) would force `.github/workflows/build.yaml`
to cross-install arm64 and armhf `-dev` packages for the media stack on top of the GL/X11 set it
already installs, and would need a matching DLL story on Windows. The `ffmpeg` subprocess keeps
`go build` and the release matrix completely unchanged — no new Go dependency, no new cgo linkage,
no CI change at all.

**Windows answer to the Analyst's open question.** The design is portable: `exec.LookPath` finds
`ffmpeg.exe`/`ffprobe.exe` on PATH, and the rawvideo pipe behaves identically. Windows is
supported when ffmpeg is installed and silently image-only when it is not. Nothing in the design
is POSIX-only — process kill, pipe semantics and path handling all behave the same.

**One caveat, added in Phase 3 (Round 2).** The claim "no Windows-specific code path is
introduced" is not quite free. Mural spawns a subprocess per slide, and a GUI-subsystem Windows
build (`-ldflags -H=windowsgui`) flashes a console window on every spawn unless the child is
started with `SysProcAttr{HideWindow: true}` — which *is* Windows-specific code. This is latent
rather than live: Mural is not currently built with `-H=windowsgui`, and the release matrix
(`.github/workflows/build.yaml`) publishes Linux binaries only. Recorded so that whoever first
ships a Windows GUI build knows to add it; no step in this plan is required to.

### ffprobe contract

*(Added in Phase 3, Round 1 — blocker B1. `parseProbeOutput` is a pure function whose whole
value is being unit-testable, which is impossible while its input shape is undefined. **Every
command and JSON sample below was executed against ffmpeg/ffprobe 8.1.2 and is transcribed from
real output, not from documentation.**)*

```
ffprobe -v error -select_streams v:0 \
  -show_entries stream=codec_name,width,height,duration:format=duration \
  -of json <abs-path>
```

Two things about this command line matter and both were got wrong earlier in the process:

- **`stream=duration` must be requested.** The Phase 2 command asked only for
  `stream=codec_name,width,height` and got the duration from `format=duration`. That is the root
  of blocker B2 — see *Duration source* below.
- **Do not pass a `--` terminator.** Round 1 of Phase 3 added one; testing shows `ffmpeg -i --
  file` fails with `Error opening input file --.` (exit 254) because ffmpeg treats `--` as the
  filename. ffmpeg has no end-of-options terminator. `filepath.Abs` alone is the fix (see
  *Subprocess hygiene*). Verified: a file named `-dash.mp4` passed bare fails with
  `Missing argument for option 'dash.mp4'`; passed as `./-dash.mp4` it succeeds.

*(Using one combined `-show_entries stream=...:format=...` flag versus two separate
`-show_entries` flags makes no difference — ffprobe accumulates across repeated flags. Round 1
claimed otherwise; that claim was wrong and is retracted in `CriticReview.md`.)*

**Real output, valid h264 clip** (note `programs` **and** `stream_groups`, both empty, both
present in 8.x; `encoding/json` ignores them):

```json
{
    "programs": [],
    "stream_groups": [],
    "streams": [
        { "codec_name": "h264", "width": 640, "height": 480, "duration": "3.000000" }
    ],
    "format": { "duration": "5.000000" }
}
```

Types are not uniform and this is the part most likely to be got wrong: `width` and `height`
decode as JSON **numbers** (`int`), while `codec_name` and both `duration` fields are JSON
**strings** requiring `strconv.ParseFloat`.

**Real output, audio-only file** — the "no video stream" case. `streams` is an empty array and
**ffprobe still exits 0**, so a non-zero exit code cannot be the only rejection signal:

```json
{ "programs": [], "stream_groups": [], "streams": [], "format": { "duration": "2.000000" } }
```

**Real output, HEVC-in-MP4** — exits 0, so this too is a parse-time rejection, not an exec error:

```json
{ "streams": [ { "codec_name": "hevc", "width": 320, "height": 240, "duration": "1.000000" } ],
  "format": { "duration": "1.000000" } }
```

**Real output, corrupt file named `.mp4`** — stdout is a valid but *empty* JSON object, stderr
carries `moov atom not found`, and the exit code is **1**. `parseProbeOutput` must therefore
survive a document with `streams` and `format` entirely **absent** (nil slice, zero struct) and
return a clean error rather than nil-dereferencing:

```json
{

}
```

**Duration source, in priority order** *(blocker B2)*: prefer `streams[0].duration`; fall back to
`format.duration` only when the stream duration is absent or unparseable. This is not theoretical
— the sample above is a real clip with 3 s of video and 5 s of audio, and `format.duration`
reports **5.000000** against a true video length of **3.000000**. `format.duration` is the
*container* duration and includes the audio track. Playback runs with `-an`, so the video ends at
3 s. Taking the container figure would have made every such clip hold a frozen final frame for
two full seconds.

**Rejection rules** (all produce a wrapped error, all cause `scanSlides` to exclude the file):
non-zero ffprobe exit; absent or empty `streams`; `codec_name != "h264"`; `width` or `height`
`<= 0`; no parseable duration from either source; parsed duration `<= 0`. Malformed JSON is
likewise an error, never a panic.

### Timing model

The single global `time.Ticker` that currently advances every slide is replaced with a
per-slide `time.Timer`, armed by a `scheduleAdvance(d)` helper each time a slide is shown.
Image slides arm it with `s.interval` — identical behaviour to today. Video slides do not arm
it with `interval` at all; playback completion (end of the ffmpeg output stream) is the advance
trigger, with the timer armed at `duration + grace` purely as a watchdog so a hung or wedged
`ffmpeg` cannot freeze the rotation forever. This satisfies "must not advance before the video's
actual duration has elapsed, and must advance at that point regardless of `interval`" while
preserving the graceful-degradation constraint.

A `time.Timer` rather than a `time.Ticker` is required because the per-slide duration is no
longer constant; the existing `ticker.Reset(s.interval)` calls in the key handler become
`scheduleAdvance` calls.

**Timer lifecycle** *(Phase 3, blocker B6 — the plan previously never said what arms the first
advance).* `time.NewTicker` self-starts; `time.Timer` does not. Every path that displays a slide
must arm the next advance, and every path that stops the slideshow must stop the timer:

| Site | Today | After |
|------|-------|-------|
| `Run()` (`slideshow.go:260`) | `s.ticker = time.NewTicker(s.interval)` | no timer created up front |
| `Run()` (`slideshow.go:265`) | `s.showFast(0)` | `s.show(0, true)` — **this call site is missing from tasks.md; it must be updated** |
| `show()` end | — | calls `scheduleAdvance` (image: `interval`; video: watchdog only) |
| `pause()` (`slideshow.go:150-152`) | `s.ticker.Stop()` | `s.advanceTimer.Stop()` (nil-guarded) **and** `s.stopPlayback()` |
| `resume()` (`slideshow.go:168-170`) | `s.ticker.Reset(s.interval)` | drops the explicit reset — `show` re-arms |
| key handler ×3 (`slideshow.go:312,315,318`) | `s.ticker.Reset(s.interval)` | drops them — `show` re-arms |

Because `show` now owns arming, the four `ticker.Reset` sites collapse rather than convert
one-for-one. `advanceTimer` is only ever touched on the Fyne main goroutine, so it needs no lock.

**The probed duration is authoritative; end-of-stream is not** *(Phase 3 ruling on the Architect's
open `-re` question, revised after measurement).*

Phase 2 proposed advancing *on* end-of-stream, with the probed duration only as a watchdog.
Measured against ffmpeg 8.1.2, `-re` finishes reliably and substantially **early**:

| Clip (video-stream duration) | Wall-clock to EOS | Early by |
|---|---|---|
| 3.000 s @ 25 fps | 2.421 / 2.423 / 2.454 s | ~0.58 s (19 %) |
| 2.000 s @ 60 fps | 1.505 s | ~0.50 s (25 %) |

`-re` paces the *reading* of the input; the last frames drain out of the decoder and filter
buffers without any corresponding sleep, so EOS consistently precedes the true playback duration
by roughly half a second. Advancing on EOS would therefore cut **every** video short — not an
edge case, the normal path — violating "must not advance before the video's actual duration has
elapsed" on every single clip.

So the rule is inverted from Phase 2: **a video slide advances at
`max(end-of-stream, probed duration)`.** The probed duration governs; EOS merely reports that
ffmpeg is done. In practice the final frame stays on screen for the last ~0.5 s of the slide,
which on signage is an ordinary freeze-frame ending and visually unremarkable.

*(An earlier Phase 3 draft framed this as a 250 ms "early-EOS floor" for an exceptional case. The
measurements above show the deviation is ~0.5 s and universal, so the threshold framing was
dropped in favour of the unconditional `max()` rule.)*

`-re` is nonetheless **required**: without it the same 3 s clip decodes in 99 ms, which combined
with the single-slot handoff would show one frame and stop. Verified.

Watchdog grace remains **2 s** on top of the probed duration — since EOS reliably arrives *early*,
the watchdog now only fires when ffmpeg has genuinely wedged.

**Minimum re-arm floor** *(warning W5).* When a video slide advances to *itself* — the Analyst's
"only slide in the rotation is a video" case, where `(current+1) % 1 == current` — the next
showing is deferred so that consecutive playback starts are at least **1 s** apart. This is not
a duration clamp (which the Analyst excludes): the clip still plays in full and is never cut off
or extended. It only stops a sub-second lone clip from spawning ffmpeg dozens of times a second.

### Playback cancellation

Playback is owned by a `context.Context` stored on the `Slideshow` as `playerCancel`. A single
`stopPlayback()` helper cancels it (which kills the ffmpeg process) and is called from `show`,
`pause`, `Reload`, and the nav key handler. The existing `generation atomic.Int64` counter is
retained as the second line of defence: every frame push re-checks the generation it was started
under and drops the frame if it is stale, so a frame already in flight through `fyne.Do` at
cancellation time can never overwrite a newer slide. This is the mechanism the Analyst's
"rapid manual navigation" edge case and the generation-counter constraint both require.

### Goroutine ownership

*(Phase 3, Round 2 — blockers V2 and V3. Both are crash-class defects that Phase 3's own Round 1
fixes introduced, which is precisely what the fresh-eyes round exists to catch.)*

`Slideshow`'s mutable state is documented as "set during Run and accessed only on the Fyne main
goroutine, except via Pause/Reload which marshal through fyne.Do" (`slideshow.go:121-122`). Two
new fields break that invariant unless ownership is stated explicitly:

| Field | Owner | Hazard if unowned |
|-------|-------|-------------------|
| `playerCancel` | Fyne main goroutine **only** | `Reload` runs on a background goroutine — `go s.Reload()` from the Home key (`slideshow.go:319`) and from `Schedule` at turn-on (`main.go:39`). A bare `s.stopPlayback()` there races `show`'s write on the main goroutine. |
| the reject set | passed in, returned out — **never** shared mutable state | `scanSlides` is called from `Run()` on the main goroutine *and* from `Reload` on a background goroutine. A `s.rejected` map written in place is a **concurrent map write**: an unrecoverable Go runtime fatal, not a benign race. |

Neither needs a mutex, and neither should get one. `playerCancel` is fixed by wrapping the
out-of-thread caller: `fyne.Do(s.stopPlayback)`. The reject set is fixed by mirroring the pattern
already used for `existing []Slide` (`slideshow.go:184-190`) — snapshot in, new value out,
assigned inside the `fyne.Do` block that already swaps `s.slides` (`slideshow.go:201-205`).
Adding a lock instead would introduce the first lock in a file whose whole concurrency story is
"main goroutine plus `fyne.Do`", and would be inconsistent with `Schedule`, which does use a
mutex but only because it is genuinely shared across two background goroutines.

`go test -race` with `Run` and `Reload` scanning concurrently is the acceptance check for both.

### Frame delivery and Raspberry Pi throughput

*(Phase 3 ruling — blocker B3 and warning W1. Phase 2 flagged both and deferred the decision;
this section is that decision.)*

**The queue must be bounded.** A 1080p RGBA frame is 8.3 MB and the OS pipe buffer is 64 KB, so
ffmpeg *does* get write-backpressure — but only against the reader goroutine, which hands each
frame to `fyne.Do` and immediately loops to read the next. Nothing bounds the `fyne.Do` queue, so
hardware that cannot keep up accumulates frame closures until it dies. The generation counter
makes stale frames cheap to *drop*; it does not bound the *queue*.

The fix is a **single-slot, latest-frame-wins handoff**: the reader stores the newest frame into
a one-slot buffer and schedules a `fyne.Do` drain only if one is not already pending. A frame
that arrives while a drain is pending overwrites the slot and is never separately scheduled. At
most one frame is ever queued and at most one is ever in flight.

**What this does and does not do — the rationale matters, because an earlier draft of this
section had it backwards.** The handoff bounds **memory**, not rate. It does *not* make the
reader inherit the pipe's backpressure: latest-frame-wins means the reader never blocks on the
UI, so it drains the pipe as fast as ffmpeg fills it and the pipe never backs up. **All pacing
comes from `-re`**, and the measurement confirms it — the same clip that takes 2.42 s with `-re`
decodes in 99 ms without it. So: `-re` sets the rate, the single slot caps the queue at one
frame, and the UI simply consumes whatever is current when it gets around to it. Under capacity
nothing is dropped; over capacity the display drops frames and stays real-time instead of
drifting behind and then dying. Step 22b's acceptance criteria are written against *this*
reasoning, not the earlier inverted version.

**Frame buffers must be recycled** (warning V8). A fresh `*image.RGBA` per frame is roughly
250 MB/s of garbage at 1080p30. On a Pi that surfaces as GC CPU and frame jitter while RSS stays
flat — so the flat-RSS check alone would miss it. Keep a free-list of 2–3 identically-sized
buffers and recycle one only after the drain that consumed it has released it; never write into
a buffer the UI may still be reading. Two is the minimum (one displayed, one filling).

**Throughput measures adopted in this phase:**

1. **Scale inside the filter graph** (already in the Phase 2 design) — the pipe carries
   window-sized frames, never source-sized ones. Retained.
2. **Explicit output frame-rate cap of 30 fps**, appended to the filter graph as
   `...,fps='min(30,source_fps)'`. Uncapped, a 60 fps 1080p source pushes ~500 MB/s; capped it is
   ~250 MB/s, and a 60 fps source degrades to smooth-enough rather than to a stall.

   **The `min(...)` expression is load-bearing — a bare `fps=30` is actively harmful here.** The
   `fps` filter is a rate *converter*, not a cap: measured against a 25 fps source, `fps=30`
   duplicated frames to produce 90 where the source had 75, raising pipe traffic 20 % on the most
   common signage frame rate — the exact opposite of the intent. Verified against ffmpeg 8.1.2:
   with `fps='min(30,source_fps)'` a 25 fps source yields 75 frames (untouched) and a 60 fps
   source yields 60 over 2 s (clamped to 30).
3. **The single-slot handoff above**, which is what actually converts "too slow" from a crash
   into a degradation.

**Rejected in this phase, with reasons, so it is not re-litigated:**

- **Hardware decode** (`-hwaccel v4l2m2m` / `drm`). Availability varies by Pi model and Pi OS
  release, and a missing or broken hwaccel makes ffmpeg *fail* rather than fall back — which
  would break the graceful-degradation constraint the entire design rests on.
- **A `yuv420p` + shader path.** Would require a custom Fyne painter. It is also likely a
  pessimisation: Fyne uploads RGBA regardless, so handing `canvas.Image` an `*image.YCbCr` moves
  the colour conversion out of ffmpeg's SIMD swscale and into single-threaded Go on the UI thread.
- **A `config.toml` playback-quality knob.** Nothing else in Mural asks the operator to tune
  performance, and the "no config changes" decision below holds.

**Accepted residual risk:** a Pi 3 at 1080p is expected to drop frames. That is now a *designed*
degradation rather than an unbounded failure, and the operator fallback is documented (re-encode
the source clip smaller) rather than coded. Step 22b verifies this on real hardware.

### Subprocess hygiene

*(Phase 3 — blocker B5 and warning W2. Both are risks the image-only codebase never had, because
it only ever handed filenames to `os.Open`.)*

**Every subprocess must be reaped.** `exec.CommandContext` kills the child when the context is
cancelled, but a killed child remains a zombie until waited on. Mural is a kiosk explicitly built
to run unattended for months: at one video slide per 30 s that is roughly 86 000 unreaped children
in 30 days, which exhausts the process table long before anyone visits the device. `probe`,
`firstFrame` and `play` must each `Wait()` on every path, including the cancelled and error paths.
`CombinedOutput`/`Output` wait implicitly, so `probe` is safe if written that way; `play` streams
from a `StdoutPipe` and therefore **must** `Wait()` explicitly after the read loop ends.

**Filenames are attacker-influenced and must not be parsed as flags.** `content/` is a
network-writable Samba share (`install.sh` §7), so filenames come from outside the trust boundary.
`filepath.Join(s.dir, name)` normally yields `content/foo.mp4`, which is safe — but Mural takes the
content directory as a positional argument, so `./mural .` yields the bare name `-foo.mp4`, which
ffmpeg parses as an option. Confirmed against ffprobe 8.1.2: a file named `-dash.mp4` passed bare
fails with `Missing argument for option 'dash.mp4'` (exit 1).

**Resolve every path with `filepath.Abs` before handing it to a subprocess. Do *not* add a `--`
terminator** — ffmpeg has none, and `ffmpeg -i -- file` fails with `Error opening input file --.`
(exit 254), as it takes `--` for the filename. `filepath.Abs` alone closes this completely, since
an absolute path always begins with `/` (or a drive letter on Windows); `./` prefixing works too
and was also verified. This is *not* a shell-injection risk — `exec` does not use a shell — it is
argument-position confusion.

### Incidental correctness fix

The current auto-advance goroutine calls `decodeAndFit` *inside* `fyne.Do`, i.e. it decodes and
Lanczos-resizes a full-resolution JPEG on the UI thread every `interval`. The refactor in step 11
moves that decode into the background goroutine that `show` already uses. Visible behaviour is
deliberately preserved: auto-advance still swaps straight to the finished full image with no
thumbnail flash (`show(idx, instant=false)`), while manual nav still shows the thumbnail first
(`instant=true`). Video slides always show the first-frame thumbnail while ffmpeg warms up, per
the Delta.

## API Contract

Mural has no HTTP surface — it is a single-binary desktop application. The equivalent contract is
the internal Go API of the new `video.go` module and the changed `Slide` shape. No route, request
body, response, or auth applies.

| Symbol | Signature | Contract |
|--------|-----------|----------|
| `NewVideo` | `func NewVideo() *Video` | Probes `ffmpeg` and `ffprobe` on PATH. Logs one line and returns a disabled `*Video` if either is missing. Never returns nil, never errors. |
| `(*Video).probe` | `func (v *Video) probe(path string) (videoInfo, error)` | Runs `ffprobe` under a **10 s** `context.WithTimeout` (W4). Resolves `path` with `filepath.Abs` (W2 — no `--` terminator; ffmpeg has none). Errors when disabled, when the codec is not `h264`, when there is no video stream, or when duration/dimensions are zero or unparseable. Must `Wait()` the process (B5). |
| `parseProbeOutput` | `func parseProbeOutput(data []byte) (videoInfo, error)` | Pure. Parses the JSON shape pinned in *ffprobe contract* above and applies the validity rules there, including the stream-then-format duration priority. Unit-testable with no subprocess. |
| `(*Video).firstFrame` | `func (v *Video) firstFrame(path string, width uint) (image.Image, error)` | Returns the first decoded frame resized to `width` (aspect preserved), or an error. Runs under a **15 s** timeout (W4) — it is called synchronously inside `scanSlides`, which runs inside `Reload`, which runs at schedule turn-on. Same `filepath.Abs` handling and same `Wait()` requirement. |
| `fitDimensions` | `func fitDimensions(srcW, srcH int, winW, winH float32) (int, int)` | Pure. Contain-fit scale, mirroring `decodeAndFit`'s `math.Min` rule. Returns even dimensions, minimum 2, so the rawvideo frame size is exact and predictable. |
| `(*Video).play` | `func (v *Video) play(ctx context.Context, path string, w, h int, onFrame func(image.Image), onDone func(error))` | Blocking. Streams RGBA frames of exactly `w*h*4` bytes, calling `onFrame` per frame on the caller's goroutine. Muted (`-an`), paced at native rate (`-re`, verified required), capped via `fps='min(30,source_fps)'` in the filter graph. Frame size is exact: measured 27 648 000 bytes for 90 frames at 320×240 RGBA, zero remainder, so `io.ReadFull` of `w*h*4` is a sound contract. **No timeout** — clips are legitimately arbitrarily long, so `ctx` cancellation is the only stop signal; the wedged-ffmpeg case is covered by the `show`-side watchdog instead. Returns via `onDone(nil)` at clean end of stream, `onDone(err)` otherwise. Cancelling `ctx` kills the process; `play` must still `Wait()` before returning so no zombie is left (B5), and must return promptly. |
| `advanceDuration` | `func advanceDuration(sl Slide, interval time.Duration) time.Duration` | Pure. Returns `interval` for an image slide and `sl.duration` for a video slide. Added in Phase 3 (warning V11) purely as a testable seam — without it, advance-duration selection lives inside `show`, which needs a live Fyne canvas, and Step 21 has nothing to assert against. |
| `videoInfo` | `struct { duration time.Duration; width, height int }` | Scan-time metadata carried into the `Slide`. |
| `Slide` (changed) | `struct { path string; kind slideKindT; thumb image.Image; duration time.Duration; vidW, vidH int; size int64; mtime time.Time }` | `kind` distinguishes image from video. `duration`/`vidW`/`vidH` are zero for image slides. Reuse on `Reload` is still keyed on `path` + `size` + `mtime`, so cached video metadata is reused for free. |
| `NewSlideshow` (changed) | `func NewSlideshow(dir string, interval time.Duration, thumbWidth uint, cec *CEC, vid *Video) *Slideshow` | Gains the `*Video` dependency, mirroring how `*CEC` is injected today. |

## Data Model Changes

No database and no schema exist in this project — no migrations apply.

In-memory model changes only, all in `slideshow.go`:

- `Slide` gains `kind`, `duration`, `vidW`, `vidH` (see table above). Additive; existing fields
  keep their meaning, so the `Reload` reuse comparison is unchanged.
- `Slideshow` loses `ticker *time.Ticker` and gains `advanceTimer *time.Timer`,
  `playerCancel context.CancelFunc`, and `vid *Video`.
- `Slideshow` also gains a **negative-result cache** of rejected files (W3). Rejected videos never
  enter `[]Slide`, so the existing `path`+`size`+`mtime` reuse check (`slideshow.go:79`) can never
  cover them, and an HEVC or corrupt MP4 left in `content/` would otherwise pay a fresh ffprobe on
  *every* scan. `Reload` runs at each schedule turn-on **and on every Home keypress**, so without
  this a few bad files make Home visibly laggy. Keyed and invalidated exactly like the positive
  cache; entries for files no longer on disk are dropped each scan so it cannot grow unbounded
  over months of uptime.

  **It must be threaded through `scanSlides` as a parameter and return value, not held as a
  mutable field** — see *Goroutine ownership* (blocker V3). An in-place `map` write here is a
  concurrent map write between `Run()` and `Reload`, which is a hard runtime fatal.

**No `config.toml` changes.** The Analyst excludes audio configuration and excludes any
duration clamp on video slides, and `interval`/`thumb_width` keep their current meaning
(`interval` continues to govern image slides only). Nothing new needs configuring.

## Files to Create

| File | Purpose |
|------|---------|
| `video.go` | `Video` struct wrapping `ffmpeg`/`ffprobe`: availability probe, metadata probe, first-frame extraction, RGBA frame-streaming playback, and the pure `parseProbeOutput`/`fitDimensions` helpers. |
| `video_test.go` | Unit tests for `parseProbeOutput` and `fitDimensions`; an ffmpeg-gated integration test that generates a tiny clip and exercises probe/firstFrame/play. |
| `slideshow_test.go` | Unit tests for slide-kind classification, `scanSlides` (positive reuse, negative-result cache, and the two *distinct* invalid-file behaviours — corrupt images are included with a nil thumb, invalid videos are excluded), advance-duration selection, and headless `fyne.io/fyne/v2/test` coverage of the timer/pause/nav state added in Phase 3 (Step 21b). |

## Files to Modify

| File | Change |
|------|--------|
| `slideshow.go` | `Slide` fields; `videoExts` + `slideKind`; video branch in `scanSlides`; negative-result cache; `*Video` on the struct and constructor; ticker → timer + `scheduleAdvance`; `showFast` → `show(index, instant)` **including the `Run()` call site at line 265**; `stopPlayback`; video branch in `show`; single-slot frame handoff; advance-on-completion + watchdog; `pause`/`resume`/`Reload` playback teardown; key handler updated. |
| `main.go` | Construct `NewVideo()` and pass it to `NewSlideshow`. |
| `install.sh` | Add `ffmpeg` to the `apt install` package list; update the "Copy images (JPG/PNG)" next-step wording to mention MP4. |
| `README.md` | Document MP4 (H.264) support, the ffmpeg requirement, muted playback, and that video slides ignore `interval`. |
| `docs/INSTALL.md` | Same, in the Raspberry Pi walkthrough. |
| `CLAUDE.md` | Add `video.go` to Project Structure; update Architecture Notes (supported formats, timing model, playback cancellation) and the external-dependency list. Also fix two pre-existing stale statements found in Phase 3 (note N1): it names `.github/workflows/release.yaml` when the real file is `build.yaml`, and describes the Samba share as "anonymous read/write" when `install.sh` §7 sets `guest ok = no` with `valid users`. |

Deliberately **not** modified: `go.mod`/`go.sum` (no new Go dependency),
`.github/workflows/build.yaml` (no new cgo linkage, so the cross-compile matrix is untouched),
`cec.go`, `schedule.go`.

> **The `go.mod` claim is conditional** (Phase 3, warning V6). It holds only because the test
> steps are restricted to the stdlib `testing` package plus `fyne.io/fyne/v2/test`, which lives
> inside the already-required `fyne.io/fyne/v2` module. `testify` is present today as an
> **indirect** dependency; importing it from a test would promote it to a direct requirement and
> `go mod tidy` would rewrite `go.mod`. Step 21b therefore forbids it. `go mod tidy` leaving
> `go.mod` byte-identical is an acceptance check on that step.

## Dependencies Between Steps

- Steps 2–6 depend on Step 1 (the `Video` struct must exist before methods hang off it).
- Step 3 depends on Step 2 (`probe` delegates to `parseProbeOutput`).
- Step 6 depends on Step 4 (`play` is called with dimensions from `fitDimensions`).
- Step 7b depends on Step 1 (`Slideshow` cannot hold a `*Video` field before the type exists).
- Step 8 depends on Steps 3, 5, 7, **and 7b** — scanning needs `probe`, `firstFrame`, the new
  `Slide` shape, *and* the `vid` field to call them through. Phase 2 listed only "3, 5, 7", which
  in the original ordering meant the tree would not compile (blocker V1).
- Step 9 depends on Step 7b (`main.go` cannot pass an argument `NewSlideshow` does not take yet).
- Steps 10 and 11 are **mutually** dependent and land as one commit (warning V10): Step 10 deletes
  the ticker goroutine and needs `show` to arm the timer; Step 11 defines `show` and needs
  `scheduleAdvance` to call. Phase 2 declared only 11 → 10.
- Step 13 depends on Steps 6, 11, and 12 (the video branch needs `play`, the new `show`, and `stopPlayback`).
- Step 14 depends on Steps 10 and 13 (advance-on-completion arms the timer from the playback callback).
- Steps 15 and 16 depend on Steps 10 and 12 (they call `scheduleAdvance` and `stopPlayback`).
- Step 17 depends on Step 2; Step 18 on Step 4; Step 19 on Step 7; Step 20 on Step 8; Step 22 on Step 6.
- Step 21 depends on Steps 10, 13 and 14 (advance-duration selection is decided across the
  `show`/`play`/`onDone` path, not in `scheduleAdvance` alone).
- Step 21b depends on Steps 10–16 (it exercises the fully refactored timing and pause paths).
- Step 22b depends on everything through Step 16 — it is on-hardware verification of the built binary.
- Steps 23–25 depend on nothing in the code — they document decisions already fixed in this file.

## Parallelisation Opportunities

**File locks — steps touching the same file CANNOT run concurrently:**

- `video.go` — Steps 1, 2, 3, 4, 5, 6. Strictly sequential.
- `slideshow.go` — Steps 7, **7b**, 8, 10, 11, 12, 13, 14, 15, 16. Strictly sequential. This is the
  largest lock in the plan and the critical path. **Steps 10 and 11 additionally must land as one
  commit** (warning V10) — the dependency between them is mutual, not one-way, and neither half
  compiles alone.
- `main.go` — Step 9 only.
- `video_test.go` — Steps 17, 18, 22.
- `slideshow_test.go` — Steps 19, 20, 21, 21b.
- `install.sh` — Step 23. `README.md` + `docs/INSTALL.md` — Step 24. `CLAUDE.md` — Step 25.
  Each is a lock of one, held by one step; listed for completeness because their omission from
  this table was itself a Round 2 finding.
- Step 22b holds no file lock — it is on-hardware verification and produces no diff.

> **Corrected in Phase 3.** Two errors here, both of which would have caused real damage if the
> plan were parallelised as written. (Blocker B7) The old Step 9 bundled the `slideshow.go` struct
> and constructor change together with the `main.go` wiring, and the lock table omitted it from
> the `slideshow.go` list while the prose asserted it "touches a file nothing else in the plan
> touches" — running it concurrently with Step 7 or 8 would have clobbered `slideshow.go`.
> (Blocker V1) That step also had to move *ahead* of Step 8, since `scanSlides` cannot call
> `s.vid.probe` before the field exists. It is now **Step 7b** (`slideshow.go` only, before
> Step 8), and the `main.go` wiring is **Step 9**.

**Safe to run in parallel:**

- The `video.go` chain (1–6) and the docs chain (23–25) are fully independent.
- Step 9 (`main.go`) touches a file nothing else in the plan touches; it can run any time after
  Steps 1 and 7b, though the tree will not compile between Step 7b and Step 9 landing.
- Once Steps 1–9 are done, the test chains `video_test.go` (17, 18, 22) and `slideshow_test.go`
  (19, 20, 21, 21b) are independent of each other and can run in parallel.
- Steps 23, 24, and 25 touch three different files (`install.sh`, README+INSTALL, `CLAUDE.md`)
  and are mutually independent — except that Step 24 touches two files, both of which only it
  touches.

**Practical recommendation:** the `slideshow.go` lock means multi-agent parallelism buys very
little here. Run 1–16 sequentially and fan out only across the test and docs steps.

## Rollback

*(Added in Phase 3, Round 2 — warning V13. Brownfield change to a device that runs unattended
for months; the plan had no revert story.)*

There are three levels, in increasing order of cost:

1. **Remove ffmpeg from `PATH`** (`sudo apt remove ffmpeg`). `NewVideo()` returns a disabled
   `*Video`, every `.mp4` is skipped at scan time, and the player reverts to exactly its
   image-only behaviour. No binary change, no redeploy, doable over SSH in one command. **This is
   the field kill switch** and is the only one a remote operator can realistically perform.
2. **Remove the video files** from `content/` (or over the Samba share). Same effect, narrower.
3. **Downgrade the binary.** `install.sh` fetches `releases/latest` unconditionally and has no
   downgrade flag, so this means manually `curl`-ing a prior release asset over `~/mural/mural`.
   Only needed if the ticker→timer refactor itself regresses image playback — which is what Step
   21b exists to prevent.

Level 1 must be documented in `docs/INSTALL.md` (Step 24); the other two are self-evident once
it is.

## Risks & Open Questions

Phase 2 listed seven risks for Phase 3 to rule on. All seven have now been ruled on; the
resolutions are recorded here and the full findings are in `CriticReview.md`.

### Resolved in Phase 3

- **Raspberry Pi throughput** — *was the single biggest unknown.* **Resolved:** see *Frame
  delivery and Raspberry Pi throughput*. Adopted in this phase: filter-graph scaling (retained),
  a 30 fps output cap, and a single-slot latest-frame-wins handoff. Rejected with reasons:
  hardware decode, a `yuv420p`+shader path, and a config knob. Residual risk (a Pi 3 dropping
  frames at 1080p) is now a designed degradation with a documented operator fallback, verified
  on real hardware by Step 22b.
- **`-re` pacing accuracy** — **Resolved:** grace is 2 s; an EOS arriving more than 250 ms early
  defers the advance to the probed duration. See *Grace and the early-EOS floor*.
- **Frame delivery through `fyne.Do` is unbounded** — **Resolved: yes, the single-slot buffer is
  required.** This was raised as a "should scrutinise" but is a genuine unbounded-growth bug on
  under-capacity hardware. See *Frame delivery*.
- **Scan cost on a large content directory** — **Resolved: per-file timeouts are sufficient;
  scanning stays sequential.** 10 s for `probe`, 15 s for `firstFrame`. Concurrency was rejected
  as complexity the Analyst does not ask for. One gap Phase 2 missed: *rejected* files are not
  covered by the reuse cache at all, so a negative-result cache was added (see *Data Model
  Changes*) — without it, every Home keypress re-probed every bad file.
- **Ticker → timer refactor touches the existing image path** — **Confirmed as the highest
  regression risk, and Step 21 alone was not enough.** Step 21b adds headless `fyne.io/fyne/v2/test`
  coverage of the pause/resume/nav timer state. Phase 2 also missed the `Run()` call site at
  `slideshow.go:265` and never said what arms the first advance; both are fixed in *Timer
  lifecycle*.
- **No test culture exists in this repo** — **Ruled: the manual-only boundary was NOT acceptable
  for the timing refactor**, which changes behaviour every existing user depends on. Fyne ships
  `test.NewApp()` and `testify` is already an indirect dependency, so headless coverage is cheap.
  Step 21b added. The boundary *is* accepted for the frame-rendering path itself, which genuinely
  needs a GL context and real hardware — that is what Step 22b covers instead.
- **ffmpeg becomes a runtime dependency for a feature, not for the app** — **Ruled:
  silent-skip-with-log is the correct runtime failure mode.** An on-screen error would contradict
  the Analyst constraint that a bad video "must never crash the slideshow or block advancing".
  But the *documentation* gap was real: existing installs never re-run `install.sh`, so Step 24
  must tell existing users to `sudo apt install ffmpeg`.

### Still open (accepted, not resolved)

- **Window resize mid-playback.** Frames are sized from the window dimensions captured when
  playback started. On resize, `canvas.ImageFillContain` rescales them, so the picture stays
  correct but soft. Restarting ffmpeg on resize remains judged not worth the complexity for a
  kiosk whose window never changes size. Phase 3 agrees with Phase 2 here — **no change**.
- **Verified against ffmpeg 8.1.2 only.** Every command line, JSON sample, frame count and
  timing figure in this document was executed on ffmpeg/ffprobe **8.1.2** (obtained temporarily
  via `nix shell nixpkgs#ffmpeg`; not installed on the machine). Raspberry Pi OS ships whatever
  ffmpeg its Debian base carries — currently 5.x or 7.x — so two things should be re-checked on
  target during Step 22b: that `fps='min(30,source_fps)'` is accepted (the `source_fps` variable
  is not present in very old builds), and that `stream=duration` is populated for MP4. Both have
  documented fallbacks: a bare `fps=30` still works if the expression is rejected, and
  `format.duration` remains the documented fallback for duration.
- **Playback was verified as a byte stream, not on a GL surface.** The frame contract, pacing and
  process lifecycle are measured; what is *not* verified is Fyne's texture-upload cost, which is
  precisely what Step 22b exists to measure on real hardware.

## Architect Checklist

- [x] Approach fits existing project patterns (mirrors `cec.go`'s CLI-wrapper + graceful-no-op pattern; reuses the generation counter, `fyne.Do` marshalling, and size/mtime reuse cache)
- [x] API contract defined before any code (internal Go API table above; no HTTP surface exists)
- [x] Schema changes identified (explicitly none — no database in this project; in-memory model changes enumerated)
- [x] Auth and ownership checks included in plan (not applicable — single-user local kiosk with no network surface, no accounts, no request context)
- [x] No step requires modifying multiple unrelated files (Step 24 touches README and INSTALL, which are the same documentation change; every other step is single-file — **corrected in Phase 3**: the old Step 9 touched both `slideshow.go` and `main.go`, and is now split into Step 7b (`slideshow.go`, moved ahead of Step 8) and Step 9 (`main.go`))
- [x] Parallelisation opportunities and file locks declared
- [x] Notification/event needs considered for mutations affecting live state (playback completion and cancellation are the only events; both are routed through the existing generation counter and the new `stopPlayback` teardown)
