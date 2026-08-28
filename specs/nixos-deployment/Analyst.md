# Analyst: NixOS Deployment Support

> Phase 1 — Problem definition. Approved before architecture begins.

## Goal / Outcome

**Scope classification:** Complex

Mural is deployed today by `curl … | bash` onto Raspberry Pi OS Lite. That
installer is a one-shot imperative mutation of a mutable system: it runs `apt`,
downloads a release binary, writes dotfiles into `$HOME`, appends to
`~/.bashrc`, drops a `systemd` unit override into `/etc/`, and appends a share
stanza to `/etc/samba/smb.conf`. It works, and for a single sign it works well.

The cost lands later. A deployed sign is unattended, physically remote, and
maintained by people who are not administrators. What a given sign is actually
running is knowable only by logging into it and looking. Two signs installed six
months apart are not the same system: `apt` moved underneath them, the installer
changed, and whatever was hand-patched to make a particular display or a
particular network behave was never written down anywhere. When an SD card dies
— the characteristic failure of this hardware — recovery means re-walking
`docs/INSTALL.md` by hand and hoping the result matches what was lost.

The outcome sought is that **a sign's entire configuration is a single
declarative artifact**: buildable to an identical result from a clean machine,
diffable, revertible to the previous known-good state if a change breaks the
display, and re-appliable to replacement hardware without a human retracing the
steps. That is what a NixOS target would buy, and it is the only reason to want
one — this is a *deployment and lifecycle* problem, not a shortcoming in the
player itself. Nothing here asks Mural's Go code to behave differently.

**The user has decided this is worth doing, and worth doing as a replacement
rather than an addition.** NixOS becomes *the* supported deployment target for
Mural on Linux; Raspberry Pi OS is retired rather than maintained alongside it.
That choice buys a single coherent deployment story and removes a permanent
double-maintenance burden, at the cost of stranding already-deployed Debian
signs and leaving the project without a fallback if the hardware does not
cooperate. Both costs are accepted knowingly and are recorded in full below.

This phase defines **what that replacement must achieve and what it must not
break** — not how to build it.

## Scope

**Included:**

- **Retiring the Raspberry Pi OS path.** The user has decided: NixOS
  **replaces** Raspberry Pi OS as the supported deployment target rather than
  joining it. `install.sh` is retired, `docs/INSTALL.md` is rewritten for
  NixOS rather than qualified, and the project ships exactly one supported way
  to deploy a sign.
- **Reconsidering the purpose of the published release binaries**, which lose
  their documented consumer when the Debian path goes away.
- **Documenting the migration gap for already-deployed Debian signs** — see
  *Rules & Constraints*. Building migration tooling is out of scope; leaving
  the gap unstated is not.
- Establishing how a runnable Mural is obtained on a NixOS host at all, given
  that the published release binaries cannot run there (see *Rules &
  Constraints* — this is a finding of this phase, not an assumption).
- Establishing how the kiosk session — autologin, X, a bare window manager, and
  launching the player — is expressed declaratively.
- Establishing how the content directory and its `config.toml` are provisioned,
  while preserving the fact that both are **mutable runtime state** written by
  operators, not configuration owned by the system.
- Establishing how network file sharing of the content directory is expressed,
  preserving the current access restriction (authenticated users only, never
  anonymous).
- Confirming that Mural's three external CLI dependencies remain discoverable
  and behave equivalently, and defining what happens to the documented
  graceful-degradation behaviour that depends on them being absent.
- Defining the update path for a deployed NixOS sign, since the current one
  ("re-run the installer, it fetches the latest release") does not transfer.
- Defining what hardware "NixOS support" is actually claimed on, and what is
  merely believed to work.
- Establishing, by investigation rather than assumption, whether NixOS runs
  acceptably on the Pi-class hardware actually deployed. This is the largest
  unknown in the feature and is scoped here as a required spike.
- The documentation this implies, and what happens to the existing
  Raspberry Pi OS documentation.

**Excluded (non-goals):**

- **Any code change, in this phase or as an outcome of it.** Mural's Go source
  is not expected to change. If the architecture phase finds a change is
  required, that is a finding to escalate, not a licence to make it.
- **Writing the flake, the module, the packaging, the CI changes, or the new
  documentation.** Phase 1 is problem definition. No file outside
  `specs/nixos-deployment/` is touched.
- **Windows.** Unchanged and unaffected. The replacement decision applies to
  the Linux deployment target only; it does not narrow the project's stated
  intent to support Windows.
- **Making Mural a general-purpose desktop application on NixOS**, or
  packaging it for a desktop user's `environment.systemPackages`. The target is
  the unattended kiosk deployment, the same use case the current installer
  serves.
- **Upstreaming a package to `nixpkgs`.** A worthwhile thing, a different
  project, and one that imposes review obligations this project has not agreed
  to.
- **Building and publishing bootable SD card images.** Plausibly the natural
  endpoint of this work and explicitly deferred; it is a substantial project of
  its own and the feature must be useful without it.
- **Fleet deployment tooling** — remote push, multi-host orchestration,
  secrets management, monitoring. One sign, deployed deliberately, is the unit
  of work here.

  *Sharpened by the user: there is exactly one physical deployment in
  existence, and the goal is reproducibility of that single install — the
  ability to rebuild or redeploy this one Raspberry Pi reliably and
  identically — not a mechanism for provisioning N signs. Where the rest of
  this document speaks of "a sign" or "signs" in the plural, that is the
  general problem class a declarative approach addresses (and the property a
  reproducible build must have — see the "two machines" behaviour below), not
  a claim that multiple signs exist or that this feature manages a fleet of
  them.*
- **Building migration tooling for existing deployed signs.** No automated
  conversion, no in-place upgrade path, no dual-boot arrangement. A deployed
  Debian sign is re-provisioned from scratch or left alone.

  *This is a non-goal, not a non-issue. Under replacement those signs are
  running a path the project no longer supports, and that consequence must be
  written down where an operator will find it — see the migration-gap rule
  below. What is excluded here is the work of automating the transition, not
  the obligation to be honest that it exists.*

## Behaviour

Declarative statements about what the deployment path must do. "The system"
here means the install and deployment mechanism, not the player process.

- When an operator installs Mural on a NixOS host, the system must produce a
  working sign without requiring an imperative script that mutates system
  state, and without requiring the operator to hand-edit files the system also
  manages.
- When the same declarative configuration is applied to two machines of the
  same architecture, the system must produce the same result on both. (Stated
  as a determinism property the single deployment must have — e.g. rebuilt
  after a card dies, or reproduced on a replacement board — not as a
  requirement to provision multiple signs; see the fleet non-goal above.)
- When a configuration change is applied and it breaks the sign, the previously
  working state must remain recoverable without network access, a rebuild, or a
  re-image.
- When Mural runs on a NixOS host, `ffmpeg`, `ffprobe`, and `cec-client` must
  be discoverable by name on the process's `PATH`, because the player resolves
  all three with `exec.LookPath` at runtime and holds no configured paths.
- When any of those three tools is deliberately not provided, the player must
  degrade exactly as it does today — video skipped and logged, CEC a silent
  no-op — and must not fail to start.
- When CEC display control is enabled, the user the player runs as must have
  read/write access to the kernel CEC device. Providing `cec-client` alone is
  insufficient: device-node permissions are a separate, independently necessary
  condition, and without them CEC fails at runtime despite a correct-looking
  configuration.
- When the host boots, the system must reach the running slideshow without
  human input: no login prompt, no manual `startx`, no keyboard.
- When the player exits or crashes, the system must return to a state an
  operator can recover from, matching the intent of the current installer's
  30-second guarded restart rather than leaving a dead screen.
- When the system provisions the content directory, it must create it and seed
  a starter `config.toml` **only if absent**, and must never overwrite,
  revert, or delete content an operator has since placed there — including
  across rebuilds and across reboots.
- When network file sharing is enabled, access must require authentication as a
  known user; anonymous or guest access must not be reachable.
- When network file sharing is not enabled, no share and no file-sharing
  service may be present.
- When a new version of Mural is released, an operator must have a documented
  way to move a deployed sign to it, and a documented way to move it back.
  Both directions must be written down — the current deployment has no recorded
  update procedure at all, so this is ground the feature gains rather than
  parity it must preserve.
- When an operator follows the NixOS documentation end to end on the supported
  hardware, they must arrive at a running sign without needing to consult
  general NixOS documentation to fill in gaps.

## Rules & Constraints

- **The published release binaries cannot run on NixOS.** This was verified
  against `.github/workflows/build.yaml` during this phase and is the single
  most consequential constraint here. CI builds with `CGO_ENABLED=1` and links
  against Ubuntu's `libgl1-mesa-dev`, `libx11-dev`, `libxrandr-dev`,
  `libxinerama-dev`, `libxcursor-dev`, `libxi-dev`, and `libxxf86vm-dev` —
  Fyne requires CGo, so the artifact is a dynamically linked ELF expecting a
  filesystem layout NixOS does not have. `install.sh` step 2 therefore cannot
  be reused, adapted, or conditionally branched: **the NixOS path must build
  Mural from source.** Any plan premised on "download the same binary, install
  different packages" is invalid.

  *This contradicts the assumption the feature was scoped under — that the
  binary needs no attention because CI already cross-compiles it. It is
  recorded prominently because it moves work from "packaging" into "building,"
  and because a Phase 2 that inherits the original assumption will design the
  wrong thing.*

  **Under replacement this resolves cleanly rather than awkwardly.** The
  awkward version was *additional*: keep publishing binaries that work for one
  supported target and not the other, and explain the distinction in the
  documentation. With the Debian path retired, the Linux release artifacts
  simply lose their last documented consumer — and since they could never have
  run on the new target anyway, retiring or repurposing them removes something
  already inapplicable rather than giving up a working capability. Nothing is
  lost that currently works.

  *What to actually do with the Linux half of the release workflow — delete it,
  keep it for non-NixOS users as explicitly unsupported, or repoint it — is
  Phase 2's call. Note that the Windows target is unaffected and is a reason
  the workflow may still need to exist.*

- **`install.sh` cannot be extended to cover NixOS; it is retired, not
  adapted.** Not a matter of taste. Every one of its six substantive steps is
  Debian-shaped and fails or is meaningless on NixOS: `apt` does not exist;
  the binary will not run; `/etc/samba/smb.conf` and `/etc/systemd/system/` are
  generated from the system closure and hand-edits there are discarded on the
  next rebuild; and appending a launcher hook to `~/.bashrc` is precisely the
  undeclared drift the target exists to eliminate. A script that mutated a
  NixOS host into a working sign would produce a machine that stops being one
  the next time anyone rebuilds it.

  *Stated as sizing evidence, not as design. Whether the answer is a flake, a
  module, both, or something else is Phase 2's decision.*

  *Under replacement this stops being a trade-off and becomes simple deletion:
  there is no second path to keep working, so the script is removed rather than
  guarded, taught to detect NixOS, or left to rot as a trap for someone who
  finds it in the repository and runs it.*

- **Mural must remain unprivileged.** It runs as an autologin kiosk user today
  and must not gain root, a setuid helper, or capabilities. This is a
  pre-existing invariant, reaffirmed by the `usb-stick-hotplug` spec, and the
  declarative path must not quietly relax it in exchange for convenience.

- **The content directory is mutable state and must never be managed
  declaratively.** Operators write to it over the network share, over SSH, and
  (per `usb-stick-hotplug`) from a USB stick; the player itself will write to
  it. Placing it under declarative management would mean a routine rebuild
  silently reverting a sign's content. Seeding-if-absent is the only acceptable
  relationship the system may have with it.

- **The Samba password database is irreducibly stateful.** The share
  *definition* — `valid users`, `guest ok = no`, `force user` — maps cleanly
  onto declarative configuration. The credential does not: `install.sh` calls
  `smbpasswd -a` interactively, and the resulting database is runtime state
  that no rebuild can reproduce. **Full declarative parity with the current
  Samba setup is therefore not achievable**, and the feature must state which
  residual manual step remains rather than implying it has been eliminated.

- **The interactive installer prompts have no declarative equivalent.**
  `install.sh` asks two `[Y/n]` questions at install time. A declarative system
  answers those before the fact. The two optional features must become
  configuration the operator sets ahead of time, and the documentation must
  compensate for the loss of a prompt that currently tells them the choice
  exists at the moment it matters.

- **The documented `ffmpeg` kill switch does not transfer, and may become
  impossible.** `docs/INSTALL.md` currently offers `sudo apt remove ffmpeg` as
  an immediate, no-reboot revert of video playback on a misbehaving kiosk. On
  NixOS this requires a rebuild at best. Worse, if packaging pins the tools
  onto the player's `PATH` — the natural way to guarantee the dependency —
  then removing them out from under a running sign becomes impossible by
  construction. The escape hatch must be preserved in some form or its loss
  documented deliberately; it must not simply disappear.

- **Dependency guarantees cut both ways.** Pinning `ffmpeg`/`ffprobe`/
  `cec-client` makes the graceful-degradation paths in `video.go` and `cec.go`
  unreachable in normal operation. That is mostly a benefit — those paths exist
  because Debian installs can be incomplete — but they must remain reachable
  deliberately, since "run without CEC" and "run without video" are supported
  configurations, not merely failure states.

- **There is now exactly one supported way to deploy a sign, and therefore no
  fallback.** This is the structural consequence of choosing replacement, and
  it changes how every other risk in this document must be treated. Under the
  rejected *additional* reading, a NixOS path that failed to materialise would
  have been a disappointment. Under replacement it leaves the project with no
  deployment story at all. Nothing here may be planned on the assumption that
  the Debian path is standing by to catch a failure — the moment it is retired,
  it is not.

  *One consolation: the worry that two supported targets might drift apart in
  behaviour vanishes along with the second target. There is one path, so there
  is nothing to keep in sync.*

- **CEC is best-effort, and is not a gate on this feature.** Display control is
  wanted where it works, but it is inherently hardware-dependent — whether a
  given screen honours CEC at all is outside this project's control, and
  `cec.go` is already built for that reality, degrading to a silent no-op when
  `cec-client` is absent. That contract is the position here too: **a NixOS
  deployment whose CEC does not work on day one is a working deployment**, with
  the schedule still governing what is displayed. CEC must not be treated as a
  go/no-go risk for the NixOS path, and must not block the gate.

  One practical note for whoever implements it, recorded so it is not
  rediscovered the hard way: reaching the kernel CEC device requires group
  membership (conventionally `video`) or an equivalent `udev` rule, over and
  above installing the tool. On the current Debian target this is invisible
  because the default user already has it; a declaratively created kiosk user
  would not. It is the likeliest reason for CEC to look correctly configured
  and still do nothing — worth checking `nixpkgs`' rules rather than assuming,
  and worth a verification item, but nothing more than that.

- **NixOS on the actual target hardware is unverified by anyone consulted, and
  is the feature's principal risk.** The deployed hardware is an older
  Pi-3-class 64-bit board. Neither consulted source could speak to the
  NixOS-on-Raspberry-Pi story — boot and firmware handling, `nixos-hardware`
  profiles, `aarch64` image generation, or whether a build is done on-device or
  elsewhere. Raspberry Pi OS is the vendor's own operating system for this
  board and NixOS is not; the gap is real and its size is currently unknown.

  *Two further pressures compound it. A Pi-3-class board is modest hardware for
  building anything locally, which makes "just rebuild on the device" a claim
  requiring evidence rather than a default. And the existing installer's own
  hardware notes are sparse enough that if the vendor OS is silently handling
  display or firmware details, nothing in this repository records what they
  are — so a NixOS port would have to rediscover them.*

  **This must be settled by a spike on real hardware before any commitment**,
  and the replacement decision raises its stakes considerably. Under
  *additional*, a board that would not boot cost the effort and nothing else.
  Under replacement, the Debian path is being retired in favour of this one, so
  a late discovery does not merely waste work — it removes the project's only
  deployment story. The spike is correspondingly non-negotiable and must come
  first.

  **Spike definition — carry into Phase 2 as task 1, blocking all others.**
  Phase 2 should plan no packaging, module, or documentation work that is not
  contingent on this passing. It must answer, on the real board:

  1. **Boot.** Does a current NixOS `aarch64` image boot the Pi-3-class board
     to a usable console — and by what firmware/bootloader route?
  2. **Display.** Does X come up on the HDMI output at the sign's resolution,
     with working GL sufficient for Fyne? A sign that boots headless is not a
     sign.
  3. **Build feasibility.** Can Mural (CGo, Fyne, full GL/X11 dependency
     closure) be built for this target at all, and where — on-device, by
     cross-compilation, or by an emulated or remote builder? Record the
     wall-clock cost, because it decides the update model rather than merely
     informing it.
  4. **Runtime dependencies.** Are `ffmpeg`/`ffprobe` (with H.264 decode) and
     `cec-client` available and functional for this architecture?
  5. **Sustained operation.** Does the board run a slideshow — images and
     H.264 video — without thermal, memory, or performance regression against
     the Debian baseline? Replacement means accepting this as *the* platform,
     so "boots once" is not the bar.

  **Exit criteria.** Items 1–3 are pass/fail and gate the feature outright: any
  failure returns the replacement decision to the user rather than being
  designed around, since there is no fallback path to retreat to. Item 4 is
  pass/fail for video; CEC failure is tolerated per the best-effort rule above.
  Item 5 is a judgement call whose result is recorded either way and which may
  legitimately narrow the supported-hardware claim without killing the feature.

  *If the spike cannot be run — no spare board, no willingness to risk a live
  sign — that is itself a finding to escalate before Phase 2 proceeds, not a
  reason to proceed on optimism.*

- **Every line of Nix here is net-new; there is no in-house pattern to copy.**
  The available NixOS configuration to draw on is a flake-based single-host
  desktop with `home-manager` wired in as a module. It contains **no** local
  package definitions of any kind — no `buildGoModule`, no overlays, no
  packages flake output — and **no** kiosk, autologin, or bare-window-manager
  configuration; it is a full desktop environment on x86-64, which is close to
  the opposite of what a sign needs.

  Both halves of the work — packaging a CGo Go application, and expressing an
  autologin X11 kiosk session — would therefore be written from scratch against
  standard idioms rather than adapted from something already working and
  understood. That is ordinary work, not a blocker, but it must be sized as
  **building two unfamiliar things**, not as pointing an existing installer at
  a different distribution. Optimistic sizing is the main way this feature goes
  wrong after the hardware risk.

- **The `ffmpeg` variant is a real decision with runtime consequences.**
  `nixpkgs` offers several (full, headless, and versioned variants) which
  differ in codec and feature coverage. Mural needs both the `ffmpeg` and
  `ffprobe` executables and H.264 decoding. Debian's single `ffmpeg` package
  makes this a non-question today; it becomes a choice that must be made
  deliberately and verified against real content, not inferred from a name.

- **Support must be claimed only where it is tested.** NixOS on x86-64 and
  NixOS on a Raspberry Pi are materially different propositions — the second
  involves firmware, boot, and hardware-support questions the first does not.
  Since the deployment target is a Pi, **verification on x86-64 does not
  discharge this feature**; it would demonstrate the packaging while leaving the
  actual risk untouched. Whichever is verified is what may be documented as
  supported; the other is "believed to work," if it is mentioned at all.

- **Already-deployed Debian signs become unsupported, and this must be stated
  plainly.** Replacement means the path those signs were installed with no
  longer exists. Nothing reaches out and breaks them — they keep running — but
  they receive no further installer, no documented update route, and no
  documentation that still describes them. The practical consequences are that
  a sign which dies is re-provisioned onto NixOS rather than rebuilt as it was,
  and that the `docs/INSTALL.md` an operator has bookmarked stops matching the
  machine in front of them.

  **The obligation is disclosure, not remedy.** Automated migration is a
  non-goal; leaving operators to discover the gap on their own is not
  acceptable. The rewritten documentation must say what happens to a sign
  installed the old way, in terms a non-administrator can act on.

- **Replacement removes the double-maintenance cost, and that is a genuine part
  of its value.** Under *additional*, every future change touching install,
  dependencies, or the kiosk session would have had to be made twice and
  verified twice, with a release process able to check only one of them
  automatically. Replacement buys that away permanently: one path, one set of
  documentation, one thing to verify.

  *Recorded because it is the compensating benefit for the migration gap above.
  The two belong together — replacement trades a one-time stranding cost for a
  standing reduction in maintenance surface.*

## Edge Cases

| Scenario | Expected behaviour |
|----------|--------------------|
| Operator downloads a release binary and runs it on NixOS | Fails to execute — dynamic loader and library paths absent. Must be documented as expected, not filed as a bug; the NixOS path builds from source |
| Operator runs `install.sh` on NixOS | Moot going forward — the script is retired rather than taught to detect and refuse. Recorded because it is *why* adaptation was rejected: the script fails at `apt`, and any hand-forced completion would produce a host that stops being a sign at the next rebuild |
| A rebuild is applied while the sign is mid-rotation | Content and `config.toml` survive untouched; a restarted player resumes from the existing content directory. A rebuild must never be a content-loss event |
| A rebuild breaks the display | Previous generation remains bootable and selectable without network access. This is the core value proposition and must actually hold, not be assumed from NixOS's reputation |
| Operator edits `config.toml` over the share, then a rebuild happens | Their edit survives. The seeded starter config is a first-boot convenience only, never re-asserted |
| Content directory already populated at first build | Seeding is skipped entirely; nothing is overwritten |
| `cec-client` present but the display ignores CEC | Unchanged from today — CEC commands are issued and have no effect. Not a NixOS concern, but the packaging must not make it *look* like one by changing the failure signature |
| Deliberately deploying without video support | Supported configuration. Player starts, `.mp4` files are skipped and logged, exactly as on a Debian host without `ffmpeg` |
| Deliberately deploying without CEC | Supported configuration. Player starts, display power control is a silent no-op, schedule still governs what is shown |
| Video misbehaving on a live sign, operator wants the documented kill switch | Does not work as documented. Requires a replacement mechanism or an explicit documentation change — see *Rules & Constraints* |
| Samba enabled but no password ever set for the user | Share exists and is unusable. Must be called out in documentation, since unlike the current installer nothing will interactively prompt for it |
| Anonymous connection attempted against the share | Refused, matching `guest ok = no` and `valid users` today. Parity here is a hard requirement, not a nicety |
| Player crashes repeatedly on a headless sign | Restart behaviour matches the current guarded loop's intent; must not become a tight crash loop, and must not leave a permanently black screen with no path to a console |
| Sign is powered off mid-rebuild | Boots into a working generation. Standard NixOS behaviour, but worth verifying on the actual SD-card hardware rather than assuming |
| Operator wants to move a deployed sign back to a previous Mural version | Documented and possible. The current path has no real answer to this, so this is an improvement to claim only if it is genuinely provided |
| Kiosk user can reach `cec-client` but not the CEC device node | CEC silently does nothing while everything looks configured. The deployment is still valid and the sign still works — CEC is best-effort — but this is the first thing to check when display control misbehaves |
| Chosen `ffmpeg` variant lacks H.264 decoding | Every video is rejected and logged, images keep working. Must be caught by verification against real content, since the failure looks identical to a bad file |
| Raspberry Pi hardware where NixOS lacks working display or boot support | Feature is not available on that hardware, and the effort is abandoned rather than worked around. Must be discovered by the up-front spike, not by an operator with a dead sign |
| Spike shows NixOS runs on the board but building on-device is impractically slow | Does not kill the feature but forces the update model — the build must then happen elsewhere, which makes remote reachability a hard requirement rather than a convenience |
| Existing Raspberry Pi OS sign, after this feature ships | Keeps running and keeps displaying content — nothing reaches out to break it — but is now on an unsupported path with no installer, no update route, and no documentation describing it. Must be disclosed to operators, not silently dropped |
| Existing Raspberry Pi OS sign whose SD card then dies | Re-provisioned from scratch onto NixOS, not rebuilt as it was. This is the migration gap made concrete, and the moment an operator actually encounters it |
| Operator finds an old copy of `install.sh` and runs it | The script is deleted, not left in the repository to be found. Anything that survives in a shell history or a bookmark is outside the project's control, which is exactly why the documentation must state that the old path is gone |

## Open Questions

| Question | Answer |
|----------|--------|
| Is NixOS an **additional** target or a **replacement** for Raspberry Pi OS? | **Answered by the user: full replacement.** NixOS becomes the supported deployment target; Raspberry Pi OS is retired rather than kept alongside. `install.sh` is removed, `docs/INSTALL.md` is rewritten rather than qualified, the Linux release binaries lose their documented consumer, and existing Debian signs become a disclosed migration gap. The alternatives — *additional*, and a staged middle in which the Debian path is frozen but not removed — were both put to the user and declined. This document and `Delta.md` are written to that decision throughout; no hedging remains |
| Can the existing release binary be reused on NixOS? | **No — settled by inspection, not open.** CGo plus Ubuntu-linked GL/X11 libraries. The NixOS path builds from source. See *Rules & Constraints* |
| Does this require changes to Mural's Go source? | **Expected: no.** All three external tools are already resolved via `PATH` at runtime and already degrade gracefully when absent, which is exactly what a Nix-wrapped invocation needs. Treated as a finding to confirm in Phase 2, not a guarantee |
| Does this require CI changes? | **Reopened by the replacement decision — the earlier "no" no longer holds.** With the Debian path retired, the Linux release artifacts lose their last documented consumer. This resolves cleanly rather than painfully: those binaries could never have run on NixOS anyway, so retiring or repurposing them discards something already inapplicable rather than a working capability. What to actually do with the Linux half of the workflow — delete, keep as explicitly unsupported, or repoint — is Phase 2's call, and the Windows target is unaffected and may independently justify keeping it. Writing the change remains out of scope for Phase 1 |
| What happens to signs already deployed on Raspberry Pi OS? | **They keep running, become unsupported, and the gap is documented rather than automated.** Nothing reaches out and breaks them, but no installer, update route, or documentation will still describe them; a sign that dies is re-provisioned onto NixOS. Building migration tooling is an explicit non-goal — disclosing the consequence in the rewritten documentation is an explicit requirement. This is the acknowledged cost of replacement, accepted by the user with the alternatives in view |
| Which hardware is the actual target — Raspberry Pi, or x86-64? | **Answered: a Raspberry Pi, an older Pi-3-class 64-bit board.** Confirmed by consultation during this phase. The risk therefore does *not* evaporate — it is the live one. It also means x86-64 verification cannot stand in for real verification, and that the board is modest enough for on-device builds to need proving |
| Will NixOS actually run acceptably on that board? | **Open, unverified, and the feature's gating risk.** Nobody consulted had NixOS-on-Pi experience to offer. Requires a hardware spike — boot and firmware, `aarch64` image generation, display bring-up, and whether builds happen on-device or elsewhere — **before** any commitment to Phase 2. If this fails, the feature ends here, and it is far cheaper to learn that now |
| How much Nix has to be written from scratch? | **All of it — more than the framing suggested.** There is no in-house precedent for either half: no local package definitions of any kind, and no kiosk/autologin configuration anywhere to adapt. Both the packaging and the kiosk session are net-new against standard idioms. Not a blocker, but the feature must be sized as building two unfamiliar things rather than as retargeting an installer |
| Which `ffmpeg` variant? | **Open.** `nixpkgs` offers several with differing codec coverage; Mural needs the `ffmpeg` and `ffprobe` executables plus H.264 decode. A non-question on Debian, a deliberate choice here, and one to verify against real content rather than infer from the package name |
| Is CEC a risk to this feature? | **No — explicitly settled by the user.** CEC is best-effort: wanted where it works, not a blocker, and already designed to no-op gracefully. It gets a verification item and nothing more, and must not be allowed to gate the NixOS path |
| Is full declarative parity with the current Samba setup achievable? | **No.** The share definition maps over cleanly; the password database is runtime state that cannot be reproduced by a rebuild. The feature must name the residual manual step honestly rather than claim parity |
| What replaces the `sudo apt remove ffmpeg` kill switch? | **Open — must be answered, not dropped.** It is documented operator-facing behaviour for a misbehaving kiosk. Options are a configuration toggle plus rebuild, an in-application setting, or an explicit documented removal of the capability. Silently losing it is not acceptable |
| How does a deployed NixOS sign get updated? | **Open, but better-positioned than expected.** Consultation established that there is no documented update procedure today at all — no installer re-run, no recorded mechanism — while remote administrative access to deployed signs over SSH already exists and is the normal way in. A declarative push-from-elsewhere model therefore fits the way these signs are already reached, and would replace an undocumented process rather than displace a working one. Recorded as a promising direction, **not** a decision: on-device rebuild versus remote push is Phase 2's to make, and it is partly forced by the hardware spike's findings about build speed |
| Are bootable SD images part of this? | **No — deferred deliberately.** Likely the natural continuation, and a large project in its own right. This feature must stand on its own without it |
| Should the NixOS path be upstreamed to `nixpkgs`? | **No.** Out of scope; imposes external review obligations the project has not taken on |
| Scope classification | **Complex.** New domain (Nix packaging and NixOS module semantics), cross-cutting across build, installer, kiosk session, file sharing, update path, and documentation, with a pivotal product decision still open and a load-bearing assumption already found to be false. Full pipeline including `sdd-harden` |

## Analyst Checklist

- [x] Goal is tied to a specific user need
- [x] Scope boundaries are explicit — what's in and what's out
- [x] All ambiguities resolved — **every specification-level question is
      settled.** The additional-vs-replacement decision has been made by the
      user (full replacement) and this document commits to it throughout. What
      remains outstanding is not an ambiguity but an empirical unknown: whether
      NixOS runs on the target board. No amount of specification can answer
      that, which is why it is a spike rather than an open question
- [x] Behaviour is declarative, not prescriptive
- [x] Edge cases are identified and handled
- [x] Non-goals prevent scope creep

**Gate status: ready for Phase 2, with one carried dependency.**

The additional-vs-replacement decision — previously the blocking condition — has
been answered by the user as **full replacement**, and this document and
`Delta.md` are written to it with no remaining hedges.

**The hardware spike carries forward as Phase 2's task 1, blocking all others.**
It is deliberately *not* resolved here, because it is a question about physical
hardware rather than about the specification. Its full definition, exit
criteria, and escalation path are in *Rules & Constraints* → "NixOS on the
actual target hardware is unverified"; the architect should lift that directly
into `tasks.md` as the first task and make the rest contingent on it. Items 1–3
of that spike (boot, display, build feasibility) are pass/fail and return the
replacement decision to the user if they fail, since replacement leaves no
fallback path to retreat to.

**Current status: the single physical deployment is temporarily deactivated
for physical-constraint reasons** (unspecified further here). It is not
believed to be gone permanently, and this does not by itself block the
feature — but it does interact with the still-open boot criterion above: per
`spike-findings.md`, that criterion already required a spare SD card to safely
close out (to avoid touching the production sign's own card), and the board
being deactivated is a further reason that exercise may not happen soon. Treat
the boot-criterion gate as pending for longer than previously assumed, not as
newly at risk or invalidated.

Nothing else needs resolving before Phase 2 begins. In particular, CEC is
explicitly *not* a gating condition: it is best-effort, already degrades
gracefully by design, and a NixOS sign whose CEC does not work is still a
working sign.
