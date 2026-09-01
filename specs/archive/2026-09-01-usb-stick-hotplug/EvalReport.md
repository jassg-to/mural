# Eval Report: USB Stick Hotplug

> Phase 5 — Post-implementation evaluation. Run after sdd-coder completes.
> Evaluator had no access to the coder's reasoning — fresh context only.
> Evaluated at commit `8412097`, working tree clean.

## Computational Sensors

| Check | Result |
|-------|--------|
| `gofmt -l .` | ✅ 0 files |
| `go vet ./...` | ✅ 0 findings |
| `go build .` | ✅ clean |
| `go test -race ./...` | ✅ all pass (1.9s) |
| `staticcheck ./...` | ⚠️ not installed on this machine — Step 32b calls it conditional ("if available"); not run, so nothing it would catch is ruled out |
| Cross-compile `GOARCH=amd64` | ✅ clean |
| Cross-compile `GOARCH=arm64` | ✅ clean |
| Cross-compile `GOARCH=arm GOARM=7` | ✅ clean (the `Bsize` `int32`/`int64` trap of Step 10 is genuinely closed) |
| Coverage — whole module | ⚠️ 43.5% of statements |

Per-file coverage (statements covered / total):

| File | Coverage | Note |
|---|---|---|
| `ingest.go` | 69.6% (135/194) | feature file |
| `media.go` | 36.7% (29/79) | feature file; the pure parts are ~100%, the watcher goroutine is 0% |
| `slideshow.go` | 74.7% (177/237) | modified |
| `schedule.go` | 47.1% (74/157) | modified |
| `compositor.go` | 89.5% | pre-existing |
| `drm_ioctl.go` | 100% | pre-existing |
| `cec.go` | 68.8% | pre-existing |
| `input.go` | 17.9% | pre-existing |
| `main.go` / `drm.go` / `vt.go` / `headless.go` | 0% | structurally untestable (hardware, process entrypoint) |

Zero-coverage functions inside the feature's own files: `availableBytes`,
`volumeDisposition.String`, `rollbackMovedOut` (`ingest.go`);
`NewMediaWatcher`, `Events`, `watch`, `readMountsUnder` (`media.go`).

## Semantic Evaluation

| Criterion | Verdict | Detail |
|-----------|---------|--------|
| CONTRACT | ✅ | Both new files (`media.go`, `ingest.go`) and both new test files exist. Every one of the 22 entries in Architect.md's Package Contracts table is present with the exact signature specified, including the awkward ones (`ingestVolume`'s five-parameter form with `avail` and `onCommit`, `startRescan(wake, freshThumbs bool)`, `scanSlides(existing, thumbWidth)`, `Slideshow.ingestFn`, `Slideshow.onScheduleConfig`, `Schedule.ApplyConfig`). Every file in the Files-to-Modify table shows the described change; the untouched list is genuinely untouched. |
| BEHAVIOUR | ✅ | All 18 Analyst "Behaviour" clauses are present in code. Spot-verified: three dispositions (`ingest.go:108-132`), abort-whole-ingest on bad config (`ingest.go:303-306`), rotation becomes exactly the volume's images (`ingest.go:452-489`), config-only leaves rotation untouched (`ingest.go:424-430`), live `[slideshow]`+`[schedule]` apply without restart (`slideshow.go:449-452`, `schedule.go:234-250`), wake-regardless-of-schedule (`slideshow.go:456` → `reloadResultMsg` → `resume`), rotation restarts at slide 0 (`slideshow.go:620`), empty content directory is a legal permanent state (`slideshow.go:503-505` + guards at `:491`, `:516`, `:587`, `:674-676`). |
| EDGE CASES | ⚠️ | 38 of the 39 rows in Analyst.md's edge-case table map to real code and, in most cases, to a named test. One narrow window is left open — see Finding 6. |
| TESTS | ⚠️ | Every test file the Architect asked for exists and passes under `-race`; the required test names from Steps 2/5/8/14/18/22/24/27 are all present. Three explicitly-specified error branches are nonetheless untested — see Findings 2, 3, 4. |
| PATTERNS | ⚠️ | Follows CLAUDE.md's Go conventions throughout: `gofmt` clean, errors wrapped with `%w` everywhere, no `_` discards, `context.Context` threaded through the ingest, single-goroutine ownership of `Slideshow` state preserved, no new dependencies (`go.mod`/`go.sum` unchanged). One function violates its own documented contract — see Finding 5. |
| COVERAGE | ⚠️ | The rubric's generic "≥80% backend" bar is not met on any reading: 43.5% module-wide, 69.6% for `ingest.go`, 36.7% for `media.go`. The project has never stated a coverage bar and roughly 300 statements are in structurally untestable hardware files, so the raw number is not itself damning — but the specific uncovered lines named above include a rollback path the plan singled out as important. |
| NO EXTRAS | ✅ | No scope creep found. Everything that looks like an addition is traceable to a spec line: `ingestCommitMsg`/`ingestCommitting` (Step 25c), `logCaseInsensitiveConfig` (Step 7), `pendingMountsCap` (Step 25d), `copyFileSynced` fallback in `commitConfig` (Step 12c's "fail closed"), `pollFallbackInterval` (Step 6), `filepath.Abs` on `contentDir` (Step 28), `volumeDisposition.String()` (Step 7). The overnight-window bug was correctly left unfixed per the Phase 3 ruling. |

## Findings

### ⚠️ 1 — Step 34's `POLLPRI`-vs-fallback confirmation is not recorded, and the code cannot distinguish the two

> **Addressed 2026-09-01 (resolved by user observation, not by instrumentation).**
> Liz ran multiple additional hotplug trials on `pi3b.local` and reports the
> slideshow was up on the new content in definitely less than 2 seconds every
> time — see `tasks.md`'s Step 34 addendum. Step 6's fallback only re-reads
> every 5 seconds, so consistent sub-2s response across several trials, with no
> trial drifting toward the 5s ceiling, is inconsistent with the fallback being
> the active path and is accepted as sufficient confirmation that `POLLPRI` is
> firing. **This is a timing inference from watching a clock, not the
> log-instrumented proof the step originally called for** — `watch` still has
> no wake-reason logging, so the finding's underlying observation (no line
> distinguishes the two paths) remains literally true. Treated here as closed
> for archiving purposes; the code-level gap (no wake-reason log) is not fixed
> and would need to be if this question ever needs to be re-litigated with
> certainty.

`media.go:146-192`, `tasks.md:162-166`, `status.json`

Step 34 requires: *"confirm that `MediaWatcher` fires at all, and confirm it via
the `POLLPRI` path rather than the timeout fallback… Log the wake reason (poll
event vs. timeout re-read) at debug level during this validation… a feature that
only ever works via the fallback is a finding, not a pass."*

`watch` has no wake-reason logging at any level, and nothing in `tasks.md`,
`status.json`, or `Architect.md` records the outcome of that comparison. The
recorded hardware confirmations (a–e, h, i, j) would all pass identically if
`POLLPRI` never fired and the 5-second re-read at `media.go:167-171` were doing
all the work — including the 7.5-hour soak, which proves only that the goroutine
survives, not which arm woke it. Architect.md names a silently non-firing
`POLLPRI` as *"the single highest risk in this design"*; the step designed to
close that risk is marked `[x]` with the risk still open. Step 34 is otherwise
substantively evidenced.

### ⚠️ 2 — `rollbackMovedOut` has zero test coverage

`ingest.go:494-502` (0.0% covered), reached from `ingest.go:464` and `ingest.go:480`

Step 12b calls this out specifically: *"a failure after every active image has
moved out but before any staged image has landed leaves `content/` with no images
at all, which Step 20 then faithfully adopts as the waiting state — a stick that
fails mid-commit would black the sign completely. Rolling back turns that into a
no-op."* The rollback is implemented correctly as far as reading goes, but no
test in `ingest_test.go` drives a rename failure inside `commitImages`, so
neither the rollback nor the `mutated=true` distinction it guards is exercised at
the `ingest.go` level. (`slideshow_test.go:958-1008` tests only what
`handleIngestResult` does *with* a `mutated` flag supplied by a fake.)
`commitImages` sits at 56.5%; `availableBytes` at 0%.

### ⚠️ 3 — The `ingestCommitMsg` blank-prevention barrier has no test

`slideshow.go:637-646` (handler), `slideshow.go:418-420` (`onCommit` posting site), `slideshow.go:633-635` (the `advanceMsg` no-op)

This barrier is the sole defence of the Analyst's *"the display must not blank,
stall, or show partial results mid-ingest"* (Analyst.md:98-100), added
deliberately by Step 25c. `grep` finds no occurrence of `ingestCommitMsg` or
`ingestCommitting` anywhere in `slideshow_test.go`. A regression that dropped the
`s.ingestCommitting` check from `advanceMsg`, or that stopped posting
`ingestCommitMsg`, would pass the whole suite.

Related and also untested: the `pendingMountsCap` drop branch at
`slideshow.go:402-405`, including the warning-level log Step 25d says must not be
silent.

### ⚠️ 4 — Step 14's "every subtest" volume-immutability assertion is applied to half the subtests

`ingest_test.go`

Step 14 requires *"an assertion in every subtest that the volume directory's
contents and mtimes are byte-for-byte unchanged."* `assertVolumeUnchanged` is
called in 5 of the 10 `TestIngestVolume` subtests. It is absent from:

- `ingest_test.go:336` "mid-copy failure leaves the active set intact" — defensible, the test itself `chmod`s the volume
- `ingest_test.go:362` "retention bound"
- `ingest_test.go:387` "config-only retention"
- `ingest_test.go:417` "symlinks and FIFOs are skipped"
- `ingest_test.go:440` "content/config.toml is never observably absent"

The last two are the ones where it would have had the most value: the FIFO
subtest is precisely where a wrong `io.Copy` could have written to the volume.

### ⚠️ 5 — `futureEvents` is documented and contracted as pure, but aliases and mutates its input

`schedule.go:326-334`

```go
func futureEvents(events []event, now time.Time) []event {
	future := events[:0]
	...
}
```

`events[:0]` reuses the caller's backing array, so the call overwrites the
argument slice in place. Architect.md's Package Contracts table lists it as
`— pure`, and its own doc comment (`schedule.go:321-325`) describes it as an
extracted pure filter. There is no live bug today: the only production caller
(`schedule.go:294`) passes a slice `windowsToEvents` freshly allocates on every
call, and `TestFutureEvents` (`slideshow_test.go:828-848`) calls it exactly once
so the aliasing is invisible to it. It is a latent trap for the next caller and a
divergence from the stated contract.

### ⚠️ 6 — The commit barrier stops `advanceMsg` but not the other two paths into `show`

`slideshow.go:637-646` vs. `slideshow.go:547-566` and `slideshow.go:656-660`

`ingestCommitting` makes `advanceMsg` a no-op during the commit window, which
closes the path Step 25c analysed. Two other routes to presenting a since-renamed
file remain open:

- a `startImageDecode` goroutine launched before the commit that has not yet
  opened its file — it would fail the open, post `img = nil`, and
  `compositeLetterboxed(nil, …)` presents a solid black frame;
- `handleVTEvent(vtEventAcquired)` (`slideshow.go:658`) calls `show(s.current, true)`
  unconditionally, with no `ingestCommitting` check, so a VT switch landing inside
  the commit window presents the thumbnail and then a black decode result.

Both windows are milliseconds-to-seconds wide and require unusual timing (a VT
switch, or a decode outlasting the whole staging phase), and neither is named in
Analyst.md's edge-case table. Recorded because Step 25c reasoned explicitly about
this exact failure mode and closed only one of its three entrances.

### ⚠️ 7 — `MediaWatcher`'s runtime half is entirely uncovered

`media.go:103-213` — `NewMediaWatcher`, `Events`, `watch`, `readMountsUnder` all at 0.0%

Expected for `watch` (it needs a real mount to exercise, which Architect.md
acknowledges). `readMountsUnder` (`media.go:198-213`) is the composition of two
already-tested pure functions plus a `Seek`, and is testable against a temp file
without any hardware; it was not tested. This leaves the seam between
`parseMountinfo` and `underRoot` — the `root` filtering that decides whether a
stick is seen at all — asserted only on the board.

### ⚠️ 8 — Minor: the accepted-ingest log omits the image count Step 26 asks for

`slideshow.go:444`

```go
log.Printf("media: %s accepted (images applied: %v)", m.mountPoint, res.imagesApplied)
```

Step 26 specifies *"log at info level (mount point, image count, whether images
were applied)"*. `ingestResult` carries no count field, so the count is not
available at this call site — the gap is in the result type, not just the format
string.

### ⚠️ 9 — Informational: `staticcheck` was not run

CLAUDE.md mandates `go vet` and `staticcheck`; Step 32b softens the latter to
"if available". It is not installed on this machine and no network fetch was
authorised, so it was skipped. `go vet` is clean.

### Documented deviations that check out

Not findings — verified as legitimately substituted, per tasks.md's own text:

- Step 34(f) "stick pulled mid-copy" deferred to `ingest_test.go:336` — that test exists and asserts the active set intact plus no staging left behind.
- Step 34(g) "two sticks at once" deferred to `TestHandleMediaMountQueue` (`slideshow_test.go:1010-1104`) — both the ordering and the dedup subtests exist.
- The `ATTRS{removable}=="1"` guard removal is consistently reflected in `install.sh:100-129`, `Architect.md:521-542`, and `tasks.md:147`, with the same reasoning in all three. The two-rule `ID_FS_TYPE` split Step 29 requires is present at `install.sh:124-125`, the `99-` prefix is used, and the `systemd-mount` presence warning (Step 29b) is at `install.sh:80-82`.
- `docs/INSTALL.md` carries the three-step recovery procedure, the FAT32/exFAT guidance, the physical-port security note, and the re-run-the-installer note. `README.md` and `CLAUDE.md` carry their Step 31/32 content.

## Verdict

- [ ] **PASS** — all criteria ✅, ready to archive
- [x] **PARTIAL PASS** — only ⚠️ warnings, archive with known gaps noted
- [ ] **FAIL** — one or more ❌, specific items must go back to coder

No ❌. Every computational sensor that could be run is green, including the
armv7 cross-compile and `-race`. The implementation matches the Architect's
contract table entry-for-entry, and the transaction semantics the Analyst cared
most about — three dispositions, all-or-nothing, bounded per-payload retention,
never-empty-the-rotation, the stick never written to — are all present and
directly tested.

The nine warnings are concentrated in two places: **verification the spec asked
for and did not get** (Finding 1 is the significant one — the design's
self-declared highest risk is still unmeasured), and **error/defence branches
that exist but are unexercised** (Findings 2, 3, 7). Nothing here blocks
archiving; Finding 1 is the one worth resolving before the sign goes back into
public service, and it needs a board, not a coder.

> **2026-09-01 update:** Finding 1 is now addressed by user observation (see
> the annotation on Finding 1 above and `tasks.md`'s Step 34 addendum) —
> multiple hardware trials with consistent sub-2s hotplug response, which is
> inconsistent with the 5s fallback path being the one firing. This is a timing
> inference, not log-instrumented proof, so it is recorded as resolved-by-
> observation rather than fully closed by measurement. Of the nine warnings,
> one (Finding 1) is now addressed this way; the remaining eight are
> unexercised-but-implemented defence/error branches (Findings 2–9) that still
> do not block archiving.
