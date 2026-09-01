# Critic Review: Native Rendering Layer (DRM/KMS + evdev)

> Phase 3 — Plan stress-test. Approved before coding begins.
> This review went through two rounds. Round 1 = self-critique.
> Round 2 = fresh-eyes adversarial pass by an independent agent.

## Round 2 (fresh-eyes) — findings

A second adversarial pass with a fresh context window (zero prior context from
this conversation) found 1 additional blocker and 5 additional warnings that
Round 1 missed, plus 1 note. All have been applied.

### Round 2 Blockers (applied)

| ID | Finding | Where fixed |
|----|---------|-------------|
| B1 | Analyst.md explicitly requires that signal handling/mode restoration's "interaction with the guarded restart loop must be designed rather than assumed" — but `install.sh`'s `tty1-guard.sh` (which execs `startx` and wraps it in a 30s crash-loop banner, hooked via `.bashrc`) was never named anywhere in Architect.md or tasks.md Step 28. | Architect.md (new "guarded restart loop" design paragraph before the VT-switch section); tasks.md Step 28 (explicit `tty1-guard.sh` instruction: swap `startx` for a direct Mural exec, keep the banner/sleep/`.bashrc` shape unchanged) |

### Round 2 Warnings (applied)

| ID | Finding | Where fixed |
|----|---------|-------------|
| W1 | Log destination for "diagnosable error in its log" (Analyst.md requirement) was never made an explicit design decision — stderr's fate on a kiosk box with no systemd service and a console DRM is about to take mode-setting ownership of was left ambiguous. | Architect.md (new "Log output is unchanged" paragraph, cross-referencing `vt.go`'s console-restore path as the mechanism that keeps it readable) |
| W2 | tasks.md Step 31's README.md scope (Windows/CGo note only) missed several other stale lines found by inspection: the "built with Fyne" tagline, the Controls table's `Home | Rescan...` row, "Ratpoison will automatically fit it to screen," the `fyne.Do()`/"scaled to the window" lines, and `go run .` dev instructions that now need the `-headless` flag. | tasks.md Step 31 (expanded with the specific stale lines) |
| W3 | `context.Context` — CLAUDE.md's own Go convention for "cancellation and timeouts in long-running or concurrent operations" — is never mentioned anywhere in the four spec files; the concurrency redesign used a bespoke channel-based shutdown instead. | Architect.md (new "Shutdown is a `context.Context`" paragraph); tasks.md Steps 16, 18, 19, 21 (wire `ctx`/`cancel` through input watchers, the run loop, `NavQuit` handling, and `main.go`) |
| W4 | Analyst.md's "Where does this sit relative to the NixOS work?" open question and a matching Rules & Constraints bullet, plus a Delta.md bullet, all still described `specs/nixos-deployment` as racing to retire `install.sh` in conflict with this feature. That spec is now PARKED at Phase 1 (hardware spike failed; Phase 2/3 artifacts deleted as stale) — the collision no longer exists. | Analyst.md (open-question table row + Rules & Constraints bullet, both updated to RESOLVED); Delta.md (kiosk-session MODIFIED bullet's footnote updated) |
| W5 | No defer-cleanup discipline was specified for `OpenDRMRenderer`'s multi-step resource acquisition (fd → master → buffer 1 → buffer 2) despite CLAUDE.md requiring "defer for cleanup... immediately after acquiring the resource" — a partial failure could leak DRM master, the exact failure mode the feature exists to avoid. | tasks.md Step 8 (explicit defer-immediately-after-each-acquisition instruction) |

### Round 2 Notes acknowledged and applied
- `usb-stick-hotplug/Delta.md` requires eventually splitting `Reload`'s "rescan" from "resume," which this feature's ported `Reload` does not do. Confirmed as intentionally deferred per Analyst.md's own "whichever ships second re-reads the first" framing, and confirmed forward-compatible (splitting a single command-channel message later is no harder than splitting today's single `fyne.Do` call). Documented as a cross-reference in Architect.md's Risks section rather than treated as a redesign requirement.
- `TestSlideshowImageAdvanceTimingUnchanged`'s existing long comment explains Fyne-specific race-avoidance reasoning that becomes factually stale post-rewrite. Folded into tasks.md Step 25's instructions.

### Round 2 Residual risks (couldn't be verified offline)
- Hardware spike results (`kmsmove.c` timings, `pi3b.local` vc4-drm binding) — accepted as prose record from the Phase 1 spike, not independently re-verifiable without the board.
- Actual remote-control keycodes and RSS footprint — explicitly unresolved spike items 3/4, gated behind tasks.md Step 32/33 as already documented.

---

## Round 1 (self-critique) — findings

A self-critique pass (same context window as the spec's authoring) read
Analyst.md, Delta.md, Architect.md, and tasks.md against the current
codebase (`slideshow.go`, `main.go`, `go.mod`, `.github/workflows/build.yaml`,
`install.sh`, `CLAUDE.md`, `README.md`) and the two sibling specs
(`specs/nixos-deployment`, `specs/usb-stick-hotplug`). Found 2 blockers and 3
warnings. All applied.

### Round 1 Blockers (applied)

| ID | Finding | Where fixed |
|----|---------|-------------|
| B1 | `loadThumbnail` (slideshow.go:55, `resize.Resize(width, 0, src, resize.Lanczos3)`) was never mentioned in tasks.md Step 17, which only migrated `decodeAndFit` off `nfnt/resize`. Since Step 26 removes `github.com/nfnt/resize` from `go.mod` entirely, leaving `loadThumbnail` unmigrated breaks the build. `x/image/draw` has no width-only auto-height convenience, so the fix must also specify computing the target height manually. | Architect.md (Approach: new "Both scaling call sites move off `nfnt/resize`" paragraph; Files to Modify table row for slideshow.go); tasks.md Step 17 (expanded to cover `loadThumbnail` explicitly) |
| B2 | Analyst.md's edge-case table and Open Questions both explicitly required HDMI-hotplug behaviour to be "defined rather than discovered," but neither Architect.md nor tasks.md addressed display (as opposed to input-device) hotplug anywhere. | Architect.md (new "HDMI hotplug is a defined, minimal behaviour" design paragraph in the DRM Backend section); tasks.md Step 9 (Present's page-flip failure handling spelled out); Analyst.md (open-question row and edge-case row both updated to RESOLVED, pointing at Architect.md) |

### Round 1 Warnings (applied)

| ID | Finding | Where fixed |
|----|---------|-------------|
| W1 | Analyst.md cited `specs/nixos-deployment/spike-findings.md` as the source for a Tailscale/SSH-access claim, but that file no longer exists — it was deleted as stale along with the rest of `nixos-deployment`'s Phase 2/3 artifacts (per `CLAUDE.md`'s NixOS paragraph). Dangling citation. | Analyst.md (citation rewritten to point at the surviving `Analyst.md`, with an inline note explaining the deletion) |
| W2 | README.md's "first build takes 10-20 min on Windows due to Fyne's CGo compilation" note (line 96) wasn't explicitly covered by tasks.md Step 31, which named only the Windows/MSYS2/TDM-GCC section. Since CGo disappears project-wide, not just for Windows, this note needed explicit coverage. | tasks.md Step 31 (expanded — later folded into the larger Round 2 W2 expansion of the same step) |
| W3 | tasks.md Step 14's `parseInputEvent` design didn't explicitly state whether kernel autorepeat (`value == 2`) is treated the same as initial key-down (`value == 1`). Analyst.md's edge case requires a held nav key to keep advancing without lag ("each repeat advances one slide immediately"); the more obvious reading of the original wording ("key-up and non-key events return `ok == false`") could be misread as dropping repeat events. | Architect.md (new "Kernel autorepeat" paragraph in the Input section); tasks.md Steps 14 and 15 (explicit `value == 1`/`value == 2` handling and corresponding test coverage) |

### Round 1 verdict
Approved, with all findings applied. Proceeded to Round 2.
