# Architect: USB Stick Hotplug

> Phase 2 — Design decisions. Approved before coding begins.
> Implementation checklist is in tasks.md.

## Approach

Mural stays a single flat `package main` Go program with no new dependencies —
`golang.org/x/sys` is already a direct require, and everything this feature
needs from the kernel (mount-table change notification, free-space query) comes
from it. Two new files are added (`media.go`, `ingest.go`), mirroring the way
`input.go` was added as a self-contained subsystem feeding one channel into the
run loop. `slideshow.go`, `schedule.go`, and `main.go` are modified;
`drm.go`, `drm_ioctl.go`, `compositor.go`, `vt.go`, `headless.go`, `renderer.go`,
and `cec.go` are untouched.

**The feature decomposes into three independent concerns, deliberately kept in
separate files so that only the last one touches player state:**

1. *Noticing a volume* (`media.go`) — unprivileged, hardware-adjacent, emits
   mount-point strings on a channel. Exactly the `InputWatcher` shape.
2. *Deciding and performing the ingest* (`ingest.go`) — pure filesystem work
   against two directories. Knows nothing about `Slideshow`, `Renderer`, or
   `Schedule`; returns a result value. This is where the whole all-or-nothing
   transaction lives, and it is the part with real unit-test coverage.
3. *Applying the result* (`slideshow.go`, `schedule.go`) — runs on the single
   run-loop goroutine, and is small precisely because (2) did the work.

This split is what makes the feature testable at all. The Analyst's hard
requirements — all-or-nothing, no partial files, retention bound, free-space
gate, three dispositions — are all properties of (2), which is ordinary
`t.TempDir()`-testable Go with no hardware, no mounts, and no root.

### Mount detection: `/proc/self/mountinfo`, not inotify on the media directory

The obvious design — inotify `IN_CREATE` on `/media/mural` — is wrong, and
wrong in a way that would produce a flaky, occasionally-empty ingest. A mount
point directory is *created first and mounted second*; `IN_CREATE` fires in the
gap, so Mural would scan an empty directory, find no `config.toml`, and
silently classify a perfectly good stick as *ignored*. There is no inotify
event for "a filesystem was mounted here."

Instead, `MediaWatcher` opens `/proc/self/mountinfo` and blocks in
`unix.Poll` waiting for `POLLPRI`/`POLLERR` — the kernel's documented
notification mechanism for mount-table changes (`proc(5)`). It fires *after* a
mount is established, which is exactly the edge we need. On each wake the
watcher re-reads the file from offset 0, parses it, filters to mount points
under the configured media root, and diffs against the previous set; newly
appeared mount points are sent on `Events()`. Disappeared ones are dropped from
the tracked set, which is what makes "re-inserting the same stick repeats the
ingest" fall out for free rather than needing stick identity.

`unix.Poll` also wakes for mounts and unmounts that have nothing to do with us.
That is harmless — the diff produces an empty result and the loop goes back to
sleep — and it is much cheaper than polling on a timer.

**Three properties of the poll loop are load-bearing** (added in Phase 3; the
first two are outright bugs in the original design and the third is the only
in-code defence against this section's headline risk):

- *Finite timeout, not `-1`.* A `-1` poll blocks forever and `ctx.Done()` cannot
  interrupt it. `input.go`'s cancellation shape — a second goroutine closing the
  fd — works for `read(2)` on a device node but does **not** reliably wake a
  thread blocked in `poll(2)`, so it must not be copied here. The loop polls with
  a 1-second timeout and checks `ctx.Err()` on each expiry.
- *`EINTR` is retried, not fatal.* The Go runtime delivers `SIGURG` for async
  preemption, which interrupts blocking syscalls as a matter of routine. Treating
  `EINTR` as an error would kill the watcher within minutes of normal running,
  and the symptom — sticks are never noticed — is indistinguishable from the
  feature not being installed.
- *Progress does not depend on `POLLPRI` firing.* Every fifth timeout (≈5 s) the
  watcher re-reads and diffs anyway. This is deliberate insurance: the Risks
  section below names a silently non-firing `POLLPRI` as the single highest risk
  in this design, unit tests structurally cannot cover it, and without the
  fallback the failure mode is a feature that is completely inert on the board
  while passing every test. With it, the worst case degrades to 5-second
  detection latency. The cost is parsing ~40 lines of `/proc` every five seconds.
  Step 34 must confirm the `POLLPRI` path works *and* is what is being used —
  a feature that only ever works via the fallback is a finding, not a pass.

The mount points present at startup are treated as newly appeared (the tracked
set starts empty), which satisfies "a volume already mounted at startup is
evaluated at startup" with no separate startup code path.

`parseMountinfo` is a pure function over an `io.Reader`, tested against real
captured `mountinfo` text. Two details of the format it must get right: fields
are escaped with octal sequences (`\040` for space — a stick labelled
`MY STICK` mounts at `/media/mural/MY\040STICK`), and the fixed-position fields
are separated from the trailing fields by a `-` marker whose position varies
with the number of optional fields. Getting either wrong silently loses sticks
with ordinary labels.

### Media root is a command-line flag, not a config key

`-media-dir` (default `/media/mural`), following the `-headless` precedent.
This is not just consistency: putting it in `config.toml` would be actively
unsafe, because `config.toml` is the thing a stick *replaces*. A stick carrying
a config that changed or blanked `media_dir` would disable hotplug on the sign
permanently, and the only recovery would be the shell access this whole feature
exists to avoid needing. The one setting that governs whether sticks are read at
all must not itself be settable from a stick.

An empty `-media-dir` disables the watcher outright: no goroutine, no file
opened, a channel that never fires.

**A media root that does not exist is logged once as a warning and the watcher
runs anyway.** This is deliberate and is the *only* correct reading — an earlier
draft of this section said the watcher "stays inert" in that case, which was
wrong twice over. First, there is nothing to be inert about: detection reads
`/proc/self/mountinfo`, not the directory, so a nonexistent root simply means no
line ever matches `underRoot` and the diff is permanently empty. Guarding on the
directory's existence buys nothing. Second, it creates a real failure mode —
`install.sh` creates `/media/mural`, so a Mural started *before* an installer run
(or before the directory is restored) would latch into permanent inertness and
need a restart to notice, which is precisely the "feature appears installed and
does nothing" trap this design keeps trying to avoid. The pre-existing-deployment
case is handled by there being no automounter and therefore no mounts, not by the
player refusing to look.

Three places must agree on this and did not before Phase 3: this section,
tasks.md Step 6, and tasks.md Step 28 ("a missing root or empty flag is a logged
non-failure"). None of them may say "inert" for the missing-root case.

### Windows is a non-goal that needs no code

The Analyst lists graceful no-op on non-Linux. No build tags or stub files are
added, because the project already does not build on Windows and has not since
the DRM/evdev migration — `input.go`, `drm.go`, `vt.go`, and `drm_ioctl.go` are
all unconditionally Linux. Adding `_linux.go` tags for `media.go` alone would
imply a portability that does not exist anywhere else in the tree. The non-goal
is satisfied by the project's existing platform reality, and this is stated
rather than implemented.

### Ingest is a stage-then-commit transaction inside the content directory

```
content/
  config.toml            active config      (path unchanged — main.go, schedule.go)
  *.jpg *.png            active rotation    (unchanged)
  previous/              most recent displaced set (images + config.toml)
  .ingest-staging/       transient scratch; exists only mid-ingest
```

Both new directories live *inside* the content directory, and that is a
correctness requirement, not tidiness: `os.Rename` is only atomic within a
single filesystem, and the entire commit depends on rename. A staging directory
in `/tmp` or `$HOME` could land on a different mount and silently degrade every
commit into a copy.

`scanSlides` already skips directory entries (slideshow.go:96-98), so both
directories are out of the rotation with no change to the scanner — this is the
"content directory gains internal structure" Delta item, satisfied by existing
code rather than new filtering.

`previous/` is deliberately *not* dot-prefixed. It is the operator's recovery
path, and Samba's default `hide dot files = yes` would make a hidden directory
invisible over the very share an operator would use to recover from a
mis-plugged stick. `.ingest-staging/` *is* dot-prefixed, for the opposite
reason: the operator must never interact with it, and it must not be mistaken
for content if a crash leaves it behind.

The sequence, in order, with the mutation window marked:

1. **Classify.** `config.toml` at the volume's top level? Absent → *ignored*,
   return immediately, log at info level, nothing else runs. Present but
   `LoadConfig` fails → *rejected*, logged as an error. Present but
   `hasAnyOnWindow` is false → *rejected*, logged as an error. Otherwise
   *accepted*.
2. **Enumerate** the volume's top-level supported images: non-directory,
   **regular files only**, recognised extension via the existing `isImageExt`.
   The regular-file check is not paranoia — a FIFO or device node named
   `x.jpg` on an attacker-controlled or corrupt filesystem would otherwise hang
   the copy forever inside `io.Copy`, on a background goroutine, with the sign
   still running and no indication why the ingest never completes.
3. **Reclaim the retention slot, scoped to the payload being displaced.** Delete
   the *image files* inside `previous/`, and **only when the volume actually
   supplied images**. `previous/config.toml` is left alone here; the commit
   replaces it separately in 6c. See the ordering argument below.
4. **Free-space gate.** `unix.Statfs` on the content directory; require
   `sum(image sizes) + config size + margin <= Bavail*Bsize`. If not, decline
   before touching anything else and log. The margin (32 MiB) exists so a
   successful ingest can never leave the SD card at literally zero free bytes,
   which would break the next ingest, the log, and Samba simultaneously.
5. **Stage.** Copy each image and the `config.toml` into `.ingest-staging/`,
   `fsync` each file, then `fsync` the staging directory. Verify each copy's
   byte count against the source's stat size. Any error — a yanked stick's
   `EIO`, a short read, `ctx` cancellation during shutdown — deletes the
   staging directory and returns failure with nothing else on the device
   touched.
6. **Commit** *(the only window in which the active content directory
   changes)*:
   a. `os.MkdirAll previous/` — never a blanket delete; step 3 already reclaimed
      what was warranted;
   b. **only if the volume supplied at least one image**, `os.Rename` every
      active image from `content/` into `previous/`, then rename every staged
      image from `.ingest-staging/` into `content/`;
   c. always **`os.Link` `content/config.toml` to `previous/config.toml`** (after
      unlinking any existing one) and then `os.Rename` the staged config over
      `content/config.toml`;
   d. remove the now-empty `.ingest-staging/`.

   Step 6c is a link followed by an atomic rename-over rather than the obvious
   rename-out-then-rename-in, because the latter leaves a window in which
   `content/config.toml` **does not exist at all** — and `Schedule`'s daily
   `reload()` reads exactly that path, from a different goroutine, on a timer
   this ingest knows nothing about. `os.Rename` over an existing destination is
   atomic on Linux, so with the link form no reader can ever observe a missing or
   half-written config. The cost is one extra inode reference for the duration of
   the commit.

   Any error from 6b onward must be reported to the caller with a `mutated` flag
   set (see `ingestResult`). Once the first rename in 6b succeeds the Analyst's
   "leave the running player exactly as it was" guarantee no longer holds:
   `s.slides` in memory now names files that have moved into `previous/`, and the
   sign would show a black frame for each of them until something rescanned. The
   run loop's response to `mutated` is a rescan **without** a wake — the rotation
   is repaired, and a failed ingest still does not light up the display.

Step 6b's condition is the entire config-only case. The Analyst is emphatic
that an accepted volume with no images must leave the rotation *exactly* as it
was and must never empty it — so the image half of the commit does not run at
all, rather than running over an empty staged set. `previous/` in that case
holds only a displaced `config.toml`, which is correct: that is what was
displaced.

**Why the retention slot is reclaimed before the copy rather than after.**
Reclaiming first is what makes the free-space gate honest — space occupied by a
set we are contractually allowed to discard is space the ingest can use, and
measuring availability before reclaiming it would decline ingests that actually
fit. The cost is that a *failed* ingest has already consumed the retention slot.
That is acceptable and, on inspection, is what the rules require: "retaining at
most the single most recent displaced set" explicitly sanctions discarding the
older one, and the set the "nothing is destroyed" rule protects is the one
currently in rotation — which is untouched until step 6 and fully intact after
any failure. The alternative (keep `previous/` until success) requires headroom
for two full sets plus the incoming one on the smallest disk in the system.

**Phase 3 revision — the reclaim is scoped to the payload, not wholesale.** The
original design deleted `previous/` entirely at step 3, which is wrong for the
config-only case and wrong in the one direction the Analyst cares most about.
Consider: an image stick is accepted, so `previous/` holds the operator's only
recovery copy of the sign's former library. A second stick carrying nothing but a
`config.toml` — a settings change, displacing no images whatsoever — is then
accepted, and the wholesale delete destroys that library. The operator changed a
schedule and silently lost their content backup. Nothing in the Analyst sanctions
that: the retention bound permits discarding a displaced set when a *newer set
displaces it*, and a config-only ingest displaces no images at all. So the
reclaim now deletes `previous/`'s images only when the incoming volume actually
has images, and `previous/config.toml` is replaced independently in step 6c. The
free-space argument above is unaffected — in the config-only case the payload is
a few kilobytes and there is nothing meaningful to reclaim anyway.

**Why the commit is not itself atomic, and why that is accepted.** Step 6 is a
sequence of renames, not one operation. A power cut between 6b's two halves
leaves a content directory holding some old images and some new. The window is
milliseconds of pure metadata work, and the resulting state is still a *valid*
image set — the sign comes back showing a coherent mixture, not a crash. A truly
atomic alternative exists — build the new set as a sibling directory and swap
with `renameat2(RENAME_EXCHANGE)` — but it requires the rotation to live one
level below the content directory, which breaks the positional content-directory
argument, the Samba share path, and every existing deployment's muscle memory.
Considered and rejected; recorded here so the Critic evaluates the trade rather
than rediscovering it.

The stick is opened read-only and never written, deleted from, renamed, or
`chtimes`'d — the mount is `ro` as well, so this is enforced twice.

### Ingest never runs on the run-loop goroutine, and never runs twice at once

The run loop gains `case mp := <-s.mediaCh:`, which does not perform the ingest.
It either starts a background goroutine (if idle) or appends the mount point to
a queue (if an ingest is in flight); the goroutine posts an `ingestResultMsg`
back into `cmdCh` and the run loop drains one queued mount point on receipt.
This is the same `startReload`/`reloadResultMsg` shape already in the file, and
it delivers the Analyst's two requirements directly: the display keeps playing
throughout (the run loop is never blocked by a multi-megabyte copy) and volumes
are serialised one at a time (a single in-flight flag, not a mutex).

### Splitting `Reload`'s fused rescan-and-wake

`Reload`'s handler currently ends unconditionally in `resume()`
(slideshow.go:409-420). It becomes `startRescan(wake, freshThumbs bool)` and
`reloadRequestMsg`/`reloadResultMsg` each gain a `wake bool` (the existing type
names are kept — renaming them would churn `Reload`'s call sites for no gain).
`Reload()` stays `postCommand(reloadRequestMsg{wake: true})`, whose handler calls
`startRescan(m.wake, false)`, so the scheduled turn-on path is behaviourally
unchanged. Ingest calls `startRescan(true, true)` on success and nothing at all
on ignore/reject — see below for why `freshThumbs` has to be a parameter.

Note that an ignored or rejected volume never reaches a rescan in this design, so
the split is not strictly load-bearing for correctness today. It is implemented
anyway, per the Delta, because it converts "a rejected stick doesn't wake the
display" from an accident of call-site ordering into a structural property — and
because the next person to add a rescan caller should not have to rediscover
that rescanning silently powers on the display.

An ingest-triggered rescan passes `nil` as `scanSlides`' `existing` argument
rather than the current slide set, forcing every thumbnail to be re-decoded.
This is required, not merely tidy: `thumb_width` may have changed with the
adopted config, and `scanSlides`' reuse key is `(path, size, mtime)` — it has no
notion of thumbnail width and would happily keep thumbnails at the old size
forever.

That requirement is why the split is `startRescan(wake, freshThumbs bool)` and
not `startRescan(wake bool)`: `startReload` snapshots `s.slides` internally
(slideshow.go:282-293), so without a second parameter no caller can express
"scan from nothing", and the two halves of this design contradict each other.

**`scanSlides` also gains an explicit `thumbWidth uint` parameter.** It currently
reads `s.thumbWidth` (slideshow.go:120) from the rescan's *background*
goroutine, which is safe today only because nothing ever writes that field after
construction. Live config application makes it writable from the run loop, so
leaving the read in place would be a real data race between `ApplyConfig` and an
in-flight rescan. `startRescan` captures the width into the goroutine's closure
alongside the `existing` snapshot, exactly as it already does for the slide set,
and for the same reason — the comment at slideshow.go:78-82 already articulates
this discipline for `existing`; the width simply joins it.

### The waiting state is where the sharp edges are

Making an empty content directory legal is three lines in `Run` and a genuine
crash hazard everywhere else. `advanceToNext` (slideshow.go:313-315) and
`handleNavKey` (slideshow.go:391-399) both compute `% n` where `n :=
len(s.slides)`, and `show` (slideshow.go:329) indexes `s.slides[index]`
directly. With an empty rotation these are two integer divisions by zero and one
out-of-range panic — on the run-loop goroutine, so each one kills the sign
outright. A single `s.waiting()` predicate guards all of them, `show` presents
black and arms no timer when waiting, and `s.current` is clamped whenever the
slide set shrinks. This gets its own test step for each of the three sites,
because "empty is now legal" is the kind of change whose failure mode is a panic
in production three weeks later on the one sign whose operator deleted
everything over Samba.

Related and deliberate: `reloadResultMsg`'s current early return on an empty
rescan (slideshow.go:414-417, "keep showing what we had") is replaced by
adopting the empty set and entering the waiting state. Keeping the early return
would mean the waiting state is reachable at startup but not at runtime, which
contradicts the Delta's "no images stops being an error condition anywhere in
the player and becomes an ordinary state." The visible consequence — an operator
who empties the share over Samba now gets a black sign at the next scheduled
turn-on instead of the stale previous set — is the honest reading, and is
flagged for the Critic.

### Live configuration application, and the schedule goroutine that would sleep through it

Adopting a stick's config has two halves. The slideshow half looks easy and
contains one trap. `ApplyConfig(SlideshowConfig)` posts into `cmdCh` and the run
loop assigns `interval` and `thumbWidth`, with the same zero-value defaults
`main.go` currently applies at startup (30s, 80px) hoisted into one shared helper
so startup and ingest cannot drift apart.

**The trap: the ingest result handler must not call `ApplyConfig`.** `cmdCh` is
unbuffered (slideshow.go:222) and the run loop is its only reader
(slideshow.go:480); `postCommand` blocks until that reader is ready. The ingest
result handler runs *inside* `handleCommand`, on the run-loop goroutine itself,
so posting from there is the run loop sending to itself with no one selecting —
a permanent freeze of the sign, on every accepted ingest, until `ctx` is
cancelled. So the assignment lives in an unexported `applySlideshowConfig` that
mutates directly and is only ever called on the run loop, and the exported
`ApplyConfig` is the any-goroutine wrapper that posts a message which calls it.
The ingest handler calls the former.

This is worth stating at design level rather than leaving to implementation
care, because **the test suite cannot catch it**: `newHeadlessSlideshow` builds
`cmdCh` with capacity 8 (slideshow_test.go:364), so a test would absorb the send
and pass while the sign froze in production. The same hazard applies to
`Schedule.ApplyConfig`'s re-plan signal below, which is called from the same
handler on the same goroutine.

The schedule half has a trap. `Schedule.reload()` already replaces `s.cfg` under
the mutex, so `ApplyConfig(ScheduleConfig)` is trivial — but the goroutine that
*acts* on the schedule is sitting in `time.Sleep(time.Until(e.at))`
(schedule.go:237) against the **old** config's event list, and will not consult
the new one until it wakes. After an afternoon ingest that could be hours; after
an ingest on a day the old config had no remaining events, it is next midnight.
"Apply the new schedule to the running player without a restart" is not
satisfied by a config that takes effect tomorrow.

So `Start`'s event loop gains a re-plan channel: every `time.Sleep` becomes a
`select` over `time.After` and `<-s.replan`, and `ApplyConfig` (and `reload`,
which inherits the fix for free) signals it. On re-plan the loop breaks out and
recomputes the day's events from scratch. This is a real change to working code
in service of this feature, and it is called out here because it is the piece
most likely to be skipped as "the mutex already handles it" — the mutex handles
visibility, not wakeup.

**The signal must be a non-blocking send** — `select { case s.replan <- struct{}{}: default: }`.
A capacity-1 buffer alone gives neither coalescing nor non-blocking behaviour: a
plain send on a channel that already holds an unconsumed signal blocks, and
`ApplyConfig` is called from the ingest result handler on the run-loop goroutine,
so blocking there freezes the sign exactly as the `cmdCh` trap above would.
`replan` is initialised in `NewSchedule`; a nil channel would make the `select`
arm in `Start` block forever and the re-plan would silently never happen — the
same class of silent-inertness failure as a non-firing `POLLPRI`.

One consequence of "recompute from scratch, dropping past events" is worth
stating so it is not mistaken for a bug later: a config adopted at 14:00 whose
on-window is 08:00–20:00 yields only an *off* event for the rest of today, never
a catch-up *on*. The sign is on regardless, because the ingest woke it directly —
wake and schedule are independent paths, and this is precisely the nav-key wake
semantics the Analyst chose to inherit. No synthetic catch-up event is added.

The daily `reload_time` goroutine has the same shape but does not need the fix:
its own next-wake time is recomputed from `s.cfg` at the top of every iteration,
so a changed `reload_time` takes effect on the next cycle, and being at most one
day late on a config-reload timer is not a behaviour anyone can observe.

### What "wake" means, precisely

Accepting a volume calls `resume()` — un-pause, redisplay, CEC `TurnOn` —
which is definitionally the same wake a nav key performs, satisfying "inherits
the existing manual-wake semantics exactly, no new timeout." The display returns
to schedule control at the next scheduled off event because that event is
already queued by `Start`'s loop; nothing new is needed to make the wake expire.

A config-only ingest arriving at a sign with an empty rotation also wakes: the
screen powers on and shows black. The Analyst's edge-case table says this state
"must not be treated as failed," and powering on is the only confirmation the
operator gets that the stick was read at all. Recorded as an interpretation
rather than a literal quote from the spec.

### "Defines no on-window" is `Off > On`, not "the config is non-empty"

`hasAnyOnWindow(ScheduleConfig) bool` scans all seven `DayConfig`s and all six
occurrence lists (`all`, `first`, `second`, `third`, `fourth`, `last`), and
counts a `Window` as an on-window only when `w.Off > w.On`. The strictness
matters in both directions:

- A hand-typed `"18:00-18:00"` parses fine and is a zero-length window.
  `windowsToEvents` emits an on and an off at the same instant and `IsOn`
  returns false for all time — a permanently dark sign, which is precisely the
  failure mode this rejection rule exists to catch.
- An overnight `"22:00-02:00"` also fails `Off > On` and is therefore rejected.
  This is correct *given current behaviour*: `windowsToEvents` anchors both ends
  to the same calendar day, so overnight windows do not work in Mural today
  either — rejecting them is more honest than adopting a config the scheduler
  will misinterpret.

  **Phase 3 measured what "do not work" actually means, and it is worse than
  this section assumed.** Running the real `windowsToEvents` and `IsOn` against
  `Window{On: 22:00, Off: 02:00}`:

  ```
  events: at=22:00 turnOn=true, at=02:00 turnOn=false   (emitted in this order)
  IsOn(01:00)=false  IsOn(12:00)=false  IsOn(21:00)=false  IsOn(23:00)=false
  ```

  Two distinct defects, not one. First, `windowsToEvents` sorts *windows* by `On`
  but never sorts the emitted *events* by time, so the off event is returned
  before it should be; `IsOn`'s scan then reads the trailing `off@02:00` as
  applying after `on@22:00` and reports **false at 23:00 — inside the window the
  operator asked for.** Second, `Start`'s event loop drops events already in the
  past, which discards `off@02:00` every single day, so the only event that ever
  fires is `on@22:00`. The net observable behaviour of an overnight window today
  is **a sign that turns on at 22:00 and never turns off again** — while
  simultaneously reporting itself as off, so `main.go`'s `startPaused` boots it
  paused. This is not a partially-working feature; it is a stuck-on display.

  That makes the rejection decision straightforward rather than a close call:
  adopting such a config from a stick would leave a sign permanently lit, which
  is the same class of unrecoverable-without-shell-access failure the "no
  on-window" rule exists to prevent, and squarely against the Analyst's "decline
  rather than damage". Rejection is retained. See the Risks section for the
  scope decision on the underlying bug.

A config whose only windows live under an occurrence key (`second = [...]`, with
no `all`) counts as usable. It genuinely is on sometimes.

### Mounting: a udev rule driving `systemd-mount`

The installer gains a udev rule that mounts USB block devices carrying a
filesystem under `/media/mural/<kernel name>`, read-only, owned by the kiosk
user:

```
ACTION=="add", SUBSYSTEM=="block", SUBSYSTEMS=="usb", ENV{ID_FS_USAGE}=="filesystem", \
  RUN{program}+="/usr/bin/systemd-mount --no-block --collect -o ro,nosuid,nodev,noexec,uid=<kiosk>,gid=<kiosk>,umask=0077 $devnode /media/mural/%k"
```

Chosen over the alternatives on availability and blast radius. `usbmount` is
unmaintained and absent from Debian since buster, so it is not an option on the
Raspberry Pi OS Lite image `docs/INSTALL.md` specifies. `udisks2` + `udiskie`
assumes a desktop session and a polkit agent that a bare autologin console
kiosk does not have. `systemd-mount` is already present (systemd is what runs
the getty this kiosk logs in on), needs no extra package, no session, and no
daemon — and `--collect` makes systemd tear the unit down on unplug, which is
what keeps `/proc/self/mountinfo` honest and lets the watcher's disappearance
tracking work.

`ro` in the mount options is the second enforcement of "the stick is read-only,"
below Mural's own never-write discipline. `uid=/gid=/umask=` is what makes a
FAT32/exFAT stick readable by the unprivileged kiosk user.

**Phase 3 correction — the original claim about ext4 here was wrong, and wrong in
the worst direction.** This section previously said an ext4-formatted stick
"ignores those options and may be unreadable, which lands in the Analyst's
'volume mounts but is unreadable → ingest failure, logged' case." It does not
ignore them. `uid=`, `gid=`, and `umask=` are options of the FAT/exFAT/NTFS
drivers, not generic VFS options; ext4 (and btrfs, and xfs) **reject unknown
mount options and fail the mount outright**. The volume therefore never appears
in `/proc/self/mountinfo` at all, so the disposition is not "ingest failure,
logged" — it is *nothing*. No mount, no event, no log line, no ingest, no
diagnosis. An operator handing the sign an ext4 or NTFS stick gets silence, which
is the single failure mode this design's top risk is about.

The fix is to make the filesystem-specific options conditional on the filesystem
type: two udev rules keyed on `ENV{ID_FS_TYPE}`, one carrying
`ro,nosuid,nodev,noexec,uid=,gid=,umask=` for `vfat|exfat|ntfs`, and one carrying
only `ro,nosuid,nodev,noexec` for everything else. The generic four are true VFS
options and are safe on every filesystem. An ext4 stick then mounts successfully
and *may* still be unreadable to the kiosk user if its files are root-owned —
which is the case the Analyst actually describes, reached honestly, via a real
mount and a real logged permission error. Documenting the limitation instead of
fixing it is acceptable only if the docs say plainly that non-FAT sticks are
unsupported; silently mounting nothing is not.

This is a prerequisite, not a nicety: without it a stick on a stock deployment
appears as a block device and is never mounted, so the feature is inert. Every
pre-existing installation needs `install.sh` re-run, and that goes in the docs.

## Package Contracts

| Contract | Shape | Defined in | Consumed by |
|---|---|---|---|
| `MediaWatcher` | `NewMediaWatcher(ctx context.Context, root string) (*MediaWatcher, error)`; `Events() <-chan string` (mount points) | `media.go` | `main.go` (construct), `slideshow.go` (run loop) |
| `parseMountinfo` | `(r io.Reader) ([]string, error)` — mount points, octal-unescaped | `media.go` | `media.go`, tests |
| `newMountPoints` | `(prev, cur map[string]bool) []string` — pure set diff | `media.go` | `media.go`, tests |
| `underRoot` | `(mountPoint, root string) bool` — pure, separator-aware strict-descendant test | `media.go` | `media.go`, tests |
| `volumeDisposition` | `int` enum: `volumeIgnored`, `volumeRejected`, `volumeAccepted` | `ingest.go` | `slideshow.go` (logging), tests |
| `classifyVolume` | `(mountPoint string) (volumeDisposition, *Config, error)` — `fs.ErrNotExist` on `config.toml` is *ignored* with a **nil** error; every other stat error is *rejected* | `ingest.go` | `ingest.go`, tests |
| `hasAnyOnWindow` | `(cfg ScheduleConfig) bool` — pure | `schedule.go` | `ingest.go`, tests |
| `volumeFile` | `{name string; size int64}` — one enumerated top-level image | `ingest.go` | `ingest.go`, tests |
| `imageFilesIn` | `(dir string) ([]volumeFile, int64, error)` — the **single** definition of "the images in a directory", used for the volume, `content/`, and `previous/` alike. Predicate is exactly `isImageExt` + `Mode().IsRegular()`, matching `scanSlides` (slideshow.go:95-102); `d.Info()` lstat semantics so symlinks are skipped | `ingest.go` | `ingest.go`, tests |
| `volumeImages` | `(mountPoint string) ([]volumeFile, int64, error)` — a call to `imageFilesIn`; kept as a name for readability at the call site | `ingest.go` | `ingest.go`, tests |
| `availableBytes` | `(dir string) (uint64, error)` — `uint64(st.Bavail) * uint64(st.Bsize)`; both casts required for the armv7 target | `ingest.go` | `slideshow.go`, tests |
| `ingestVolume` | `(ctx context.Context, mountPoint, contentDir string, avail func(string) (uint64, error), onCommit func()) ingestResult` — `onCommit` fires once, immediately before the commit, so the run loop can raise its blank-prevention barrier | `ingest.go` | `slideshow.go` (background goroutine), tests |
| `Slideshow.handleMediaMount` | `(mp string)` — the run loop's `mediaCh` case, extracted so queue behaviour is testable without calling `Run` | `slideshow.go` | `slideshow.go`, tests |
| `futureEvents` | `(events []event, now time.Time) []event` — pure; the past-event filter extracted from `Start` so the no-double-fire property is testable without a wall clock | `schedule.go` | `schedule.go`, tests |
| `ingestResult` | `{disposition volumeDisposition; cfg *Config; imagesApplied bool; mutated bool; err error}` — `mutated` is true only for a commit that failed after its first successful rename | `ingest.go` | `slideshow.go` |
| `Slideshow.applySlideshowConfig` | `(cfg SlideshowConfig)` — assigns directly; **run-loop goroutine only** | `slideshow.go` | `slideshow.go` (ingest result handler) |
| `Slideshow.ApplyConfig` | `(cfg SlideshowConfig)` — posts into `cmdCh`; safe from any goroutine **except the run loop itself** | `slideshow.go` | external callers |
| `Slideshow.startRescan` | `(wake, freshThumbs bool)` — `freshThumbs` passes `nil` as `scanSlides`' `existing` | `slideshow.go` | `slideshow.go` |
| `Slideshow.scanSlides` | `(existing []Slide, thumbWidth uint) ([]Slide, error)` — width is now a parameter, not a field read from a background goroutine | `slideshow.go` | `slideshow.go`, tests |
| `Slideshow.ingestFn` | field, same signature as `ingestVolume`, defaulted to it — the seam Step 27's tests substitute | `slideshow.go` | `slideshow.go`, tests |
| `Slideshow.onScheduleConfig` | `func(ScheduleConfig)` field, assigned in `main.go` after `NewSchedule`; nil-checked before use | `slideshow.go` | `main.go` |
| `Schedule.ApplyConfig` | `(cfg ScheduleConfig)` — swaps config under the mutex and signals re-plan with a **non-blocking** send | `schedule.go` | `slideshow.go` (ingest result handler) |
| `slideshowDefaults` | `(cfg SlideshowConfig) (time.Duration, uint)` — the 30s/80px defaults, shared by startup and ingest | `slideshow.go` | `main.go`, `slideshow.go` |

No HTTP/RPC contract applies — single embedded process.

## Data Model Changes

No schema migration exists to run; `config.toml`'s shape is **unchanged** — a
stick's config is the same `Config` struct `main.go` already loads, which is the
point (the sample file `install.sh` writes is directly usable as a stick's
config).

New on-disk structure inside the content directory:

| Path | Purpose | Lifetime |
|---|---|---|
| `content/previous/` | Most recent displaced image set + displaced `config.toml` | Until the next successful ingest displaces it |
| `content/.ingest-staging/` | In-progress copy from the volume | Deleted on both success and failure |

Both are skipped by `scanSlides`' existing directory filter — no scanner change.

New in-memory types only, all in `package main`:
- `volumeDisposition int`, `ingestResult` (`ingest.go`)
- `ingestResultMsg`, `applyConfigMsg`, and a `wake bool` field added to
  `reloadRequestMsg`/`reloadResultMsg` (`slideshow.go`)
- `replan chan struct{}` field on `Schedule` (`schedule.go`)

`Slide`, `Renderer`, `NavKey`, and every DRM type are unchanged.

## Files to Create

| File | Purpose |
|---|---|
| `media.go` | `MediaWatcher`: `/proc/self/mountinfo` `POLLPRI` watch, pure `parseMountinfo` and `newMountPoints`, mount-point channel |
| `ingest.go` | `volumeDisposition`, `classifyVolume`, volume image enumeration, free-space gate, staged copy, commit, retention bound |
| `media_test.go` | Tests for `parseMountinfo` (octal escapes, the `-` separator, non-media mounts) and `newMountPoints` |
| `ingest_test.go` | Tests for `classifyVolume`'s three dispositions and `ingestVolume`'s transaction: happy path, config-only, no-config, unparseable config, no-on-window, insufficient space, mid-copy failure, retention bound, stick left unmodified |

*Deviation note:* `drm-renderer` put every test in `slideshow_test.go`. This
feature adds enough filesystem-transaction coverage to justify two files beside
the code they test — the project already has more than one test file
(`bench_test.go`), so this is a small extension of existing practice rather than
a new convention.

## Files to Modify

| File | Change |
|---|---|
| `slideshow.go` | Waiting-state guards (`waiting()`, `show`/`advanceToNext`/`handleNavKey`/`resume`/`handleVTEvent` empty-rotation safety, `current` clamping); `Run` no longer errors on an empty directory; split `Reload` into `startRescan(wake, freshThumbs bool)`; `scanSlides` takes `thumbWidth` as a parameter; empty rescan adopts the empty set instead of returning early (and still honours `wake`); `applySlideshowConfig` + `ApplyConfig` + `applyConfigMsg`; `slideshowDefaults` helper; `ingestFn` and `onScheduleConfig` fields; `mediaCh` in the run loop with the deduplicated one-at-a-time queue, background ingest call, and `ingestResultMsg` handling including the `mutated` repair path |
| `schedule.go` | `hasAnyOnWindow(ScheduleConfig) bool`; `Schedule.ApplyConfig(ScheduleConfig)`; `replan` channel (initialised in `NewSchedule`, signalled with a non-blocking send) so `Start`'s event goroutine re-plans immediately instead of sleeping through a config change |
| `main.go` | `-media-dir` flag (default `/media/mural`); construct `MediaWatcher` (non-fatal if the root is missing or the flag is empty) and pass its channel into `Run`; use `slideshowDefaults`; assign `ss.onScheduleConfig = sched.ApplyConfig` after `NewSchedule` returns and before `sched.Start()` (the construction cycle means it cannot be a constructor argument — see tasks.md Step 28) |
| `slideshow_test.go` | Tests for the waiting state (empty startup, nav keys, auto-advance, empty rescan), the rescan/wake split, `ApplyConfig`, and ingest-result handling including the queue |
| `install.sh` | Create `/media/mural`; install the udev automount rule with the kiosk user's uid/gid substituted; `udevadm control --reload`; mention the USB workflow and the `config.toml` requirement in the closing "next steps" output |
| `docs/INSTALL.md` | New section on updating content by USB stick (the stick must carry a top-level `config.toml`; copy the sample from `content/`); the physical-port-access security note the Delta requires; a note that pre-existing installs must re-run `install.sh` |
| `README.md` | Document the USB stick workflow, the `config.toml` requirement, `previous/` as the recovery path, and the `-media-dir` flag |
| `CLAUDE.md` | Project Structure entries for `media.go`/`ingest.go`; Architecture Notes for the ingest transaction, the content-directory structure, the waiting state, and live config application |

`compositor.go`, `renderer.go`, `drm.go`, `drm_ioctl.go`, `vt.go`,
`headless.go`, `cec.go`, `input.go`, `go.mod`, `go.sum`, and
`.github/workflows/build.yaml` are untouched — **no new dependency is
introduced**.

## Dependencies Between Steps

- `hasAnyOnWindow` (schedule.go) has no dependencies and is needed by
  `classifyVolume` — it comes first.
- `parseMountinfo`, `newMountPoints`, and `underRoot` are pure and independent of
  everything; `MediaWatcher` depends on all three.
- `classifyVolume` depends on `hasAnyOnWindow` and on the existing `LoadConfig`.
- `ingestVolume` depends on `classifyVolume` and on the existing `isImageExt`.
- The waiting-state guards in `slideshow.go` must land **before** the ingest
  wiring: an ingest into a previously empty content directory is the first thing
  that exercises the empty rotation at runtime, and wiring ingest first means
  debugging a divide-by-zero panic instead of an ingest.
- The rescan/wake split must land before the ingest wiring, which calls it.
- `Schedule.ApplyConfig` + the re-plan channel must land before the ingest
  result handler that calls it.
- `Slideshow.applySlideshowConfig` (and its `ApplyConfig` wrapper) must land
  before the ingest result handler.
- `scanSlides`' new `thumbWidth` parameter lands with the rescan split (Step 19)
  and updates two existing call sites plus five in `slideshow_test.go` — it is
  the one change in this plan that touches already-passing tests, so it must not
  be deferred into a later step where the breakage looks unrelated.
- `main.go`'s wiring depends on `MediaWatcher`, on `Run`'s new signature, and on
  `Schedule.ApplyConfig` existing (it assigns `ss.onScheduleConfig` to it) —
  done last among code files.
- Whole-tree validation (Steps 32b/32c) comes after every code and doc step and
  before the hardware gate.
- Docs and `install.sh` have no compile-time dependency but should follow the
  code so they describe what was built.
- On-hardware validation is last, and depends on `install.sh`'s udev rule
  actually being installed on the board.

## Parallelisation Opportunities

- `media.go` (+ `media_test.go`) and `ingest.go` (+ `ingest_test.go`) touch
  disjoint files and have no dependency on each other beyond
  `hasAnyOnWindow` — safe to build concurrently once that one function exists.
- `install.sh`, `docs/INSTALL.md`, `README.md`, and `CLAUDE.md` are four
  disjoint files — safe as a parallel batch.
- **File lock: `slideshow.go`.** Five separate concerns modify it (waiting-state
  guards, rescan split, `ApplyConfig`, ingest wiring, `slideshowDefaults`).
  These are sequential steps on one file and **cannot** be parallelised with
  each other.
- **File lock: `schedule.go`.** `hasAnyOnWindow`, `ApplyConfig`, and the re-plan
  channel all touch it; sequential only.
- **File lock: `slideshow_test.go`.** One step per concern, but all on one file
  — sequential.

## Risks & Open Questions

- **`/proc/self/mountinfo` + `POLLPRI` is the least-exercised primitive here.**
  It is documented and widely used (systemd, util-linux), but it is not
  something this codebase has done before, and a mistake is silent: the watcher
  simply never fires and the feature appears to work in every unit test while
  doing nothing on the board. The parse is unit-tested; the *poll* is not
  testable without mounting something, so it needs explicit on-hardware
  confirmation, not inference.
- ~~**Deleting `previous/` before the copy consumes the retention slot on a
  failed ingest.**~~ **Re-derived in Phase 3 and partly overturned.** The
  before-the-copy *ordering* survives (the free-space honesty argument holds).
  The *scope* did not: a wholesale delete let a config-only stick, which
  displaces nothing, destroy the operator's recovery copy of a previous image
  library. The reclaim is now scoped to the payload — images are reclaimed only
  when the incoming volume has images. Consuming the slot on a failed *image*
  ingest remains accepted, as originally argued.
- **The commit window is a sequence of renames, not an atomic operation.** A
  power cut mid-commit yields a coherent-but-mixed image set. The atomic
  alternative (`renameat2(RENAME_EXCHANGE)` on a subdirectory layout) was
  rejected for breaking the content-directory path contract; the Critic should
  confirm that trade.
- **Rejecting overnight windows (`"22:00-02:00"`): resolved in Phase 3.** The
  measurement above shows the pre-existing bug is not "overnight windows are
  ignored" but "overnight windows produce a permanently-on sign that reports
  itself as off". Given that, the three candidate dispositions resolve cleanly:

  1. *Required fix.* **No.** Correcting overnight windows means changing the
     day-anchoring model in `windowsToEvents`, `IsOn`, and `Start`'s event loop —
     the code path that decides whether the sign is ever on, on every
     deployment, for a case no user of this feature has asked for. That is its
     own Analyst-level change with its own risk, and folding it into a USB
     hotplug feature is exactly the scope creep the Analyst's non-goals guard
     against.
  2. *Explicit non-goal.* **Yes, for the fix itself.** Recorded here and in
     `CLAUDE.md` so the next person finds the measurement rather than
     rediscovering it.
  3. *Something else.* A non-fatal warning on the **local** `config.toml` was
     drafted (a `warnDegenerateWindows` helper called at startup and at each
     daily reload) on the argument that rejection protects only sticks, while the
     likelier victim is a hand-edited local config that is never validated.
     **Put to the user at the Phase 3 gate and declined.** The ruling: keep this
     feature USB-only. Sticks still reject degenerate and overnight windows —
     that part is unaffected and correct — but nothing outside the USB path
     changes, and the local-config warning becomes a separate, later fix with its
     own scope. The draft step was removed from `tasks.md` accordingly.

  So the final disposition is (1) **no** required fix, (2) **yes** explicit
  non-goal, (3) **no** collateral change. The measurement above is retained
  precisely because it is the thing worth not rediscovering: whoever picks up the
  overnight-window bug later starts from "this produces a stuck-on sign" rather
  than from "overnight windows seem to be ignored."
- **The empty-rescan behaviour change reaches beyond USB.** Making an empty
  rescan adopt the empty set means an operator who clears the Samba share now
  gets a black sign at the next scheduled turn-on, where today the sign keeps
  showing the previous set. Consistent with the Delta, but it is a behaviour
  change on a path that has nothing to do with USB sticks.
- **`Schedule.Start`'s re-plan channel is a change to working concurrency code.**
  The event goroutine currently sleeps through the whole day's event list; the
  rewrite must recompute the list on re-plan without double-firing an event that
  already fired, and without busy-looping if `ApplyConfig` is called repeatedly.
  Small, subtle, and on the path that controls whether the sign is ever on.
- **A missing `config.toml` in the *content* directory is still a fatal startup
  error** (`main.go:26-30`), unchanged by this design. The Delta only makes an
  empty content *directory* non-fatal, and `install.sh` always writes a
  `config.toml`, so every real deployment has one — but "boot a bare sign and
  provision it entirely from a stick" is only fully true because of that
  installer behaviour, not because the player tolerates a missing config.
  Deliberately left out of scope; raised here in case the user wants it in.
- **`config.toml` is matched case-sensitively** at the volume's top level. A
  stick prepared on Windows could plausibly carry `Config.toml`, which would be
  classified *ignored* — the silent disposition, the hardest one to diagnose.
  Chosen for simplicity and because the documented workflow is "copy the sample
  file," which preserves the name. *Phase 3 kept the strict match but removed the
  diagnosis problem:* on the ignored path only, `classifyVolume` also looks for a
  case-insensitive match and logs at info level when one exists. The behaviour is
  unchanged, the mystery is not. Relaxing the match itself was rejected — it
  would make acceptance depend on which of two similarly-named files won a
  directory scan.

- **Untrusted image decoding is now reachable from the USB port.** A
  decompression bomb — a small PNG declaring enormous dimensions — is decoded by
  `loadThumbnail`/`decodeAndFit` into an `image.NewRGBA` sized from the file's own
  header, which on a 1 GB Pi 3B+ is an OOM kill. This is pre-existing (the Samba
  share and the SD card reach the same code) and the Analyst explicitly makes
  image-format handling out of scope — "inherits whatever the supported image set
  is at implementation time and adds nothing to it" — so no decode limit is added
  here. What changes is the reachability: the trust boundary moves from "someone
  with the Samba password or the SD card" to "someone who can reach the port",
  which is precisely the boundary `docs/INSTALL.md` is being told to document.
  Recorded as an accepted residual risk, not a silent one.

- **Rollback.** The feature is additive and reverts cleanly in two independent
  halves. Code: revert the commit — `media.go` and `ingest.go` are new files, and
  every modification to `slideshow.go`/`schedule.go`/`main.go` is behaviour-preserving
  when the media channel is nil, so a reverted binary runs an unmodified content
  directory unchanged. On-disk: `content/previous/` and any stray
  `content/.ingest-staging/` are inert directories that `scanSlides` already
  skips; they can be deleted at leisure or left. Host: remove
  `/etc/udev/rules.d/99-mural-usb.rules` and `udevadm control --reload-rules`.
  The one thing a revert does **not** undo is an ingest that already replaced
  `content/`'s images — that is what `previous/` is for, and the recovery
  procedure is documented in `docs/INSTALL.md` (tasks.md Step 30).

- **`Schedule.reload()`'s local re-read remains unvalidated by `hasAnyOnWindow`,
  deliberately.** A stick is foreign input and is turned away; an already-installed
  `config.toml` is not, because refusing it would take a running sign dark on an
  upgrade. The asymmetry is intentional. A warning on that path was drafted and
  declined at the Phase 3 gate to keep this feature USB-only, so the local path
  is currently validated **not at all** — a known, accepted gap with an owner
  (the separate overnight-window fix), not an oversight.
- **exFAT sticks depend on kernel exFAT support** being present in the Raspberry
  Pi OS Lite image. Modern kernels have it; if a target image does not, an exFAT
  stick never mounts and the feature is silently inert for that operator. Worth
  confirming on the board rather than assuming.
- **Two writers now share the content directory** (Samba and the player), as the
  Delta's REMOVED section flags. A Samba copy landing mid-ingest can be picked
  up, missed, or displaced into `previous/` depending on timing. No locking is
  proposed — the two are unlikely to be used simultaneously and a lock the
  Samba side cannot participate in would be theatre — but the interaction is
  undefined and should be acknowledged rather than discovered.
- **Free-space accounting ignores filesystem overhead.** Summing file sizes
  underestimates actual consumption (block rounding, directory entries), which
  the 32 MiB margin is meant to absorb. A stick of ten thousand tiny files would
  defeat that assumption; nothing in the spec suggests that is a real workload.

## Architect Checklist

- [x] Approach fits existing project patterns — flat `package main`, subsystem-per-file feeding one channel into the run loop, single-goroutine ownership of mutable state, no new dependencies
- [x] Package contracts defined before any code
- [x] No schema changes — `config.toml`'s shape is unchanged; new on-disk directory structure documented instead
- [x] Unprivileged-process constraint carried into the design — mounting is entirely the installer's udev rule; the player only reads `/proc/self/mountinfo` and copies files as itself
- [x] No step requires modifying multiple unrelated files
- [x] Parallelisation opportunities and file locks declared (`slideshow.go`, `schedule.go`, `slideshow_test.go` are all sequential-only)
- [x] Live-state mutation concerns considered — ingest results, slideshow settings, and schedule settings all apply on the run-loop goroutine or under the existing mutex, and the schedule's sleeping event goroutine is explicitly re-planned rather than left to catch up
