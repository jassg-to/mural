# Tasks: NixOS Deployment Support

> Phase 2 output — implementation checklist.
> Each step is atomic: one file created, one option added, one thing verified.
> Rationale, dependencies, and file locks are in `Architect.md`.

> **Two tracks.** Layers A and H are physical work on the board. Layers B–F and
> I are desk work and are **not** gated on the hardware. Only Layer G is.
> This follows `spike-findings.md`: a failed boot test invalidates specific
> packaging choices, not the design.

---

### Layer A: Prerequisites and the hardware gate

- [x] Step 1: Flash the stock upstream NixOS `aarch64` SD image to the sign's
      SD card and verify the write via a full read-back checksum. This is the
      project's single physical SD card — the same card that previously ran
      the Debian install. There is no spare card; the user has confirmed she
      never intended to preserve the previous Debian install and is fine with
      it being gone. **Done: image flashed from the dev machine and
      checksum-verified.**
- [x] Step 2: **HARDWARE GATE.** Insert the flashed SD card into the Pi 3B+,
      power on, and confirm it reaches a usable console on the HDMI output and
      obtains network. Done when a shell prompt is reached on the real board.
      **Done: board reachable at `nixos@192.168.2.27` over wlan0/DHCP;
      `getty@tty1` active with a shell session; HDMI console confirmed visible
      by the user (login prompt/shell, not blank). Raspberry Pi 3 Model B Plus
      Rev 1.3, aarch64-linux, stock NixOS 26.05 installer image. Non-fatal boot
      noise observed (vc4-drm DDC/i2c warning, Bluetooth reset timeouts) —
      doesn't block the gate.**
      Every step in Layer G depends on this. Nothing else does.

### Layer B: Nix packaging

- [x] Step 3: Create `nix/package.nix` — a `buildGoModule` derivation for Mural.
      `nativeBuildInputs = [ pkg-config ]`; `buildInputs` covering `libGL`,
      `xorg.libX11`, `libXrandr`, `libXinerama`, `libXcursor`, `libXi`,
      `libXxf86vm`. `src` built with `lib.fileset` restricted to `*.go`,
      `go.mod`, `go.sum` so documentation commits do not invalidate the
      derivation. Leave `vendorHash` as `lib.fakeHash` for now.
- [x] Step 4: Create `flake.nix` — pin `nixpkgs` to the current stable release
      channel (D1, **not** unstable), expose `packages.<system>.mural` and
      `.default` via `nix/package.nix`, and `overlays.default`. Add `result` and
      `result-*` to `.gitignore` in the same step.
- [x] Step 5: Resolve the real `vendorHash` from the build failure and commit it.
      Done when `nix build .#mural` succeeds on the `x86_64` dev machine and the
      resulting binary runs.
      **Done: `nix build .#mural` succeeds; `ldd` resolves all GL/X11 libs from
      the Nix store; `result/bin/mural --help` runs and prints usage.**
- [x] Step 6: Add `devShells.<system>.default` to `flake.nix` — Go, `gcc`,
      `pkg-config`, and the same GL/X11 headers — so `nix develop` reproduces the
      dev loop that `mise.toml` currently approximates. Done when `nix develop -c
      go test ./...` passes.
      **Done: `nix develop -c go test ./...` → `ok github.com/jassg-to/mural`.**

### Layer C: NixOS module — core

> **File lock:** steps 7–11, 16, and 17 all edit `nix/modules/mural.nix`.
> Run them strictly in sequence.

- [x] Step 7: Create `nix/modules/mural.nix` with the full option skeleton from
      `Architect.md`'s option contract table — `enable`, `package`, `user`,
      `contentDir`, `seedConfig`, `session` (enum `"x11"` / `"none"`),
      `restartDelay`, `cec.enable`, `share.*` — with types, defaults, and
      descriptions, and no implementation yet. Done when the module evaluates
      with `enable = false`.
      **Done: verified via `lib.evalModules` with `enable = false` →
      evaluates to `false` cleanly.**
- [x] Step 8: Implement the kiosk user in `nix/modules/mural.nix` —
      `isNormalUser`, **not** in `wheel`, no password, `extraGroups = [ "video" ]`.
      Done when a config with `services.mural.enable = true` evaluates and the
      user appears in `users.users`.
      **Done: eval'd with `services.mural.enable = true` →
      `users.users.mural = { isNormalUser = true; extraGroups = ["video"];
      hashedPassword = null; }`, not in `wheel`.**
- [x] Step 9: Implement content-directory provisioning. Add
      `nix/config.sample.toml`, lifted verbatim from `install.sh` step 4, and
      wire it through `systemd.tmpfiles.rules` using `d` for the directories and
      **`C`** (copy-if-target-absent) for `config.toml`. `C` is required, not
      incidental: it must never overwrite an operator's file on any rebuild.
      **Done: eval'd rules include `d /var/lib/mural/content 0755 mural
      users -` and `C .../config.toml 0644 mural users - <store path to
      config.sample.toml>`.**
- [x] Step 10: Implement CEC support. When `cec.enable`, add `libcec` to the
      session environment — **do not wrap the Mural binary** (D6) — and add a
      udev rule granting the `video` group access to `/dev/cec*`. When `false`,
      add nothing, so `cec.go` takes its documented no-op path.
      **Done: verified both directions via eval — `cec.enable = true` adds
      `libcec` to `environment.systemPackages` and a `cec[0-9]*` udev rule;
      `cec.enable = false` adds neither. `cfg.package` untouched (no wrapping).**
- [x] Step 11: Set `hardware.graphics.enable = true` and add an assertion that
      fails the build if it is disabled while `services.mural.enable` is true.
      This is the spike's `/run/opengl-driver/lib` trap; without it the failure
      presents as a driver bug rather than a missing flag.
      **Done: `hardware.graphics.enable = mkDefault true` (default case passes);
      an explicit override to `false` elsewhere trips the assertion with the
      "failed to open dri" explanation, verified via eval in both directions.**

### Layer D: NixOS module — kiosk session

- [x] Step 12: Create `nix/modules/kiosk-x11.nix`, applied when
      `services.mural.session = "x11"`. Configure `services.greetd` with
      `initial_session` **and** `default_session` both pointing at the same
      session script, so a session exit relaunches the kiosk instead of falling
      through to a greeter.
      **Done: eval confirms `initial_session.command == default_session.command`
      (same store path), `user = "mural"`. Real script body deferred to Step 14
      pending Step 13's session files — currently a placeholder.**
      **⚠ Found during research: the pinned nixpkgs (26.05) has removed
      `services.greetd.vt` — greetd's VT is hardcoded to VT1 upstream
      (`lib.mkRemovedOptionModule ["services" "greetd" "vt"] "The VT is now
      fixed to VT1."`). This conflicts with D5/Step 15's plan to run the kiosk
      on its own VT while `tty1` stays a free operator console — greetd now
      always claims tty1. Will resolve at Step 15 by putting the operator
      autologin console on tty2 instead; flagging now since it affects D5's
      documented rationale.**
- [x] Step 13: Create `nix/session/xinitrc.sh` and `nix/session/ratpoisonrc` as
      store-resident scripts, replacing `~/.xinitrc` and `~/.ratpoisonrc`.
      Preserve `xset s off`, `xset -dpms`, `xset s noblank`, `ratpoison`,
      `unclutter -idle 0 -root`, and launching Mural with the content directory
      as its positional argument. **No file is written into `$HOME`.**
      **Done: both files created (`ratpoisonrc` = `set border 0`, matching the
      original). `xinitrc.sh` takes `$MURAL_BIN`/`$MURAL_CONTENT_DIR`/
      `$MURAL_RATPOISONRC` as env vars from the wrapper (wired in Step 14) so
      the script itself stays static/reviewable rather than Nix-templated.
      `sh -n` syntax-clean; `chmod +x` set.**
- [x] Step 14: Implement the guarded relaunch in the session script — on session
      exit, print the operator banner and sleep `restartDelay` (default 30 s)
      before returning. Set `StartLimitIntervalSec = 0` on the greetd unit so a
      repeatedly-crashing sign keeps retrying rather than giving up into a
      permanent black screen.
      **Done: built the generated script and inspected it directly — `xinit`
      launches `xinitrc.sh` on `vt1` with `MURAL_BIN`/`MURAL_CONTENT_DIR`/
      `MURAL_RATPOISONRC` exported, then the banner prints and `sleep 30` runs.
      `systemd.services.greetd.serviceConfig.StartLimitIntervalSec = 0`
      confirmed via eval. Banner wording updated from the original
      "Ctrl+C to enter the system shell" to "Switch to VT2" — Ctrl+C no longer
      drops into a shell under greetd's session model, and per the Step 12 VT1
      finding, the operator console moves to VT2 (see Step 15).**
- [x] Step 15: Set `services.getty.autologinUser` on tty1 and place X on its own
      VT, so an operator physically at the sign always has a console. This is
      also the prerequisite for the hard rollback in step 34.
      **Deviated per the Step 12 finding: autologin console is on tty2, not
      tty1 (greetd owns tty1 unconditionally on this nixpkgs). Verified via
      eval: `autologinUser = "mural"`, `autovt@tty1.enable = false` (no
      conflict with greetd), `greetd terminal.vt = 1`. getty@tty2 starts
      on-demand on VT switch (Ctrl+Alt+F2) — not forced at boot, which is
      correct: the operator only needs it when physically present.**

### Layer E: NixOS module — file sharing

- [x] Step 16: Implement the Samba share in `nix/modules/mural.nix`, gated on
      `services.mural.share.enable` (default `false`). Set `path`, `browseable`,
      `read only = no`, `guest ok = no`, `map to guest = never`, `force user`,
      and `valid users`. When disabled, **no share and no Samba service may be
      present at all.** Verify the option path against the pinned channel —
      `services.samba.shares` was renamed to `services.samba.settings`.
      **Done: confirmed `services.samba.settings` is current on the pinned
      channel (the module source itself renames `shares`→`settings`). Verified
      both directions via eval — `share.enable = true` produces
      `services.samba.enable = true` and the full share stanza; `share.enable
      = false` (default) leaves `services.samba.enable = false`.**
- [x] Step 17: Wire `share.openFirewall` to the SMB ports, and keep `nmbd`
      enabled for parity with today's `systemctl restart smbd nmbd`.
      **Done: verified via eval — `openFirewall = true` (default) opens TCP
      139/445; `openFirewall = false` opens neither. `nmbd.enable = true`
      confirmed explicit.**

### Layer F: Host configuration for the one sign

- [x] Step 18: Create `nix/hosts/sign/default.nix` — hostname, timezone, locale,
      state version, and `services.mural` enabled with `share.enable = true`.
      **Done: `hostName = "sign"` (matches `nixosConfigurations.sign`),
      `time.timeZone = "America/Toronto"`, `i18n.defaultLocale = "en_CA.UTF-8"`
      (inherited from the dev machine as a reasonable default for a
      single co-located sign), `stateVersion = "26.05"`. Verified via eval on
      `aarch64-linux` with an overlay providing `pkgs.mural`.**
- [x] Step 19: Create `nix/hosts/sign/boot.nix` — `boot.loader.grub.enable =
      false`, `generic-extlinux-compatible.enable = true`,
      `configurationLimit = 10`, Raspberry Pi firmware, and
      `hardware.enableRedistributableFirmware = true`. Include an inline comment
      recording **why `nixos-hardware`'s `raspberry-pi-3` profile must not be
      imported** — its kernel is not in the binary cache and forces an on-device
      kernel compile that can exhaust the board's RAM.
      **Done: verified via eval — `grub.enable = false`, `extlinux.enable =
      true`, `configurationLimit = 10`, `enableRedistributableFirmware = true`.
      Comment recorded above the boot options.**
- [x] Step 20: Configure networking — Wi-Fi via
      `networking.wireless.secretsFile` pointing at `/etc/mural/wifi.env`
      (device-local, never committed). The repository is public; the PSK does
      not go in it.
      **Done: `networking.wireless.enable = true`, `secretsFile =
      "/etc/mural/wifi.env"`, `networks."REDACTED".pskRaw = "ext:mural_wifi_psk"`
      (SSID confirmed from the live board via `iwgetid -r`). Uses
      wpa_supplicant's `ext:` substitution so the PSK never touches the Nix
      store. Verified via eval; confirmed no NetworkManager conflict
      (`networkmanager.enable = false`).**
- [x] Step 21: Configure `services.openssh` — key-only authentication, root login
      disabled — and add a separate `admin` user in `wheel` with declarative
      `openssh.authorizedKeys.keys`. This is the update path's foundation and it
      directly addresses the SSH key rot the spike found on the box.
      **Done: `PasswordAuthentication = false`, `PermitRootLogin = "no"`,
      `admin` user in `wheel` with the dev machine's current key
      (`~/.ssh/id_ed25519.pub`, `kiva+2026@piranga` — already the key
      authorized on the live board's stock `nixos` user, confirmed via SSH).
      Verified via eval.**
- [x] Step 22: Add `nixosConfigurations.sign` (system `aarch64-linux`) to
      `flake.nix`, composing `nixosModules.mural`, the kiosk module, and
      `nix/hosts/sign/`. **File lock: `flake.nix`.**
      **Done: added `nixosModules.mural`/`nixosModules.kiosk-x11` outputs (not
      an explicit earlier step, but implied by Architect.md's files table) and
      `nixosConfigurations.sign` composing them with `nix/hosts/sign/{default,
      boot}.nix` and `overlays.default`. Verified via
      `nix eval .#nixosConfigurations.sign.config.networking.hostName` →
      `"sign"`.**
- [x] Step 23: Evaluate the full `aarch64` configuration from the dev machine
      (`nix eval .#nixosConfigurations.sign.config.system.build.toplevel.drvPath`)
      and fix any evaluation errors. Done when it evaluates clean. The spike's
      reference point is ~7 seconds and ~5,184 derivations.
      **Fixed one evaluation error to get here: `fileSystems` had no root
      declared (not covered by an earlier step). Added `fileSystems."/"`
      (`NIXOS_SD` ext4) and `fileSystems."/boot/firmware"` (`FIRMWARE` vfat) to
      `boot.nix`, matching the stock image's own partition labels confirmed
      via `lsblk -f` on the live board. Result: evaluates clean in 7.2s,
      4,399 derivations — matches the spike's reference closely.**

### Layer G: Automated tests

> These run on `x86_64` and verify **module logic only.** Per `Analyst.md`,
> x86-64 verification does not discharge this feature's hardware claim, and the
> documentation must not present these as evidence of Pi support.

- [x] Step 24: Create `nix/tests/kiosk.nix` — a `nixosTest` asserting greetd
      starts, the session comes up, and a Mural process is running. Include a
      case that kills Mural and asserts the session relaunches after the guard
      delay rather than dying permanently.
      **Done, after real debugging (see below): `nix build` on the test
      derivation passes — greetd starts, mural (PID captured), kill -9,
      confirmed dead, guard cycle observed in the log (~5s
      restartDelay), new mural PID confirmed distinct from the old one.**
      **Found along the way (not bugs in the shipped modules — all in the
      test fixture itself):**
      **1. Mural exits immediately with "no images found" against an empty
      content dir (pre-existing, documented Mural behavior) — the test needed
      a seeded image. A 1x1 pixel image was tried first and also worked once
      swapped for a realistic 640x480 one; the 1x1 wasn't actually the
      problem.**
      **2. `pgrep -f 'bin/mural'` self-matched my own retry-loop's shell
      command text (which literally contains the string "bin/mural"),
      returning two PIDs. Fixed by switching to `pgrep -x mural` (exact
      process-name match).**
      **3. Confirmed via a redirected-output debug run that the full chain —
      greetd → xinit → Xorg → ratpoison + unclutter (backgrounded) → mural
      (exec'd, foreground) — works correctly, including CEC's documented
      graceful no-op (`cec-client "standby 0": exit status 1, autodetect
      FAILED`) with no CEC hardware present.**
- [x] Step 25: Create `nix/tests/content.nix` — a `nixosTest` asserting the
      content directory is created, `config.toml` is seeded when absent, is
      **not** overwritten when present, and that an operator edit survives a
      subsequent activation.
      **Done: `nix build` on the test passes cleanly on the first attempt —
      dir created, config.toml seeded with `[slideshow]`, an appended
      "# operator edit" line survives a `systemd-tmpfiles --create` re-run
      (simulating a subsequent activation).**
- [x] Step 26: Create `nix/tests/share.nix` — a `nixosTest` asserting an
      authenticated user can write to the share and that **anonymous/guest
      access is refused.** The anonymous refusal is a hard requirement from
      `Analyst.md`, not a nicety.
      **Done: `nix build` on the test passes. Fixed one bug along the way —
      `smbpasswd -a -s` reads password + confirmation as two stdin lines, not
      one; a single `echo` failed with "Unable to get new password," fixed
      with `printf 'testpass\ntestpass\n'`. Authenticated write via smbclient
      succeeds; anonymous (`-N`) access is refused as required.**
- [x] Step 27: Expose all three tests as `checks.<system>` in `flake.nix`. Done
      when `nix flake check` passes. **File lock: `flake.nix`.**
      **Done: `checks.x86_64-linux` only (per Layer G's own note — these
      verify module logic, not the aarch64/Pi hardware claim). `nix flake
      check` → "all checks passed!" in 54s.**

### Layer H: Hardware bring-up — blocked on Step 2

- [ ] Step 28: First provisioning. From the booted stock image on the sign's SD
      card, run `nixos-rebuild boot --flake .#sign` on-device, unattended, then
      reboot.
      **Record the wall-clock time** — `Architect.md` D4 chose this path over a
      custom SD image on an estimate, and this measurement is what confirms or
      overturns that choice. If it is materially worse than expected, escalate
      before continuing.
- [ ] Step 29: Verify the display on the real HDMI output — X comes up under
      greetd, at the sign's resolution, on real VC4 hardware rather than the
      `llvmpipe` software fallback, and Mural renders. This is the first
      confirmation that the greetd session model works on this board (risk 3).
- [ ] Step 30: Verify CEC against the real display — that the kiosk user can open
      `/dev/cec0` and that a power-on command has an effect. **Failure here does
      not block the feature** (CEC is best-effort per `Analyst.md`); record the
      result either way.
- [ ] Step 31: Verify the Samba share from another machine on the network —
      authenticated write succeeds, anonymous connection is refused. Perform the
      one-time `smbpasswd -a` as part of this step and record it as a residual
      manual step for step 43.
- [ ] Step 32: Verify the update path — make a trivial Mural source change,
      rebuild on-device, and confirm the sign picks it up. **Record the
      wall-clock time**; the spike's reference is ~12m32s. This number goes in
      the documentation.
- [ ] Step 33: Verify soft rollback — `nixos-rebuild switch --rollback` over SSH
      restores the prior generation without physical access.
- [ ] Step 34: Verify hard rollback — a prior generation is selectable from the
      U-Boot extlinux menu with a keyboard at the sign. Confirm the physical-
      presence requirement so the documentation states it accurately rather than
      implying rollback is always remote.
- [ ] Step 35: Verify power-loss resilience — cut power mid-rebuild and confirm
      the board boots into a working generation. `Analyst.md` calls for this to
      be verified on real SD-card hardware rather than assumed.
- [ ] Step 36: Sustained-operation soak — run the sign on real content through at
      least one full scheduled on/off cycle, and compare memory and thermal
      behaviour against the Debian baseline. Record the result; it may
      legitimately narrow the supported-hardware claim without killing the
      feature.

### Layer I: CI

- [x] Step 37: Rewrite `.github/workflows/build.yaml` to run `nix build .#mural`
      on an `x86_64` runner and on an `ubuntu-24.04-arm` runner, replacing the
      cross-compilation matrix. The `aarch64` check catches target-architecture
      breakage without touching the board.
      **Done: replaced the whole cross-compile matrix with a 2-runner matrix
      (`ubuntu-24.04` / `ubuntu-24.04-arm`) running `nix build
      .#packages.<system>.mural` via `cachix/install-nix-action@v31` (verified
      current release and its `nix_path` input, and confirmed flakes are
      enabled by default in its installer script). `ubuntu-24.04-arm` is
      native arm64 hardware — no QEMU emulation — matching D12's rationale.
      Also broadened triggers from tag-push-only to push/pull_request/
      workflow_dispatch, since this is now a build check rather than a
      release pipeline. Not executed in real CI (no network/gh-hosted-runner
      access from here); the aarch64 side is unverified beyond eval — flagging
      for a first real CI run to confirm.**
- [x] Step 38: Remove the release job that publishes Linux binaries. Per
      `Delta.md` they have lost their last supported consumer, and there is no
      Windows job today for them to sit alongside.
      **Done as part of Step 37's rewrite — the old `release` job
      (`softprops/action-gh-release`) and artifact upload/download steps are
      gone entirely; the workflow no longer publishes anything.**

### Layer J: Documentation and retirement

> Steps 40, 42, and 43 all edit `docs/INSTALL.md`. **File lock — serialise.**
> Steps 40, 42, 43, and 44 depend on the measured numbers from Layer H and must
> not be finalised on estimates.

- [x] Step 39: Delete `install.sh`. Retired outright — not deprecated, not
      guarded behind a distribution check, not taught to detect NixOS and refuse.
      Leaving it in the repository leaves a trap for whoever finds it.
      **Done: `git rm install.sh`.**
- [ ] Step 40: Rewrite `docs/INSTALL.md` for NixOS end to end — flashing the
      stock image, first rebuild (with the real measured time from step 28), and
      first boot. Remove the Raspberry Pi Imager journey, `startx`, the
      `curl | bash` install, re-running the installer to enable autologin, and
      the `ffmpeg` troubleshooting section (the capability it guarded no longer
      exists — see `Architect.md` D9).
- [x] Step 41: Create `docs/MIGRATION.md` — what happens to a sign installed the
      old way, in terms a non-administrator can act on: it keeps running, it
      receives no installer and no update route, and if its card dies it is
      re-provisioned onto NixOS rather than rebuilt as it was. Disclosure is a
      hard requirement; migration tooling is an explicit non-goal. **Independent
      of every other step — can run at any time.**
      **Done: plain-language doc covering all three points (keeps running, no
      update route, re-provisioned not rebuilt), links to INSTALL.md, no
      migration tooling implied or built.**
- [ ] Step 42: Add the update and rollback procedure to `docs/INSTALL.md`, in
      both directions, with the real measured time from step 32. State plainly
      that soft rollback is remote and **hard rollback requires a keyboard at the
      sign** (step 34). This is ground the project gains — there is no documented
      update procedure today at all.
- [ ] Step 43: Add a single "residual manual state" section to `docs/INSTALL.md`
      listing every step a rebuild cannot reproduce: the Samba password, the
      Wi-Fi PSK, and the content directory itself. One honest list, not a
      footnote per feature.
- [ ] Step 44: Update `README.md` — rewrite the quick-install section, remove the
      pre-built-binary claim, and point the development loop at `nix develop`.
- [x] Step 45: Update `CLAUDE.md` — rewrite the `Deployment` section, and update
      `Project Structure` to add `flake.nix` and `nix/` and remove `install.sh`.
      **Done: Project Structure lists flake.nix, nix/package.nix, both
      modules, session scripts, hosts/sign/, tests/, and the new docs;
      Deployment section rewritten for the module contract, residual state,
      the two rollback mechanisms, the VT1/tty2 console finding, and the
      retired install.sh path. No timing numbers included (those depend on
      Layer H, per steps 40/42/44's own gating note) — this file doesn't need
      operator-facing timings.**
