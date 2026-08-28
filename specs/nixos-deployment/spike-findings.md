# Spike Findings: Hardware Spike on the Real Pi 3B+

> Working notes, not an SDD phase artifact. This captures what happened
> *after* Phase 1 (see `Analyst.md` and `Delta.md`, which already settled the
> decision that NixOS fully replaces Raspberry Pi OS as the deployment
> target). This document exists so Liz can pick the project back up cold on a
> different day without re-deriving any of this.
>
> Origin: `Analyst.md`'s Gate Status section, Task 1, defined 5 pass/fail exit
> criteria for a hardware spike, blocking Phase 2 (architect) design. This
> spike was run hands-on against the real (and only) deployed Pi 3 Model B
> Plus board by another Claude session ("servers") with SSH/LAN access to the
> box, with follow-up investigation triggered by what the spike turned up.

## Spike Setup

- Only one board exists. It is running live, on a test screen (not the
  production sign location). There was no spare SD card, no card reader on
  hand, no remote power control, and no ethernet plugged in at the time of the
  spike.
- The user authorized experimentation under one rule: proceed only with
  moderate confidence of recovering remote access after any given step. Below
  that confidence level, stop and report rather than risk the only board.
- Access reality discovered mid-spike (these are pre-existing issues on the
  box, **not** caused by the spike itself):
  - Tailscale on the box was logged out — the documented
    `ssh claude@mural.deer-goanna.ts.net` path doesn't work.
  - The LAN IP had moved from `192.168.0.23` to `192.168.2.27`.
  - The `claude` automation user's SSH key no longer authenticates. Worked
    around by using the `jassg` user instead.
  - The display attached during the spike was a NexDock (1920x1200), not the
    actual signage screen.

## Results by Criterion

### Criterion 1 — Boot: BLOCKED, not failed

A real boot test requires rewriting the SD card's boot config. The Pi 3B+ has
no EEPROM bootloader, so there's no tryboot/autoboot.txt A/B fallback (that's
a Pi 4/5 feature) — a bad config stays broken across power cycles, and
recovery would need physical access with a card reader.

The safest alternative considered was kexec — booting a NixOS kernel from
within the running Debian system without touching `config.txt` at all. This
was found to be impossible on this specific kernel: `CONFIG_KEXEC` is absent
from the Raspberry Pi OS vendor kernel build (`6.12.62+rpt-rpi-v8`), confirmed
via a direct syscall probe (`kexec_load`/`kexec_file_load` both return
`ENOSYS` even as root). Zero risk was taken pursuing this — the board's
`/boot/firmware/config.txt` and `cmdline.txt` remain byte-for-byte untouched
(verified against original timestamps/checksums).

The full real boot path was verified as fully specified and available
*without* actually booting it:

```
firmware (bootcode.bin / start.elf)
  → U-Boot 2026.04 for Pi 3 64-bit
  → extlinux.conf
  → NixOS generation
```

Every component in that chain (`raspberrypifw`, `ubootRaspberryPi3_64bit`,
`raspberrypiWirelessFirmware`) was confirmed present in the binary cache
(cache.nixos.org) for aarch64. NixOS's own docs say the stock aarch64 SD
image boots a Pi 3 out of the box.

**Conclusion:** very likely to just work, but unconfirmed. Needs a second SD
card (~£8) to test safely with a fallback in hand. This is now the single
highest-value unblocker for closing out the gate.

Also noted: the NixOS generation-rollback mechanism is the U-Boot extlinux
boot menu, which needs a keyboard/serial console physically at the sign — the
rollback safety net is real but **not remotely triggerable**.

### Criterion 2 — Display/GL: PASS

Hardware is VC4 V3D 2.1, OpenGL 2.1 compatibility profile, GLSL 1.20, GLES
2.0. Fyne v2.7.4 binds `github.com/go-gl/gl/v2.1/gl` — an exact match.

nixpkgs' own Mesa 26.2.1 was confirmed driving this exact GPU (headless via
GBM and through X, reporting real VC4 V3D 2.1 hardware, not the llvmpipe
software fallback). Debian runs Mesa 26.2.0, so this is effectively the same
Mesa version. X came up on the real HDMI output using the `modesetting`
driver with `glamor` acceleration enabled.

A Nix-built aarch64 mural binary was run for 45 seconds on the real display
against the real content directory, with ffmpeg/ffprobe/cec-client all
resolved from `/nix/store` via `PATH` — no errors, and it successfully issued
a real CEC power-on command.

**Gotcha for Phase 2 design:** nixpkgs' Mesa hardcodes the path
`/run/opengl-driver/lib`, which is only populated when
`hardware.graphics.enable = true` is set in the NixOS config. Without it,
GBM/X11 EGL fail with a "failed to open dri" error that *looks* like a driver
bug but is actually just a missing config flag.

**Residual gap:** the X server and kernel used during this test were still
Debian's (same upstream version NixOS ships), so only the actual NixOS kernel
remains unverified, pending the boot test (Criterion 1).

### Criterion 3 — Build feasibility: PASS

mural (CGo + Fyne, full GL/X11 build closure) was built natively on the board
via Nix's `buildGoModule`.

- **Cold build:** 41m24s, but ~21m of that was a transient
  `proxy.golang.org` failure that succeeds on retry (same issue seen on the
  x86 dev machine) — real cold cost is closer to **20 minutes**.
- **Incremental build** after a source change: **12m32s** — this is the
  number that matters for ongoing update practicality.
- Only 2 derivations needed to build locally; 97 build paths (212 MiB / 738
  MiB unpacked) came prebuilt from the binary cache.
- Memory peaked under ~500MB, no OOM (board has 905MB usable).

A complete NixOS sign configuration was also evaluated for aarch64 (extlinux
boot, autologin kiosk user, X + ratpoison, mural, ffmpeg-headless, libcec, a
guest-denied Samba content share, WiFi, SSH):

- Evaluates cleanly in 7 seconds, 5,184 derivations in the closure.
- mural itself is the only real local compile.
- A mural source-code change invalidates just 13 of those 5,184 derivations.

**Important trap identified:** the `nixos-hardware` project's
`raspberry-pi-3` hardware profile pulls a specific kernel version
(`linux-rpi-6.18.39`) that is **not** present in the binary cache (confirmed
404). Hydra (the NixOS build farm) only builds nixpkgs itself, not
nixos-hardware profiles, so using that profile would force compiling a full
Linux kernel on-device — which the NixOS wiki explicitly warns can exhaust
the Pi's 1GB RAM. The stock mainline aarch64 kernel (6.18.46) IS cached and
prebuilt.

**Recommendation:** Phase 2 should default to the stock kernel, and treat the
vendor/hardware-specific kernel profile as a deliberate, expensive opt-in
only if something specifically requires it.

**Update-model conclusion:**

- First-time provisioning entirely on-device is impractical: 1.8GiB download
  / 4.2GiB unpacked, and this SD card's effective write speed under
  compression-unpacking load is only 0.3-0.4 MB/s — around 30x slower than
  the board's raw ~11MB/s network throughput. First provisioning this way
  would take several hours.
- Ongoing updates on-device are entirely practical at ~12.5 minutes each,
  since only the mural package itself needs rebuilding once the base system
  already exists.

**Recommended approach:** provision via a prebuilt image or a remote/cross
builder initially, then rely on on-device `nixos-rebuild` for subsequent
updates.

### Criterion 4 — Runtime deps: PASS

- `ffmpeg`, `ffmpeg-headless`, and `ffmpeg-full` nixpkgs variants all ship
  both `ffmpeg` and `ffprobe` binaries in one package output (required for
  Go's `exec.LookPath`), and all include both the software H.264 decoder and
  the Pi's hardware decoder `h264_v4l2m2m`. All confirmed prebuilt for
  aarch64.
- `libcec` provides a `cec-client` binary under exactly that name.
- Verified directly on the board: all three real `.mp4` files in the content
  directory decoded successfully (rc=0) using nixpkgs' `ffmpeg-headless`.

**Recommendation:** use `ffmpeg-headless` specifically — 299MiB closure size
vs. 1045MiB for `ffmpeg` and 1466MiB for `ffmpeg-full`, with identical
ffmpeg+ffprobe+H.264 coverage for this use case.

Graceful degradation was confirmed under Nix packaging too: with cec-client
absent from `PATH`, mural logged "cec-client not found in PATH; CEC control
disabled" and continued running normally, matching existing documented
behavior.

**Flagged for Phase 2 verification:** nixpkgs-unstable currently ships ffmpeg
9.0, while `video.go`'s documentation says it was verified against
ffmpeg/ffprobe 8.1.2. Decoding worked fine in testing, but ffprobe's JSON
output format should be specifically re-verified against 9.0 since that's
what `video.go` parses. Note: the pinned nixpkgs 26.05 release channel still
ships 8.1.2, so this is really a channel-choice decision (unstable vs. a
pinned release) rather than an unavoidable problem.

### Criterion 5 — Sustained operation: PASS, with a caveat that grew into its own investigation thread

Initial finding: nixpkgs' ffmpeg 9.0 performed within ~7% of Debian's ffmpeg
7.1.5 on mural's actual pipeline
(`scale=1920:1080,fps=min(30,src) → rawvideo rgba`) — both were slow (0.53x
realtime for a 1080p60 clip, 0.37x for a 2160x2160 clip), with the board
hitting its thermal soft-limit (clocking 1400→1200MHz at 60-70°C) under
mural's own decode load.

**Conclusion at this stage:** NixOS causes no regression vs. Debian, so
criterion 5 passes — but sustained video on Pi-3-class hardware looked like a
pre-existing limitation neither caused nor fixed by the NixOS migration.

This result is what triggered the follow-up investigation below, which
substantially changed the picture.

## Follow-up Investigation — Video Pipeline

This is now the most architecturally significant finding of the spike. It
changed the picture from "the Pi 3 just can't do video well" to "mural's
specific approach is the bottleneck, and even the best approach has real
display-pipeline constraints on this hardware."

### 1. First follow-up — VLC, no X, decode-only

Played the same clip via VLC directly (no mural binary, no X — a
no-display, decode-focused test) using the board's hardware H.264 decoder
(`h264_v4l2m2m`/bcm2835-codec).

**Result:** ~0.94-0.97x realtime automatically, with much lighter thermal
load (clock held at 1400MHz, no throttling) vs. mural's own test, which
throttled to 1200MHz.

**Conclusion at this stage:** the slowness seen in Criterion 5 was mural's
own software decode/scale pipeline not using hardware acceleration at all —
NOT a Pi 3B+ hardware ceiling. This looked like a straightforward,
mural-side fixable problem (add `-hwaccel` usage), independent of the NixOS
decision.

### 2. Second follow-up — real fullscreen X11 test, at Liz's own hands, on the actual console

Liz herself ran the real fullscreen/ratpoison VLC test from the console and
observed inconsistent framerate and compression artifacts live.

Log analysis of all 3 runs refined the earlier finding:

- Hardware decode itself was confirmed clean and consistent across all runs
  (no fallback, zero decode errors/corruption), and the aggregate wall-clock
  ratio (~0.91-0.95x) still matched the earlier no-X test.
- **BUT:** the real display pipeline (X + glamor + DRM page-flip, layered on
  top of decode) showed heavy per-frame lateness — approximately 45-50% of
  frames triggered a lateness event in each run, and ~330-360 frames out of
  ~1831 total were actually dropped by the decoder trying to catch up.
- This was **not** thermal (clock held at 1400MHz throughout, no new
  throttle indicators) — it was frame-level jitter specific to the
  render/compositing pipeline, which the first no-X test structurally could
  not have surfaced since it had no real display/compositor/page-flip step
  at all.
- The visual compression artifacts observed were a separate, unrelated
  finding: the source clip itself is a low-bitrate encode (~779kbps at
  1920x1080@60fps, confirmed via ffprobe), not a decode-path problem.

**Conclusion at this stage:** "hardware decode = clean playback" was too
strong a claim — X11 compositing overhead itself was a real bottleneck.

### 3. Third follow-up — no-compositor DRM/KMS shootout

Given X11 showed real problems, and an early Wayland/cage+mpv test with
hardware decode forced showed even worse presentation drops (~91%), plus a
Wayland/dmabuf-wayland path where mpv segfaulted (traced to a known open
upstream mpv bug, unrelated to this hardware) — a proper 3-way shootout was
run of approaches that bypass X11/Wayland entirely and talk straight to
DRM/KMS: VLC with `--vout=drm_vout`, mpv with `--gpu-context=drm`, and Kodi
in standalone/GBM mode.

**Result: Kodi standalone/GBM wins clearly.**

- **Kodi:** hardware decode (`h264_v4l2m2m`) confirmed, zero frame-drop
  indicators across 6 runs, tightest and most consistent timing (~0.85-0.93x
  realtime every single run). One cosmetic non-fatal filter-graph error
  appeared but auto-recovers every time. Real caveat: meaningful startup
  overhead per launch, so a production setup needs to run Kodi as one
  persistent process driving a playlist, not relaunched per clip.
- **VLC/drm_vout:** close second — excellent raw drop statistics (2-3% /
  5-11%, far better than the X11 result), zero decode errors logged. But
  live viewing showed visual corruption at drop moments; the best-supported
  (not proven) explanation is a buffer-fencing race in VLC's zero-copy
  DRM_PRIME scanout path (the display committing a frame before the hardware
  decoder finishes writing it) — this needs direct re-confirmation before it
  could be trusted in production.
- **mpv/gpu-context=drm:** actual playback throughput was near-realtime, but
  it has a serious, 100%-reproducible hang in its DRM output during
  end-of-stream draining — 25 to 138 seconds of dead time after every single
  run. This disqualifies it outright regardless of decode quality.

**Kodi kiosk-control specifics** (verified against this box's actual
installed Kodi 21.3 keymap/schema, not general knowledge):

- Playlist looping requires a `~/.kodi/userdata/autostart.py` script (runs on
  every Kodi boot) calling `xbmc.executebuiltin('PlayMedia(...)')` followed
  by `PlayerControl(RepeatAll)` (or the JSON-RPC equivalent
  `Player.SetRepeat`).
- Next/previous-in-playlist is bound to PageUp/PageDown — **not**
  period/comma, which get remapped specifically to frame-stepping during
  fullscreen video playback in this Kodi version (a version-specific
  gotcha).
- Jump-to-playlist-start has **no default keybinding at all** — it needs a
  small custom `~/.kodi/userdata/keymaps/keyboard.xml` using the
  `PlayList.PlayOffset(video,0)` action, scoped specifically to the
  `<FullscreenVideo>` context so it doesn't affect Kodi's GUI elsewhere.
- Keyboard input was confirmed working correctly in this no-X GBM mode
  (libinput directly detected real keyboard hardware in test logs, not
  assumed from docs).

## The Big Open Architectural Question This Raises

mural today streams raw ffmpeg frames into its own Fyne `canvas.Image` (per
`video.go` / `CLAUDE.md`'s architecture notes) — entirely custom, in-process
video rendering. The shootout results suggest that on this hardware class, a
persistent external Kodi process driving a playlist via DRM/KMS directly is
the only approach so far that plays video cleanly without frame-level jitter.

This is a bigger fork than "which OS" — it's a question about whether mural
should keep doing its own video decode/render at all, or delegate video
playback to an external player process (Kodi) while possibly keeping its own
Go/Fyne code for image slides, scheduling, CEC control, etc.

Liz explicitly said (verbatim, when this file was requested):

> "I'm not sure if it even makes sense to keep go code, though."

That's the open question to sit with. This file's job is to preserve the
findings, not resolve it.

## Open Questions / Follow-ups for Next Session

1. **Architecture — RESOLVED.** Decision: keep the Go/Fyne codebase, but scope
   it to static images only. In-process video playback (`video.go`, the
   frame-streaming-into-`canvas.Image` approach) has been removed entirely
   from mural. A future "Kodi launcher mode" — mural handing off to an
   externally-managed Kodi process for video, rather than decoding video
   itself — is the planned path for bringing video back, but it is **not
   yet implemented**; this spike's findings (Kodi standalone/GBM as the only
   approach that played cleanly on the Pi 3B+) are what motivated the
   decision, and remain the reference for whoever builds that mode. See the
   commit that removed `video.go` and updated `CLAUDE.md` accordingly for
   where this was actioned.

   Of the options this file originally posed — (a) keep mural as-is and
   accept video jitter, (b) mural drives Kodi as an external player while
   keeping its own image/scheduling/CEC logic, (c) a larger rearchitecture,
   (d) something else — the chosen path is closest to (b), staged in two
   steps: first strip video out (this change), later add the Kodi-launcher
   handoff as its own piece of work.

2. **Boot criterion still open.** Needs a spare SD card (~£8) to safely
   confirm the real NixOS boot path works on this Pi 3B+ before fully closing
   `Analyst.md`'s Task 1 gate.

3. **ffmpeg version/channel choice.** Pin nixpkgs to a release channel with
   ffmpeg 8.1.2 (matching `video.go`'s documented verification) vs. track
   unstable's ffmpeg 9.0 and re-verify ffprobe's JSON parsing against it.

4. **mural's hwaccel gap.** mural's own ffmpeg invocation doesn't use
   `-hwaccel`/`h264_v4l2m2m` at all today — independent of the NixOS
   question, this is a likely-real performance bug worth its own task
   regardless of which video architecture is chosen.

5. **Kernel choice.** Use the stock cached mainline aarch64 kernel by
   default; only consider the `nixos-hardware` Pi-3-specific profile (which
   forces an on-device kernel compile that can exhaust RAM) if something
   concrete requires it.

6. **Infra hygiene on the box** (unrelated to NixOS but discovered along the
   way):
   - Tailscale is logged out and needs `sudo tailscale up` again.
   - The box's LAN IP moved (192.168.0.23 → 192.168.2.27) and may move
     again.
   - The `claude` automation user's SSH key stopped authenticating
     (workaround was using `jassg` instead) and should be re-provisioned.

7. **Cleanup/left-behind state on the box** (all reversible, documented in
   `~/servers/mural/README.md`'s changelog):
   - Nix was installed single-user at `/nix` (~3GB) and left in place
     intentionally since Phase 2 will want it. Removable via
     `sudo rm -rf /nix ~/.nix-profile ~/.nix-channels ~/.config/nix ~/.local/state/nix`
     if not wanted.
   - A spike work tree exists at `~/nixspike/` (safe to delete).
   - VLC and Kodi were installed for the shootout testing (~230MB+).
   - An `oom_score_adj -1000` tweak was applied to ssh/tailscaled processes
     to protect remote access during heavy builds (non-persistent, resets on
     reboot, no cleanup needed).
