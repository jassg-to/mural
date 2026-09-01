# Critic Review: USB Stick Hotplug

> Phase 3 — Plan stress-test. Approved before coding begins.
> This review is intended to go through two rounds. Round 1 = self-critique,
> verified against the code at HEAD rather than against the spec's description
> of it. Round 2 = fresh-eyes adversarial pass by an independent agent.

**Status: both rounds complete and applied. Spec is cleared for `sdd-coder`.**

---

## Round 2 (fresh-eyes) — findings

An independent agent with a fresh context window, given the spec and the code but
none of Round 1's reasoning, found **4 blockers, 8 warnings, and 5 notes** that
the self-critique missed. It found **no false positives in Round 1** and
independently cross-compiled the poll and `Statfs` code to confirm the
portability claims. All blockers and warnings are applied; note dispositions are
below.

This is the empirical case for the two-round design in miniature. Round 1 was
thorough and code-verified, and still shipped four blockers — three of which
(F1, F3, F4) are *artefacts of Round 1's own fixes*: a contradiction introduced
by applying W5 to one file and not the other, an enumeration predicate that
B5/B6 newly depended on but nobody defined, and a test seam that W8's fake could
not actually reach. A self-critique cannot catch the damage its own edits do,
because it reads the spec it just wrote as though it were the spec it intended.

### Round 2 Blockers (all applied)

| ID | Finding | Where fixed |
|----|---------|-------------|
| F1 | **Round 1's own W5 fix left the spec self-contradictory on a nonexistent media root.** W5 was applied to tasks.md Step 6 ("logged as a warning but the watcher still runs") but not to Architect.md, which still said "the watcher stays inert" — with Step 28 reading as a third variant. A coder starting from Architect.md would implement precisely the bug W5 removed. | Architect.md "Media root is a command-line flag" — rewritten with the reasoning and an explicit note that three places must agree |
| F2 | **The ext4 mount-option claim was factually wrong, and it silently disables the feature for whole classes of stick.** Architect.md asserted an ext4 stick "ignores those options and may be unreadable", mapping it to the Analyst's "unreadable → ingest failure, logged". But `uid=`/`gid=`/`umask=` are FAT/exFAT/NTFS driver options; ext4 and friends **reject unknown options and fail the mount outright**. The volume never enters `mountinfo`, so the real disposition is *nothing at all* — no mount, no event, no log, no diagnosis. Every ext4- and NTFS-formatted stick is silently inert. | Architect.md "Mounting: a udev rule" (correction with the mechanism); tasks.md Step 29 (two rules keyed on `ENV{ID_FS_TYPE}`), Step 30 (FAT32/exFAT guidance in the docs) |
| F3 | **Steps 12b and 13.3 depended on an enumeration of `content/`'s images that nothing defined.** Both said "every active top-level image" without a predicate. The naive reading — every non-directory entry — sweeps `content/config.toml` into `previous/`, after which Step 12c's `os.Link` fails with `ENOENT` *having already moved the images*, turning the happy path into a `mutated` failure on every accepted ingest. Round 1 created this dependency (B5 and B6 both lean on it) and never noticed the definition was missing. | tasks.md Step 9 (one shared `imageFilesIn(dir)` used for volume, `content/`, and `previous/`; predicate pinned to `isImageExt` + `IsRegular`, matching `scanSlides`), Steps 12b and 13.3 (both now name it); Architect.md contract table |
| F4 | **Step 27's tests could not be written.** `mediaCh` is read only inside `Run`'s `select`, and `newHeadlessSlideshow`'s doc comment states `Run` cannot be called from a test — so the queue assertions (one-at-a-time, dedupe) had no way to drive the queue. W8's `ingestFn` seam addressed the wrong half of the problem. | tasks.md Step 25(a) (extract `handleMediaMount(mp string)`), Step 27 (drive it directly, never via `Run`); Architect.md contract table |

### Round 2 Warnings (all applied)

| ID | Finding | Where fixed |
|----|---------|-------------|
| F5 | **The re-plan fix missed the sleep that matters.** `Start` has two sleeps: per-event (schedule.go:237) and until-midnight (schedule.go:245). Step 23 said "break out of the event loop" — but line 245 is *outside* it, and during scheduled off-hours (the headline scenario) `future` is empty and the goroutine is parked exactly there. The adopted schedule would have taken effect at midnight, not on insertion, defeating the step's entire purpose. | tasks.md Step 23 (both sleeps become selects; re-plan `continue`s the outer loop) |
| F6 | Step 6 specified `ctx` cancellation for the poll but not for the channel send; an unguarded send blocks forever if `Run` is not reading, and the goroutine never reaches its `ctx` check. `input.go:172-176` already shows the correct shape. | tasks.md Step 6 (guarded send) |
| F7 | **A rollback for a failed commit was available and unconsidered.** On a mid-6b failure the displaced images sit in `previous/` on the same filesystem and rename straight back. Round 1's B6 settled for the `mutated` repair path instead — which, combined with Step 20's adopt-the-empty-set, can leave the sign **entirely black** if the failure lands after all active images moved out but before any staged one landed. | tasks.md Step 12b (attempt rollback first; `mutated` only if the rollback itself fails) |
| F8 | **The Analyst's "must not blank mid-ingest" is violated in the commit→rescan window.** The commit renames image files on a background goroutine while the run loop's `advanceTimer` is still armed against `s.slides`, whose paths now point into `previous/`. An `advanceMsg` in that window decodes a missing file and presents a solid black frame — the exact partial result the Analyst forbids. | tasks.md Step 25(c) (`ingestCommitMsg{}` barrier stops the timer for the commit; cleared on every result path), Step 13 + Step 25 (`onCommit` callback parameter); Architect.md contract table |
| F9 | Step 24's no-double-fire assertion needed a clock seam `Schedule` does not have, making it a sleep-based flaky test. | tasks.md Step 23 (extract pure `futureEvents(events, now)`), Step 24 (assert against it; explicit instruction not to write the wall-clock form) |
| F10 | Two steps broke tasks.md's own "one coherent change to one file" rule and a declared file lock: old Step 1b (schedule.go + main.go) and Step 19 (slideshow.go + five call sites in slideshow_test.go). | First half **dissolved** — Step 1b was dropped on Liz's ruling. Second half: split into Step 19 / Step 19b, with an explicit note that they must land in one commit since the tree does not compile between them |
| F11 | **Step 29's udev guards were two-thirds non-functional.** `ENV{ID_FS_TYPE}!="swap"` is a no-op (the existing `ID_FS_USAGE=="filesystem"` match already excludes swap), and "skip devices already in `/proc/self/mounts`" is not expressible in a udev rule without a helper script. Only `ATTRS{removable}=="1"` works — and it excludes USB SSDs, which is exactly the USB-boot case the guard was for. **Corrected during Step 34 hardware validation: `ATTRS{removable}=="1"` does not work either — it cannot ever match, on any hardware.** udev requires `SUBSYSTEMS=="usb"` and an `ATTRS{}` condition to be satisfied by the same ancestor device; the numeric `removable` flag lives only on the block-layer disk device, while the `usb`-subsystem ancestor's own `removable` attribute is an unrelated string field (`"fixed"`/`"removable"`/`"unknown"`) that real mass-storage bridge chips report as `"fixed"` regardless of being genuinely removable. All three of Round 2's candidate guards are therefore non-functional, not two of three — Round 2 itself missed this because it was a desk review, not a hardware one. The guard is removed entirely; see Architect.md's hardware-validation correction and tasks.md Step 29 for the fix. | tasks.md Step 29 (guard removed entirely, not replaced); Architect.md "Hardware-validation correction" and Risks |
| F12 | `warnDegenerateWindows` was in Architect.md's Files-to-Modify row for `main.go` but absent from the Package Contracts table and from Step 28's change list. | **Dissolved** — Liz dropped Step 1b, so the function no longer exists. All references removed from both files; the Risks section retains a record of what was declined and why |

### Round 2 Notes

| ID | Note | Disposition |
|----|------|-------------|
| F13 | Poll mask must be `POLLPRI` only. `mounts_poll` returns `EPOLLIN\|EPOLLRDNORM` unconditionally, so the natural `POLLIN\|POLLPRI` reading yields a 100%-CPU busy loop on a passively-cooled Pi. | **Applied**, tasks.md Step 6 — promoted to its own emphasised paragraph, since `POLLIN` is exactly what one reaches for when polling a file. |
| F14 | Step 22's unbuffered-`cmdCh` subtest leaks a goroutine permanently on regression, because `postCommand` selects on `s.ctx.Done()` and the helper supplies `context.Background()`. | **Applied**, Step 22 — cancellable ctx with `t.Cleanup`. |
| F15 | `docs/INSTALL.md` deploys arm64 only; armv7 is a release artifact, so Step 32c overstated the deployment risk. | **Applied as a framing correction**, Step 32c — the step stays (it protects the CI pipeline on tag push) but no longer implies a sign could be affected. |
| F16 | `install.sh`'s kiosk launcher passes no positional argument, so `contentDir` is the relative `"content"` — and this feature adds `Statfs`, `os.Link`, and long `os.Rename` sequences on it from a background goroutine, days after start. | **Applied**, Step 28 — `filepath.Abs` at startup, log the resolved path. One line, removes the whole class. |
| F17 | Step 25's queue cap of 8 silently drops mount points beyond it. | **Applied**, Step 25(d) — warn on cap-forced drop. Dropping a *duplicate* stays silent (idempotent by the Analyst's definition); dropping past the cap is a real lost event. |

### Round 2 — did it override Round 1 anywhere?

Two refinements, no reversals. **F7 refines B6:** B6's `mutated` repair path is kept
as the fallback, but a rollback is now attempted first, so `mutated` becomes the
rare case rather than the expected one. **F3 completes B5/B6:** both leaned on an
enumeration predicate neither defined. Round 2 confirmed every Round 1 finding it
examined and contradicted none.

---

## Round 1 (self-critique) — findings

Method note: every finding below was checked against the source at HEAD
(`slideshow.go`, `schedule.go`, `main.go`, `input.go`, `install.sh`,
`slideshow_test.go`), not inferred from the spec text. Two findings (B1, B2)
were confirmed by reading the exact line the spec cites; one (the overnight
window scope decision) was confirmed by compiling and running the real
scheduler functions.

**Totals: 7 blockers, 14 warnings, 9 notes.** All blockers and all warnings
applied. Note dispositions recorded below.

### Round 1 Blockers (all applied)

| ID | Finding | Where fixed |
|----|---------|-------------|
| B1 | **Guaranteed run-loop deadlock on every accepted ingest.** Step 26 had the `ingestResultMsg` handler call `Slideshow.ApplyConfig`, which Step 21 defines as posting into `cmdCh`. But `cmdCh` is unbuffered (`slideshow.go:222`) and the run loop is its only reader (`slideshow.go:480`), and the handler already runs *on the run-loop goroutine* inside `handleCommand`. The run loop would send to itself with nothing selecting → the sign freezes permanently. Aggravating factor: `newHeadlessSlideshow` builds `cmdCh` with capacity 8 (`slideshow_test.go:364`), so **no test could ever catch this**. | tasks.md Step 21 (split into direct `applySlideshowConfig` + posting `ApplyConfig`), Step 26 (handler calls the direct form), Step 22 (unbuffered-channel regression test); Architect.md "Live configuration application" |
| B2 | **Data race on `thumbWidth` between `ApplyConfig` and an in-flight rescan.** `scanSlides` reads `s.thumbWidth` (`slideshow.go:120`) from the rescan's *background* goroutine. That is safe today only because nothing ever writes the field after construction — live config application makes it writable from the run loop. The spec never noticed, despite `slideshow.go:78-82` already documenting exactly this discipline for the `existing` slice. | tasks.md Step 19 (`scanSlides` takes `thumbWidth` as a parameter; `startRescan` captures it into the closure), Step 32b (`go test -race`); Architect.md "Splitting `Reload`'s fused rescan-and-wake" |
| B3 | **Steps 19 and 26 contradict each other.** Step 19 defines `startRescan(wake bool)`; Step 26 requires an ingest rescan to "pass `nil` as `scanSlides`' `existing`". `startReload` snapshots `s.slides` internally (`slideshow.go:282-293`), so the Step 19 signature cannot express what Step 26 demands. The coder would have had to invent a signature mid-flight. | tasks.md Step 19 → `startRescan(wake, freshThumbs bool)`; all call sites updated in Steps 20/26; Architect.md contract table |
| B4 | **The `MediaWatcher` poll loop cannot be cancelled and dies on `EINTR`.** Step 6 said "block in `unix.Poll`" and "exit on `ctx.Done()`, following `input.go`'s cancellation shape" — but a `-1` poll cannot be interrupted by context, and closing the fd from another goroutine (`input.go`'s pattern, which works for `read(2)` on a device) does not reliably wake a thread blocked in `poll(2)`. Separately, the Go runtime's async preemption delivers `SIGURG` routinely, so an unretried `EINTR` kills the watcher within minutes — and the symptom is indistinguishable from "no stick was inserted". | tasks.md Step 6 (finite 1 s timeout + `ctx.Err()` check; explicit `EINTR` retry), Step 34(j) (long-run hardware check); Architect.md "Mount detection" |
| B5 | **A config-only stick destroys the operator's image recovery copy.** Step 3 of the ingest deleted `previous/` wholesale before the copy. Sequence: image stick accepted → `previous/` holds the sign's former library; a settings-only stick, displacing *no images*, is then accepted → the library is gone. The retention bound sanctions discarding a displaced set when a newer set displaces it; a config-only ingest displaces nothing. Directly contradicts the Analyst's "nothing is destroyed" and "an accepted volume with no images leaves the rotation exactly as it was". | tasks.md Step 13 (reclaim scoped to payload — images reclaimed only when the volume has images), Step 12 (no blanket delete), Step 14 (retention subtest); Architect.md "Phase 3 revision — the reclaim is scoped to the payload" |
| B6 | **No handling for a failure *during* commit.** Step 26 said "on error, do nothing at all". But once 6b's first rename succeeds, `s.slides` names files that have moved into `previous/`, so "do nothing" leaves the sign showing a black frame per moved file until the next scheduled turn-on. The Architect analysed the *power-cut* case and missed the ordinary `EIO`/`ENOSPC` one. | tasks.md Step 13 (`mutated` field on `ingestResult`), Step 12 (every post-first-rename error sets it), Step 26 (`mutated` → `startRescan(false, true)`: repair the rotation, still no wake), Step 27 (test); Architect.md commit sequence + contract table |
| B7 | **`Schedule.ApplyConfig`'s re-plan signal blocks the run-loop goroutine.** Step 23 said "buffered, capacity 1, so repeated signals coalesce and never block the caller" — but a capacity-1 buffer gives neither property: a plain send on a channel already holding an unconsumed signal blocks, and this is called from the ingest handler on the run loop. Same freeze as B1 by a different route. Also: `replan` was never specified as initialised, and a nil channel makes the `select` arm block forever, silently disabling re-plan. | tasks.md Step 23 (non-blocking `select`/`default` send; initialise in `NewSchedule`); Architect.md "the schedule goroutine that would sleep through it" |

### Round 1 Warnings (all applied)

| ID | Finding | Where fixed |
|----|---------|-------------|
| W1 | Construction-order cycle: `main.go` builds `ss` (line 89) before `sched` (line 91, which consumes `ss.Reload`), so `sched.ApplyConfig` cannot be a `NewSlideshow` argument. Both Architect.md and Step 28 said "hand `sched.ApplyConfig` to the slideshow" without noticing it is impossible as written. | tasks.md Step 28 (`onScheduleConfig` field assigned post-construction, per the `ss.startPaused` precedent at main.go:92) |
| W2 | `classifyVolume` collapsed three stat outcomes into two. Any error → "absent" would swallow `EACCES`/`EIO` into the *silent* disposition, contradicting the Analyst's "volume mounts but is unreadable → ingest failure, logged". | tasks.md Step 7 (explicit `fs.ErrNotExist` vs. other-error branches), Step 8 (unreadable-root subtest) |
| W3 | Step 20's empty-rescan branch would naturally be written as "adopt empty set and return", silently dropping the wake — but Architect.md requires a config-only ingest into an empty sign to still power on the display, as the operator's only confirmation. | tasks.md Step 20 (must fall through to the shared `if m.wake`), Step 22 (test) |
| W4 | Root filtering was specified as "strictly under `root`" with no mechanism; a raw `strings.HasPrefix` matches `/media/mural-backup`. Also untestable while inlined in the watcher. | tasks.md Step 4 (pure `underRoot`, separator-aware), Step 5 (`TestUnderRoot` incl. the sibling-prefix case); Architect.md contract table |
| W5 | "A media root that does not exist → inert watcher" permanently disables the feature if `/media/mural` is created after Mural starts. Since detection reads `mountinfo`, not the directory, there is nothing to guard against. | tasks.md Step 6 (warn but keep watching) |
| W6 | `volumeImages` contract was `[]os.FileInfo-equivalent` — undefined. Worse, it did not say *which* stat, so `os.Stat` (follows symlinks) would let a symlink named `big.jpg` → `/dev/zero` through the regular-file check that exists precisely to stop that. | tasks.md Step 9 (`volumeFile` struct; `d.Info()` lstat semantics), Step 14 (symlink + FIFO subtest with a timeout guard); Architect.md contract table |
| W7 | Commit step 6c (rename config out, rename config in) leaves a window where `content/config.toml` does not exist — and `Schedule.reload()` reads exactly that path on a daily timer from another goroutine. | tasks.md Step 12c (link-then-atomic-rename-over), Step 14 (config-never-absent subtest); Architect.md commit sequence |
| W8 | Step 27 asked for tests "using a fake ingest hook", but Step 25 hardcoded the `ingestVolume` call, leaving no seam. | tasks.md Step 25 (`ingestFn` field defaulted to `ingestVolume`) |
| W9 | Step 18 required calling `Run` from a test, but `newHeadlessSlideshow`'s own doc comment (slideshow_test.go:348-353) states `Run` cannot be called from a test and hands back an uncancellable `context.Background()`. | tasks.md Step 18 (separate helper with `context.WithCancel`; explicit instruction not to widen the existing one) |
| W10 | No step ran the project's mandated checks. CLAUDE.md requires `go vet` and `staticcheck`; nothing in 34 steps invoked them, or `gofmt`, or the tests. | tasks.md new Step 32b (incl. `go test -race`, which is what proves B1/B2 fixed) |
| W11 | "Recovered over the Samba share" is not a procedure. Copying `previous/` back on top of `content/` yields a rotation that is the *union* of both sets, and nothing rescans until Home is pressed or the next turn-on fires. | tasks.md Step 30 (numbered recovery procedure incl. the rescan trigger) |
| W12 | The udev rule matches the **live root filesystem** on a USB-boot Pi (`SUBSYSTEM=="block"`, `SUBSYSTEMS=="usb"`, `ID_FS_USAGE=="filesystem"` all hold for `sda2`), which udev would then mount a second time under `/media/mural/`. Safe on the SD-card install `docs/INSTALL.md` specifies, but that assumption was never written down. | tasks.md Step 29 (guard or document the assumption; either is acceptable, silence is not) |
| W13 | The pending mount-point queue was unbounded and permitted duplicates; a re-enumerating hub or flaky port would queue identical ingests indefinitely. | tasks.md Step 25 (dedupe + cap 8), Step 27 (test) |
| W14 | `Bavail * Bsize` does not compile on `GOARCH=arm`, where `Bsize` is `int32` — and armv7 is a published release target, so the failure appears in CI on tag push, after the release is being cut. | tasks.md Step 10 (both casts explicit), new Step 32c (cross-compile all three targets); Architect.md contract table |

### Round 1 Notes

| ID | Note | Disposition |
|----|------|-------------|
| N1 | Step 3 demanded locating the `-` separator, but the mount point is at fixed field index 4 — the separator only matters for fields *after* it, which the parser does not return. | **Applied** (Step 3 rewritten to say so, so the coder does not over-engineer); the octal-escape trap, which is real, is kept and sharpened. |
| N2 | Case-sensitive `config.toml` makes a Windows-prepared `Config.toml` land in the *silent* disposition. | **Applied as a diagnostic, not a behaviour change** — Step 7 logs when a differently-cased config exists on the ignored path. Relaxing the match was rejected: acceptance would then depend on which of two files won a directory scan. |
| N3 | `filepath.Base` on stick filenames as defence in depth (`ReadDir` names cannot contain a separator, so there is no live traversal bug). | **Applied**, Step 9, explicitly labelled as defence in depth so it is not mistaken for a fix. |
| N4 | An out-of-hours wake persists until the *next* off event, which may be the following day — a stick inserted at 22:00 lights the sign until 20:00 tomorrow. | **Applied as documentation**, Step 23 / Architect.md. Not changed: it is identical to the existing nav-key wake, which the Analyst explicitly chose to inherit. |
| N5 | Decompression-bomb OOM from untrusted images is now reachable from the USB port. | **Documented as an accepted residual risk**, Architect.md Risks. Not fixed: the Analyst puts image-format handling out of scope, and the same path is already reachable via Samba and the SD card. What changed is reachability, which is what the security note in `docs/INSTALL.md` covers. |
| N6 | `Schedule.reload()`'s local re-read is not validated by `hasAnyOnWindow` — an asymmetry with stick configs. | **Applied as an intentional, documented asymmetry** plus the Step 1b warning. Rejecting an already-installed config would take a running sign dark on upgrade. |
| N7 | No rollback strategy documented. | **Applied**, Architect.md Risks — code revert, on-disk artefacts, host udev rule, and the one thing revert does *not* undo. |
| N8 | `handleVTEvent` (slideshow.go:441-445) is a fourth `show()` caller on the empty-rotation path, unnamed in Architect.md's list of three. | **Applied**, Steps 16 and 18. Covered by Step 15's guard inside `show`, but now asserted rather than assumed. |
| N9 | Schedule tests go into `slideshow_test.go` while `media`/`ingest` get their own test files — inconsistent. | **Not applied.** Cosmetic, and `slideshow_test.go` is already the project's catch-all (it holds DRM, input, and compositor tests). Splitting it is a tidy-up unrelated to this feature; forcing it here would add a file lock for no benefit. |

### Round 1 — the four items Phase 2 explicitly deferred to the Critic

| Deferred item | Round 1 verdict |
|---|---|
| Retention-slot reclaim ordering (delete `previous/` before vs. after the copy) | **Ordering upheld, scope overturned.** Before-the-copy survives on the free-space-honesty argument. Wholesale deletion does not — see B5. |
| Non-atomic multi-file commit window | **Trade upheld, with one free improvement.** The `renameat2(RENAME_EXCHANGE)` alternative is correctly rejected: it requires the rotation to live one level below the content directory, breaking the positional argument, the Samba path, and every deployment's muscle memory. The image half stays a rename sequence. But the *config* half needed no such trade — link-then-rename-over is atomic at no cost (W7), and it was worth taking because a live reader (`Schedule.reload`) exists on that exact path. |
| Rejecting overnight windows (`"22:00-02:00"`) | **Rejection upheld, and the underlying bug is worse than described.** Measured against the real `windowsToEvents`/`IsOn`: an overnight window produces a sign that turns on at 22:00 and never turns off, while `IsOn` reports false throughout. Fixing that is an explicit non-goal; a non-fatal warning on *local* configs (Step 1b) is added so the pre-existing failure stops being silent. **This scope decision needs the user's sign-off** — see Architect.md Risks. |
| Empty-rescan behaviour change reaching beyond USB (Samba) | **Upheld as specified.** Adopting the empty set is the honest reading of the Delta's "no images stops being an error condition anywhere". The visible consequence — clearing the Samba share now yields a black sign at the next turn-on rather than a stale set — is a behaviour change on a non-USB path and is called out in Architect.md. Worth the user's attention but not a blocker: the Analyst's waiting state is unimplementable if an empty rescan is special-cased. |

### Round 1 Residual risks (cannot be verified offline)

- `/proc/self/mountinfo` + `POLLPRI` firing at all on the target kernel. Unit
  tests structurally cannot cover it. Step 6's 5-second timeout re-read now
  bounds the blast radius to "5 seconds late" instead of "silently inert", and
  Step 34 must confirm which path is actually doing the work.
- `systemd-mount` from a udev `RUN{program}` on Raspberry Pi OS Lite. The
  pattern is standard and `--no-block --collect` is the documented shape, but it
  is unverified on this image. Step 29b warns at install time if
  `systemd-mount` is absent; Step 34 verifies the mount before blaming the
  player.
- exFAT kernel support on the target image.
- Samba and the player as concurrent writers to `content/`. No locking is
  proposed and none is realistic (Samba cannot participate); the interaction is
  acknowledged rather than resolved.

---

## Phase 3 gate decisions taken with the user

| Decision | Ruling |
|---|---|
| Add a non-fatal warning when the **local** `config.toml` contains a degenerate or overnight window (drafted as Step 1b) | **Declined.** Keep the feature USB-only. Sticks still reject such configs — that part is unaffected and, given the measured stuck-on-forever behaviour, clearly correct — but nothing outside the USB path changes. The local-config warning becomes a separate, later fix with its own scope. Step 1b removed; `warnDegenerateWindows` removed from the contracts and from `main.go`'s change list; the measurement and the reasoning are retained in Architect.md's Risks so the follow-up starts from evidence rather than rediscovering it. |

## Final gate

Both rounds complete, 12 blockers and 22 warnings applied across them. Step count
34 → 38 (dropped 1b; added 19b, 29b, 32b, 32c). `tasks.md` and `Architect.md`
were re-checked against each other after the last edit: signatures, the
`previous/` reclaim scoping, the media-root disposition, and the image-enumeration
predicate now agree in both files.

**Cleared for `sdd-coder`.** The residual risks below are all on-hardware
confirmations that Step 34 covers by design, not open questions.
