# Delta: NixOS Deployment Support

> Specification delta — what changes relative to the current system.
> Only exists when this feature modifies existing behaviour.

**Decision recorded: full replacement.** NixOS becomes the supported deployment
target for Mural on Linux, and Raspberry Pi OS is retired rather than kept
alongside it. Two alternatives were put to the user and declined — *additional*
(both targets supported in parallel) and a staged middle (NixOS for new signs,
the Debian path frozen but not removed). This delta is written to the decision
that was made; it does not hedge against the ones that were not.

Note that Mural's runtime behaviour is almost absent from this delta. That is
the intended result: this feature changes how a sign is **built and
maintained**, not what it **does**. Any pressure to change player behaviour is a
signal that scope has slipped.

Also note what this delta is not: there is exactly one physical sign in
existence, and the target is reproducibility of that single install, not
fleet-provisioning tooling for many (see `Analyst.md`'s fleet non-goal).
Below, "a sign" is used generically for the class of deployment Mural
addresses; it does not imply more than one deployment exists or is being
managed.

## ADDED

- Mural is built from source as part of deployment. Today every documented
  install consumes a pre-built release artifact and no user ever compiles
  anything. The published binaries cannot run on NixOS (see **MODIFIED**), so
  the new path must build — introducing a build toolchain into deployment for
  the first time.
- A sign's configuration becomes a reviewable artifact. Today the authoritative
  description of a deployed sign is the sign itself; no file anywhere states
  what a given machine is running. This is the feature's actual product —
  reproducibility, rollback, and diffability all follow from it.
- Rollback to a previous known-good system state becomes possible. Today there
  is none: a change that breaks a sign is repaired forward, by hand, in place.
- A documented update procedure exists, in both directions. Consultation
  established that **no documented update path exists today at all**. This is
  new ground rather than preserved parity, and one of the clearer wins
  available.
- Deployment gains an explicit, verified hardware-support boundary. Today the
  claim "runs on a Raspberry Pi" rests on the vendor OS doing the work; a NixOS
  claim must be earned per-board and stated honestly.
- The choice of `ffmpeg` build becomes a decision the project makes. Debian's
  single `ffmpeg` package means this is currently not a question at all.
- Kiosk-user device-access requirements become explicit configuration. The
  current install silently inherits the default user's group memberships; a
  declaratively created user starts with none, so an invisible inheritance
  becomes something that must be written down.
- **A supported-hardware risk becomes load-bearing for the whole project.**
  Previously the Pi was supported by its vendor's own OS and the question did
  not arise. With replacement, whether NixOS runs on the target board is not a
  feature risk but a project risk — there is no second path to fall back to.
  This is why the hardware spike gates everything (see `Analyst.md`).

## MODIFIED

- **The release binary is the universal Linux delivery mechanism** → **the
  release binary has no supported Linux consumer.** Established during this
  phase by inspecting `.github/workflows/build.yaml`: the artifact is built
  with `CGO_ENABLED=1` against Ubuntu's GL and X11 development libraries, so it
  is a dynamically linked ELF that cannot run on NixOS.

  *This is the delta most likely to be missed, because it contradicts the
  reasonable assumption that cross-compiled Linux binaries are portable across
  Linux distributions. It is why the Debian path could not simply be taught
  about NixOS.*

  **Replacement resolves this cleanly rather than awkwardly.** The awkward
  version was *additional*: keep publishing binaries that work for one
  supported target and not the other, and explain the distinction in the
  documentation forever. With the Debian path retired, the Linux artifacts lose
  their last documented consumer — and since they could never have run on the
  new target, retiring or repurposing them discards something already
  inapplicable rather than a working capability. **Nothing that currently works
  is given up.** The Windows target is unaffected and may independently justify
  keeping the workflow; the disposition of the Linux half is Phase 2's call.

- **Installation is an interactive, imperative, one-shot mutation** →
  **installation is a declarative configuration applied ahead of time.**
  `install.sh` asks two `[Y/n]` questions at the moment of install and acts
  immediately. A declarative system has no interactive phase; those optional
  features become configuration decided in advance. The prompts do real work
  today — they tell an operator the options exist at the moment the choice is in
  front of them — and the rewritten documentation must carry that weight
  instead.

- **Optional features are enabled by re-running the installer** → **optional
  features are enabled by changing configuration and rebuilding.** `install.sh`
  is explicitly documented as re-runnable to turn on autologin later. The
  equivalent gesture is different in kind, not merely in syntax.

- **`sudo apt remove ffmpeg` is a documented, immediate, no-reboot kill switch
  for video playback** → **that gesture ceases to exist.** Currently promised in
  `docs/INSTALL.md` for a misbehaving live kiosk. Removal requires a rebuild at
  best; if packaging pins the tools onto the player's `PATH` — the natural way
  to guarantee the dependency — it becomes impossible by construction.

  *A genuine capability regression, not a cosmetic documentation change. The
  feature must either preserve the escape hatch in some form or withdraw the
  promise deliberately. It must not quietly stop being true while the
  documentation still claims it. With no Debian path left, there is no version
  of the docs in which the old gesture remains valid.*

- **The Samba share and its credential are configured together by one
  interactive step** → **the share is declarative and the credential is not.**
  `valid users`, `guest ok = no`, and `force user` map over cleanly and the
  authenticated-only restriction is **fully preserved**. The password database
  is runtime state that no rebuild reproduces. Parity is therefore partial, and
  the residual manual step must be documented rather than glossed.

- **Graceful degradation on missing tools is a safety net for incomplete
  installs** → **it becomes a deliberately selected configuration.** The
  fallbacks in `video.go` and `cec.go` exist because a Debian install can be
  missing packages. Under declarative packaging the tools are present by
  construction, so those paths stop being accidents and become choices —
  "deploy without video," "deploy without CEC" — that must remain reachable on
  purpose. No code changes; the meaning of existing behaviour changes.

- **A deployed sign runs a vendor-supported OS** → **a deployed sign runs an OS
  the project supports itself.** Raspberry Pi OS is the board vendor's own
  system, with a large body of hardware documentation behind it. NixOS on this
  board is not, and whatever the vendor OS was silently handling for display or
  firmware is not recorded anywhere in this repository. The project absorbs
  that support burden.

## REMOVED

- **`install.sh` is retired and deleted.** Not deprecated, not guarded behind a
  distribution check, not taught to detect NixOS and refuse — removed. Leaving
  it in the repository would leave a trap for anyone who finds it and runs it.
- **The Raspberry Pi OS installation path is no longer supported.** With it go
  every operator gesture documented against it: `curl … | bash` installation,
  `sudo apt install ffmpeg` to add video to an existing install, the `sudo apt
  remove ffmpeg` kill switch, `startx` as the documented launch command, and
  re-running the installer to enable autologin later.
- **`docs/INSTALL.md` as it currently exists.** Rewritten for NixOS rather than
  qualified or branched. Its opening premise — image an SD card with Raspberry
  Pi OS Lite — does not survive, and neither does the journey built on it.
- **The published Linux release binaries lose their documented consumer.**
  Whether the artifacts are deleted, kept as explicitly unsupported for
  non-NixOS users, or repointed is Phase 2's decision; what is removed here is
  their status as the supported way to obtain Mural on Linux.
- **Support for already-deployed Debian signs.** They keep running — nothing
  reaches out and breaks them — but receive no installer, no update route, and
  no documentation that still describes them. A sign that dies is
  re-provisioned onto NixOS rather than rebuilt as it was.

  *This is the acknowledged cost of replacement, accepted by the user with the
  alternatives in view. Building migration tooling is an explicit non-goal;
  disclosing the consequence in the rewritten documentation is an explicit
  requirement. The gap is not to be discovered by an operator standing in front
  of a dead sign.*

- **The implicit assumption that "Linux" is one deployment target** is retired
  — though it resolves differently than expected. It is nowhere written down,
  but it underpins `install.sh`'s architecture switch, the shape of the release
  workflow, and the project's stated intent to "target Linux but support
  Windows too." Under *additional* this would have become "ask which Linux."
  Under replacement it becomes narrower and simpler: **one Linux, named.**

- **The double-maintenance burden that *additional* would have introduced**
  never arrives. Every future change touching install, dependencies, or the
  kiosk session would have had to be made twice and verified twice, with CI able
  to check only one. Recorded here as the compensating benefit for the
  migration gap above: replacement trades a one-time stranding cost for a
  standing reduction in maintenance surface.
