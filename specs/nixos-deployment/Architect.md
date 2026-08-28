# Architect: NixOS Deployment Support

> Phase 2 — Design decisions. Approved before coding begins.
> Implementation checklist is in `tasks.md`.

## Approach

Mural's deployment becomes a **flake in Mural's own repository** that exposes
three things: a package, a NixOS module, and one concrete host configuration
for the single physical sign. Everything `install.sh` did imperatively is
expressed as module options; everything it did *statefully* (Samba password,
Wi-Fi PSK, the content directory) is named explicitly as residual state rather
than pretended away.

```
mural/
  flake.nix                    inputs, packages, overlay, module, host, checks, devShell
  nix/
    package.nix                buildGoModule derivation for mural
    modules/
      mural.nix                services.mural.* — user, content dir, CEC, share
      kiosk-x11.nix            services.mural.session = "x11" — greetd + X + ratpoison
    hosts/
      sign/default.nix         the one deployed Pi 3B+
      sign/boot.nix            extlinux + Pi firmware + kernel choice
    tests/
      kiosk.nix                nixosTest: session comes up, mural runs
      content.nix              nixosTest: seed-if-absent, survives rebuild
      share.nix                nixosTest: anonymous access refused
```

Three properties drive the shape:

**The SD card's write speed is the binding constraint, not the CPU.** The spike
measured ~0.3–0.4 MB/s effective write under unpack load — roughly 30× slower
than the board's network. Every decision that adds closure bytes (a display
manager, a full `ffmpeg`, an unstable channel with poor cache coverage) is paid
for at that rate. Every decision that avoids recompiling is nearly free, because
only 13 of 5,184 derivations are invalidated by a Mural source change.

**The boot criterion is unverified, so the design is arranged to discharge it as
the first step of provisioning rather than as a separate experiment.** Step 1
is "flash the stock upstream NixOS aarch64 image to the sign's SD card and boot
it" — which is simultaneously the boot test and the first half of the real
install procedure. There is one physical SD card for this single-deployment
project (not a fleet), and it is the same card that previously ran the Debian
install; the user confirmed she never intended to preserve that install, so
overwriting it carried no risk worth designing around.

**The presentation layer is provisional.** `specs/drm-renderer/` (Phase 1
complete, gate READY) intends to delete Fyne, X11, `ratpoison`, `unclutter`, and
CGo outright. That work is not designed here and is not waited on, but the X11
session is isolated behind `services.mural.session` so that its eventual removal
is a swap of one module file, not a rewrite of this one.

## Module Option Contract

There is no HTTP API here. The equivalent contract — the surface other
configuration binds against, which must be stable before anything else is
written — is the module's option set.

| Option | Type | Default | Meaning |
|--------|------|---------|---------|
| `services.mural.enable` | bool | `false` | Master switch |
| `services.mural.package` | package | `pkgs.mural` | The Mural derivation |
| `services.mural.user` | str | `"mural"` | Unprivileged kiosk user |
| `services.mural.contentDir` | path | `/var/lib/mural/content` | Images + `config.toml`; **runtime state** |
| `services.mural.seedConfig` | nullOr path | bundled sample | Copied to `contentDir/config.toml` **only if absent** |
| `services.mural.session` | enum `"x11"` / `"none"` | `"x11"` | Which presentation layer drives the display |
| `services.mural.restartDelay` | int (sec) | `30` | Pause before relaunching a crashed session |
| `services.mural.cec.enable` | bool | `true` | Put `cec-client` on the session PATH, grant CEC device access |
| `services.mural.share.enable` | bool | `false` | Samba share of `contentDir` |
| `services.mural.share.name` | str | `"content"` | Share name |
| `services.mural.share.validUsers` | listOf str | `[ config.services.mural.user ]` | `valid users` |
| `services.mural.share.openFirewall` | bool | `true` | Open SMB ports when the share is enabled |

`session = "none"` provides the package, the user, the content directory, and
CEC access, and drives no display. It exists so that a different presentation
layer — the `drm-renderer` work, or a future Kodi launcher — can be dropped in
without touching the core module.

## Data Model Changes

No schema changes; Mural has no database. The analogue is **on-disk state
layout**, which does change:

| Today (Debian) | Under NixOS | Managed by |
|----------------|-------------|------------|
| `~/mural/mural` (binary) | `/nix/store/…-mural-<ver>/bin/mural` | Nix, immutable |
| `~/mural/content/` | `/var/lib/mural/content/` | **Operator. Never declarative.** |
| `~/.xinitrc`, `~/.ratpoisonrc` | store-resident scripts, no dotfiles | Nix, immutable |
| `~/mural/tty1-guard.sh` + `.bashrc` hook | greetd session script | Nix, immutable |
| `/etc/samba/smb.conf` stanza | `services.samba.settings.content` | Nix, immutable |
| Samba password database | `/var/lib/samba/private/` | **Residual state.** One-time `smbpasswd -a` |
| Wi-Fi PSK (`raspi-config`) | `/etc/mural/wifi.env` (device-local, uncommitted) | **Residual state.** One-time write |
| `/etc/systemd/system/getty@tty1.service.d/` | `services.greetd`, `services.getty` | Nix, immutable |

The content directory is created and its `config.toml` seeded via
`systemd.tmpfiles.rules` using the `C` (copy-if-target-absent) directive. `C`
is chosen specifically because it never overwrites an existing target, which is
exactly the Analyst's seed-if-absent requirement and survives every rebuild
without re-assertion.

## Decisions

### D1 — Channel: pin `nixpkgs` to the current stable release, not unstable

`flake.lock` pins `nixos-26.05`. Stable release channels have complete Hydra
coverage for `aarch64-linux`, which is what keeps the closure downloadable
rather than buildable — the difference between minutes and hours on this board.
It also dissolves the spike's ffmpeg 8.1.2-vs-9.0 question outright (see D9).
Updating is a deliberate `nix flake update`, reviewed and rolled back like any
other change.

### D2 — Kernel: stock mainline `aarch64`, and **do not** import `nixos-hardware`

The spike confirmed `nixos-hardware`'s `raspberry-pi-3` profile pins a kernel
that is **not** in the binary cache (Hydra builds nixpkgs, not nixos-hardware),
forcing a full on-device kernel compile that the NixOS wiki warns can exhaust
the board's RAM. The stock cached kernel is used. This is recorded in the host
config as a comment with the reason, because it is the kind of "helpful" import
someone adds later in good faith.

### D3 — Boot: `generic-extlinux-compatible` + Raspberry Pi firmware, via the stock upstream SD image

The verified-available chain is firmware → U-Boot 2026.04 (Pi 3, 64-bit) →
extlinux → NixOS generation. `boot.loader.grub.enable = false`,
`boot.loader.generic-extlinux-compatible.enable = true`, with
`configurationLimit` set (10) so generations do not fill a small card.

`boot.loader.generic-extlinux-compatible.configurationLimit` interacts with
rollback: it is the *only* mechanism by which a non-booting generation can be
escaped, and per the spike that menu requires a keyboard physically at the sign.
See D7.

### D4 — Provisioning: stock upstream SD image, then one on-device `nixos-rebuild boot`

Considered and rejected:

- **Build a custom SD image off-device.** This is the fastest path — it writes a
  populated `/nix/store` at card-reader speed instead of at 0.4 MB/s — but
  `Analyst.md` lists building bootable SD images as an explicit deferred
  non-goal, and it introduces `aarch64` emulation or a remote builder on the dev
  machine as new required infrastructure.
- **Remote builder / `nixos-rebuild --target-host`.** This does *not* solve the
  measured problem. The bottleneck is the SD card write, not where the bytes are
  compiled; pushing a 4 GiB closure over the LAN still lands on the same card at
  the same speed.

**Chosen:** flash the stock, upstream-published NixOS `aarch64` SD image (we
build nothing), boot it, then run the first `nixos-rebuild boot --flake …`
on-device, unattended, and reboot. This is a one-time multi-hour cost on a board
that is currently deactivated anyway. It honours the deferred non-goal, adds no
new build infrastructure, and — critically — its first step *is* the
outstanding boot verification. There is a single physical SD card (the same
card that previously ran the Debian install, now overwritten with the user's
consent); if a re-attempt is ever needed, it means re-flashing that card, not
swapping to a preserved fallback.

The one-time cost is smaller than the spike's 1.8 GiB / 4.2 GiB figure suggests:
that measurement included `ffmpeg-headless`, which is now gone (D9), and started
from bare metal rather than from a booted base system.

**Escape hatch, deliberately kept adjacent:** if the one-time rebuild proves
intolerable in practice, the custom-image path is a `nixos-generators` /
`sd-image-aarch64.nix` wrapper around the *same* `nixosModules.mural`. Nothing
in this design has to change to adopt it. That is why the module and the host
config are separate files.

### D5 — Session: **greetd**, and neither `displayManager.autoLogin` nor a bare getty hook

The brief asked for `services.displayManager.autoLogin` or greetd. Three
candidates were weighed:

- **`services.displayManager.autoLogin` (+ LightDM/SDDM).** Rejected. It drags a
  display manager and a greeter toolkit into the closure of a machine that will
  never render a greeter. On a board where closure bytes are paid for at 0.4
  MB/s, this is the most expensive option for the least benefit.
- **`services.getty.autologinUser` + a shell-profile `startx`.** The literal
  translation of today's `install.sh`. Lightest possible, and it yields a real
  `logind` session for free. Rejected because it reinstates a shell-hook restart
  loop as the supervision mechanism, and because the session's lifecycle then
  lives in a login shell rather than under a supervisor.
- **greetd with `initial_session`.** **Chosen.** It is a login daemon, so it
  creates a genuine `logind` seat session — which matters more than it appears:
  X acquiring DRM master, opening input devices, and `uaccess` ACLs on device
  nodes all depend on it. It carries no greeter UI in this configuration. It is
  itself a systemd service, so the outer supervision is systemd's. And swapping
  the presentation layer later is one line: `initial_session.command`.

`initial_session` and `default_session` are set to the **same** store-resident
session script, so that a session exit relaunches the kiosk rather than falling
through to a greeter.

The 30-second guarded restart from `install.sh`'s `tty1-guard.sh` is preserved
in that script: it `exec`s `xinit`, and on exit prints the operator banner and
sleeps `restartDelay` before returning, which paces greetd's relaunch. No
systemd start-rate limit is applied (`StartLimitIntervalSec = 0`), because the
Analyst requires both "must not become a tight crash loop" (the 30 s sleep
handles this) and "must not leave a permanently black screen" (a start limit
would eventually produce exactly that).

`services.getty.autologinUser` is *also* set, on **tty1**, with X on its own VT.
An operator physically at the sign always has a console, which is the other half
of the "no path to a console" edge case and is required anyway for the hard
rollback in D7.

### D6 — CEC is a module option, not a wrapper on the binary

`Analyst.md` flags a real tension: pinning tools onto the player's `PATH` is the
natural way to guarantee the dependency, and it makes the graceful-degradation
paths unreachable and the kill switch impossible.

The resolution is to **not wrap the Mural binary**. The derivation stays pure and
unwrapped. `services.mural.cec.enable` (default `true`) is what puts `libcec` on
the *session's* environment. Setting it to `false` produces a sign where
`exec.LookPath("cec-client")` fails and `cec.go` logs its no-op line —
"deploy without CEC" becomes a supported, declarative, one-line configuration
rather than an accident of an incomplete install. This is precisely the
transition `Delta.md` describes: degradation stops being a safety net and
becomes a choice.

**Device-node access is handled explicitly and not left to `uaccess`.** The
kiosk user joins the `video` group and a udev rule sets group ownership on
`/dev/cec*`. The Analyst names this as "the likeliest reason for CEC to look
correctly configured and still do nothing," and it is *more* likely under a
supervised session than under the Debian autologin it replaces. Doing it
explicitly makes it independent of whether a `logind` session happened to grant
an ACL.

### D7 — Rollback is two mechanisms, and the docs must not conflate them

| Failure | Mechanism | Requires |
|---------|-----------|----------|
| New generation boots but the sign is broken | `nixos-rebuild switch --rollback` | SSH only — **remotely triggerable** |
| New generation does not boot | U-Boot extlinux menu, select prior generation | **A keyboard physically at the sign** |

The spike established the second: the rollback safety net is real but not
remote. `Analyst.md` requires that the previous working state be recoverable
"without network access, a rebuild, or a re-image" — the boot menu satisfies
that literally, and the documentation must say plainly that it costs a trip to
the sign. Claiming remote rollback in general would be false.

`system.autoUpgrade` is **deliberately not enabled**. On a single sign with a
12-minute rebuild and a physical-presence hard rollback, an unattended upgrade
is a mechanism for bricking the deployment from a distance.

### D8 — Samba: declarative share, stateful credential, named as such

`services.samba` with `guest ok = no`, `map to guest = never`, `valid users`,
and `force user` — full parity with the current `smb.conf` stanza, with `map to
guest = never` added as belt-and-braces against the Analyst's hard requirement
that anonymous access be unreachable. `nmbd` stays enabled for parity with
today's `systemctl restart smbd nmbd`.

The whole block is gated behind `services.mural.share.enable`, default `false`,
because the Analyst requires that a sign without sharing have no share *and no
file-sharing service* present.

**The password is not reproducible and is documented, not hidden.**
`smbpasswd -a <user>` remains a one-time manual step, listed alongside the Wi-Fi
PSK in a single "residual state" section of the install doc, so the honest total
is one list rather than a footnote per feature.

### D9 — `ffmpeg` is removed from this design entirely, and the kill-switch promise is withdrawn with the feature it guarded

`Analyst.md` treats the `ffmpeg` variant choice and the `sudo apt remove ffmpeg`
kill switch as open questions that must be answered. Both are **superseded by a
change outside this spec**: video playback was removed from the Go codebase
after the spike, and Mural no longer invokes `ffmpeg` or `ffprobe` at all.

- The variant question (`ffmpeg` vs `-headless` vs `-full`) is moot. No ffmpeg
  is packaged.
- The kill switch is withdrawn because **the capability it guarded no longer
  exists** — not because NixOS made it impossible. This is the honest closure of
  that open question; the rewritten `docs/INSTALL.md` drops the video
  troubleshooting section for the same reason.
- The Analyst behaviour "`ffmpeg`, `ffprobe`, and `cec-client` must be
  discoverable on `PATH`" reduces to `cec-client` alone.

If a future Kodi-launcher mode reintroduces video, it needs its own kill switch
designed at that time. This is flagged, not built.

### D10 — Package: `buildGoModule` over a filtered source

CGo is required (Fyne). `nativeBuildInputs = [ pkg-config ]`; `buildInputs =
[ libGL xorg.libX11 libXrandr libXinerama libXcursor libXi libXxf86vm ]`,
matching the set CI already links against.

`src` is built with `lib.fileset` restricted to `*.go`, `go.mod`, `go.sum`. This
is not tidiness: an unfiltered `src = ./.` makes every documentation commit
invalidate the derivation and cost 12.5 minutes of on-device rebuild.

`hardware.graphics.enable = true` is asserted by the module. The spike found
nixpkgs' Mesa hardcodes `/run/opengl-driver/lib`, which only exists when that
flag is set, and its absence produces a "failed to open dri" error that reads as
a driver bug. The module sets it and adds an assertion so it cannot be silently
overridden.

### D11 — Secrets stay off the public repository

The Mural repository is public. The host configuration lives in it — that is the
point of a single declarative artifact — but the Wi-Fi PSK and the Samba
password do not. Wi-Fi uses `networking.wireless.secretsFile` pointing at
`/etc/mural/wifi.env`, written once on the device. Everything else about the
sign (hostname, timezone, SSH authorised public keys, share definition, schedule
seed) is public-safe and committed.

Secrets *management* (agenix, sops-nix) is out of scope — one sign, two secrets,
both written once.

### D12 — CI: replace the Linux release matrix with a Nix build check

`Analyst.md` leaves the disposition of `.github/workflows/build.yaml` to this
phase. Inspection confirms the workflow has **no Windows job today**; its only
output is Linux binaries, which `Delta.md` establishes have lost their last
supported consumer.

Chosen: **keep the workflow, delete the Linux cross-compile matrix and the
release publication, and replace them with `nix build .#mural`** on both an
`x86_64` and an `aarch64` runner (GitHub's `ubuntu-24.04-arm` runners make the
latter cheap). Rationale: deleting the workflow outright would lose the only
automated evidence that Mural still builds; keeping the binaries would keep
publishing artifacts nobody may install. A Nix build check is the thing that
actually protects the new deployment path, and an `aarch64` check catches
target-architecture breakage without touching the board.

A binary cache (Cachix/attic) fed by that CI job would cut on-device updates from
~12.5 minutes to roughly the download time, and is the single highest-leverage
follow-up available. It is **deferred**: it needs an account, a signing key, and
a token on the device, none of which this feature requires.

### D13 — Forward compatibility with `specs/drm-renderer/`

`drm-renderer` (Phase 1 complete, gate READY, not yet designed) intends to drop
Fyne, X11, `ratpoison`, `unclutter`, and CGo. This design does not anticipate it
and does not wait for it, but it deliberately avoids making it expensive:

- The X11 session is one file (`nix/modules/kiosk-x11.nix`) selected by one
  option (`services.mural.session`). Its removal is a file deletion and an enum
  value, not a refactor.
- `session = "none"` already exists as a working value, so the core module is
  proven not to depend on X.
- The GL/X11 `buildInputs` live in `nix/package.nix` and nowhere else; a pure-Go
  Mural deletes that list and gains `CGO_ENABLED = 0`.
- greetd was chosen partly because a DRM/KMS session needs a `logind` seat
  session even more than an X session does.

**Sequencing note for the operator, not a design change:** both specs will
rewrite the same operator-facing install documentation. Whichever lands second
inherits the other's docs. If `drm-renderer` is likely to land soon, the cheapest
order is this spec first (it establishes the platform) and `drm-renderer`
second (it simplifies the session it finds).

## Files to Create

| File | Purpose |
|------|---------|
| `flake.nix` | Inputs (pinned nixpkgs), `packages`, `overlays.default`, `nixosModules.mural`, `nixosConfigurations.sign`, `checks`, `devShells` |
| `nix/package.nix` | `buildGoModule` derivation, filtered source, GL/X11 `buildInputs` |
| `nix/modules/mural.nix` | `services.mural.*` — user, content dir + seed, CEC, Samba share, graphics assertion |
| `nix/modules/kiosk-x11.nix` | greetd, X on its own VT, ratpoison, unclutter, guarded relaunch |
| `nix/session/xinitrc.sh` | Store-resident session script (replaces `~/.xinitrc` + `tty1-guard.sh`) |
| `nix/session/ratpoisonrc` | Store-resident (replaces `~/.ratpoisonrc`) |
| `nix/config.sample.toml` | Seed `config.toml`, lifted verbatim from `install.sh` step 4 |
| `nix/hosts/sign/default.nix` | The one deployed Pi 3B+ — networking, users, SSH, enables `services.mural` |
| `nix/hosts/sign/boot.nix` | extlinux, Pi firmware, stock kernel, `configurationLimit`, the no-`nixos-hardware` rationale |
| `nix/tests/kiosk.nix` | nixosTest: session starts, Mural process running, survives a simulated crash |
| `nix/tests/content.nix` | nixosTest: seed-if-absent, operator edit survives a rebuild |
| `nix/tests/share.nix` | nixosTest: authenticated access works, **anonymous access refused** |
| `docs/MIGRATION.md` | Disclosure of what happens to the existing Debian sign |

## Files to Modify

| File | Change |
|------|--------|
| `docs/INSTALL.md` | Rewritten for NixOS end to end. Loses the Pi OS Imager journey, `startx`, `curl \| bash`, re-running the installer, and the ffmpeg troubleshooting section. Gains the SD flash, first rebuild, residual-state list, update/rollback, and a pointer to `MIGRATION.md` |
| `README.md` | Quick-install section rewritten; `Prerequisites` gains `nix develop`; the pre-built-binary claim removed |
| `CLAUDE.md` | `Deployment` section rewritten; `Project Structure` gains `flake.nix` / `nix/` and loses `install.sh` |
| `.github/workflows/build.yaml` | Linux cross-compile matrix and release publication removed; `nix build .#mural` checks on `x86_64` and `aarch64` runners added |
| `.gitignore` | Add `result`, `result-*` |

## Files to Delete

| File | Reason |
|------|--------|
| `install.sh` | Retired outright per `Delta.md` — not deprecated, not guarded, not taught to refuse. Leaving it is leaving a trap |

## Dependencies Between Steps

- **Step 2 is a hardware gate.** Every step in the *Hardware bring-up* layer
  depends on it. If it fails, the replacement decision returns to the user
  (`Analyst.md`) rather than being designed around.
- Steps 3–27 (packaging, module, host config, tests) are **desk work and are not
  gated on step 2.** This is deliberate and follows `spike-findings.md`'s own
  recommendation: a failed boot test invalidates specific packaging choices, not
  the design work.
- Step 5 (`vendorHash`) depends on step 3 (the derivation must exist to fail
  informatively).
- Steps 7–11 (core module) depend on step 4 (`flake.nix` must expose the module).
- Steps 12–15 (session) depend on step 7 (the `session` option must exist).
- Steps 16–17 (Samba) depend on step 7 (the `share.*` options must exist).
- Steps 18–23 (host config) depend on the core module and the session module.
- Steps 24–27 (nixosTests) depend on step 23 (the config must evaluate).
- Steps 28–36 (hardware) depend on step 2 **and** on step 23.
- Steps 39–45 (docs) depend on the hardware track for their *numbers* — measured
  first-provision and update times, and whether CEC works on the real display —
  and must not be finalised on estimates.

## Parallelisation Opportunities

Safe to run concurrently:

- **Step 1 (buy an SD card) runs alongside everything.** It is the sole physical
  prerequisite and has a lead time.
- Steps 12–15 (session module) and steps 16–17 (Samba) are independent — different
  files, both depending only on step 7.
- Steps 24, 25, 26 (three nixosTests) are three independent files.
- Steps 37–38 (CI) are independent of the whole Nix track.
- Step 41 (`docs/MIGRATION.md`) is independent of everything — it documents the
  Debian gap and needs no measurement.

**File locks — these cannot be parallelised:**

- `flake.nix` is touched by steps 4, 22, and 27. Serialise them.
- `nix/modules/mural.nix` is touched by steps 7, 8, 9, 10, 11, 16, and 17.
  Serialise the whole run.
- `docs/INSTALL.md` is touched by steps 40, 42, and 43. Serialise.
- `CLAUDE.md` and `README.md` are each single-step.

## Risks & Open Questions

Ordered by what Phase 3 should attack hardest.

1. **The boot criterion is still unverified, and there is no fallback.** The
   card has already been flashed with NixOS and checksum-verified, but booting
   it on the real Pi 3B+ has not yet happened — the board has not been
   accessible. This is the feature's principal risk. The design mitigates the
   *cost* of being wrong (the card can simply be re-flashed and retried; desk
   work survives a failure) but cannot mitigate the risk itself. There is no
   surviving Debian install to fall back to — the user was explicit that its
   loss is not a concern — so a failed boot test returns the replacement
   decision to the user rather than falling back to the old install. The sign
   is currently deactivated, so this may sit open for a while.

2. **D4's one-time provisioning cost is estimated, not measured.** The spike
   measured 0.3–0.4 MB/s and a 4.2 GiB unpacked closure, giving "several hours."
   The real number here is smaller (no ffmpeg, starting from a booted base) but
   nobody has measured it. If it turns out to be, say, eight hours rather than
   three, the custom-SD-image path becomes the right answer and the deferred
   non-goal should be revisited with the user. **This is the decision most likely
   to be overturned by evidence**, which is why the escape hatch is kept adjacent.

3. **Running X under greetd may not behave identically to running it under a
   login shell.** D5 argues greetd yields a real `logind` seat session, which
   should make DRM master, input device access, and `uaccess` behave as they do
   today. This is reasoned from how greetd works, **not observed on this board.**
   The udev rule in D6 deliberately removes CEC from depending on it, but X
   itself still does. First hardware task after boot should be "does X actually
   come up under greetd", before anything is built on top.

4. **NixOS option names have churned in this area.** `services.xserver.displayManager.autoLogin`
   → `services.displayManager.autoLogin`, and `services.samba.shares` →
   `services.samba.settings`, both moved recently. Every option named in this
   document must be verified against the pinned channel rather than trusted.
   A wrong option name is a silent no-op in some cases, which is worse than an
   error.

5. **The nixosTests run on `x86_64` and prove module logic, not hardware
   support.** `Analyst.md` is explicit that x86-64 verification does not
   discharge this feature. The tests must be described in the docs as what they
   are, or they will be mistaken for a support claim.

6. **`specs/drm-renderer/` will delete a substantial part of what this builds.**
   D13 keeps that cheap, but "cheap" is a judgement, and the two specs racing
   each other to rewrite `docs/INSTALL.md` is a real coordination cost that no
   design decision here removes.

7. **`services.mural.session = "none"` is a contract with no consumer yet.** It
   exists for forward compatibility and will be exercised only by a nixosTest.
   An untested-in-anger extension point can be the wrong shape.

8. **The `admin`/`mural` user split is new.** Today one user does everything. The
   spike found remote access to the box had already rotted (Tailscale logged out,
   SSH key no longer authenticating, LAN IP drifted). Declarative
   `authorized keys` fixes the key rot, but an admin user who cannot log in is a
   sign that needs a physical visit. Worth a critic's attention on the ordering
   of steps 21 and 28: do not switch away from a working access path before the
   new one is proven.

## Architect Checklist

- [x] Approach fits existing project patterns — the flake mirrors the existing
      `install.sh` responsibilities one for one, and the seed `config.toml` is
      lifted verbatim rather than reinvented
- [x] Contract defined before any code — the module option table above is the
      contract, and every later step binds against it
- [x] Schema changes identified — none; the on-disk state layout change is
      tabulated instead, including which rows are *not* declarative
- [x] Auth and ownership checks included — unprivileged kiosk user, no `wheel`,
      explicit CEC group + udev rule, Samba `guest ok = no` + `map to guest =
      never`, SSH key-only with root login disabled
- [x] No step requires modifying multiple unrelated files
- [x] Parallelisation opportunities and file locks declared
- [x] Notification/event needs considered — none; nothing here mutates live
      shared state. The nearest equivalent is the rebuild/rollback pair, covered
      in D7
- [x] The still-open boot criterion is handled procedurally — step 2 is an
      explicit gate, the escalation path on failure is named, and the desk-work
      track is explicitly declared not to depend on it
