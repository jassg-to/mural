# Analyst: USB Stick Hotplug

> Phase 1 — Problem definition. Approved before architecture begins.

## Goal / Outcome

**Scope classification:** Complex

Mural runs as an unattended kiosk on a Raspberry Pi. Today, updating the content
on a running sign requires either a network path (the optional Samba share) or
shell access (SSH / `startx` restart). Both assume the operator is technical and
that the Pi is reachable on a network that the operator can join.

The people who actually maintain these signs often are neither. The intended
interaction is physical and requires no knowledge of the machine: **walk up to
the sign, plug in a USB stick holding the new images and a `config.toml`, and the
sign starts showing them.** The stick is a delivery mechanism, not a playback
device — once the content has been handed over, the operator takes the stick away
and the sign keeps showing the new content indefinitely, including across reboots
and power cuts.

The `config.toml` is what tells Mural the stick is meant for it. Without that
marker any flash drive plugged into the port — a personal USB key, a camera card,
a phone charging — would replace the sign's content, so a stick that lacks one is
passed over as though it had never been inserted.

This addresses the "no network, no keyboard, no login" content update, which is
currently impossible without physically removing the SD card.

## Scope

**Included:**

- Detecting that a removable storage volume has become available at runtime,
  without restarting Mural.
- Ingesting supported images from that volume into the player's own content
  directory so they persist after the volume is removed.
- Replacing the previously displayed image set with the ingested one, while
  keeping the replaced images recoverable on the device.
- Requiring a `config.toml` at the volume's top level as the marker that the
  volume is intended as Mural content at all, and ignoring any volume without
  one.
- Ingesting that `config.toml` so a stick also carries the schedule and
  slideshow settings.
- Applying the new content and new settings to the running player without a
  restart.
- Waking the display on insertion so the operator gets immediate confirmation
  that the stick was accepted, even outside scheduled on-hours.
- Changes to the installer and install documentation so that removable volumes
  are actually mounted on the target OS image, and so that operators are told a
  stick must carry a `config.toml` to be recognised (see Rules & Constraints).

**Excluded (non-goals):**

- **Windows support for hotplug.** Linux/Pi only. On other platforms the feature
  is a documented no-op, following the precedent already set by `cec.go` when
  `cec-client` is absent.
- **Playing content directly off the stick.** Content is always copied first;
  the stick is never a live playback source.
- **Writing to, modifying, reformatting, or ejecting the stick.** The volume is
  treated as strictly read-only. Mural never deletes the operator's files, and
  never marks the stick as "consumed."
- **Recursive directory traversal on the stick.** Only the volume's top level is
  considered, matching the current flat, non-recursive treatment of the content
  directory.
- **Video or any non-image media.** A separate feature covers playback formats;
  this feature inherits whatever the supported image set is at implementation
  time and adds nothing to it.
- **Encrypted, password-protected, or otherwise non-mountable volumes.**
- **A file browser, selection dialog, progress bar, or any on-screen text UI.**
  Confirmation is delivered by the content visibly changing.
- **Per-stick identity, allowlists, pairing, or authentication.** Physical access
  to the USB port is the authorisation model (see Rules & Constraints). The
  required `config.toml` is a marker of intent, not a credential, and does not
  change this.
- **Network, cloud, or remote content sources.**
- **Merging stick content with existing local content.** Ingest replaces.

## Behaviour

- When a removable volume becomes available at runtime, the system must first
  determine whether its top level contains a `config.toml`.
- When that volume's top level contains no `config.toml`, the system must ignore
  the volume entirely, **regardless of whether it contains supported images**: no
  content change, no settings change, no display wake, and no modification of
  anything already on the device. The volume is not addressed to Mural and is not
  an error.
- When that volume's top level contains a `config.toml` that cannot be parsed, or
  that parses but defines no time window during which the display is ever on, the
  system must abort the entire ingest — images included — leave all existing
  content and settings untouched, leave the display as it was, and record the
  failure in the log.
- When that volume's top level contains a usable `config.toml`, the system must
  accept the volume and proceed with the ingest.
- When a valid ingest begins, the system must continue displaying the currently
  playing content until the ingest is complete; the display must not blank,
  stall, or show partial results mid-ingest.
- When an accepted volume supplies at least one supported image, the system must,
  on successful completion, make the rotation consist of exactly the images
  supplied by that volume, and no others.
- When images are replaced in this way, the previously displayed images must be
  retained on the device in a recoverable form rather than deleted.
- When an accepted volume supplies no supported image, the system must apply the
  configuration and leave the rotation exactly as it was. An accepted volume must
  never empty the rotation or blank the sign for want of images.
- When a volume is accepted, the system must adopt its `config.toml` as the
  player's own configuration, retain the previous configuration in a recoverable
  form, and apply the new schedule and slideshow settings to the running player
  without a restart. Because a `config.toml` is now required for acceptance, this
  happens on every successful ingest rather than occasionally.
- When an ingest completes successfully, the system must rescan its content,
  begin the rotation at the first slide, and wake the display — turning the
  physical display on — regardless of whether the schedule currently says the
  display should be on.
- When an ingest fails for any reason, the system must leave the running player
  in the state it was in before the volume was seen, and record the failure.
- When the volume is removed, the system must continue playing the ingested
  content unchanged. Removal is not an event that alters what is displayed.
- When the player next starts, it must display the ingested content, with no
  dependence on the volume being present.
- When the system is running on a platform where removable-volume detection is
  not supported, it must start and run normally with the feature inactive.

## Rules & Constraints

- **A volume has exactly three dispositions, and they are distinct.**
  1. **Ignored** — no `config.toml` at the top level. The volume is not addressed
     to Mural. Nothing changes, nothing is logged as a failure, the display does
     not wake. This is the expected outcome for someone's personal flash drive,
     a phone charging over USB, or a camera card.
  2. **Rejected** — a `config.toml` is present but unusable. The volume *is*
     addressed to Mural and is broken. Nothing changes and the display does not
     wake, but the failure is recorded in the log as an error.
  3. **Accepted** — a usable `config.toml` is present. The ingest proceeds;
     images are applied if the volume has any, and the display wakes.

  The difference between *ignored* and *rejected* is intent, not effect: both
  leave the running player untouched. They are separated so that the log
  distinguishes "not for us" from "for us, but wrong."

- **`config.toml` is a marker of intent, not a security control.** Requiring it
  prevents an arbitrary flash drive from silently replacing a sign's content. It
  authenticates nothing — anyone can put a `config.toml` on a stick. It must not
  be described or relied upon as a security boundary; the authorisation model
  remains physical access to the port.

- **A configuration that would leave the sign permanently dark is not usable.** A
  `config.toml` that parses but defines no on-window anywhere would silently
  switch the sign off forever, with no on-screen indication and no way for a
  non-technical operator to diagnose it. Such a configuration must be rejected
  rather than adopted.

- **Ingest is all-or-nothing.** An accepted volume's images and configuration are
  validated as one unit before anything on the device is changed. There is no
  partial application, and no state in which some of the stick's images are
  showing and some are not.
- **Partially copied files must never enter the rotation.** A volume pulled out
  mid-copy must leave the sign showing a complete, coherent image set — either
  the old one or the new one, never a mixture and never a truncated file.
- **The stick is read-only.** No writes, no deletions, no metadata changes, no
  unmount side effects that could damage the operator's data.
- **Nothing is destroyed.** Every image and configuration file that an ingest
  displaces must remain recoverable on the device afterwards. The operator can
  overwrite the sign's content by accident; they must not be able to lose the
  previous content by accident.
- **Retained content must not accumulate without bound.** The SD card on a Pi is
  small relative to a USB stick. The system must bound how much displaced content
  it keeps, retaining at most the single most recent displaced set.
- **Retained content must not appear in the rotation.** Recoverable does not mean
  visible.
- **An ingest that does not fit must not be attempted.** If the device lacks the
  free space for the volume's images, the system must decline the ingest, leave
  existing content intact, and log it — never fill the SD card and never leave a
  half-copied set.
- **Mounting is the installer's responsibility, not the player's.** Mural runs as
  an unprivileged autologin kiosk user. It must not acquire root, a setuid
  helper, or any other privilege escalation in order to mount media. The
  installer provides the automount mechanism; the player only observes mount
  points appearing and disappearing.
- **This makes existing installations incomplete.** Deployments installed before
  this feature will have no automount mechanism and the feature will be inert
  until the installer is re-run. That must be documented, not silently assumed.
- **The target OS image does not automount today.** `docs/INSTALL.md` specifies
  Raspberry Pi OS Lite, and `install.sh` installs no automounter. A USB stick
  plugged into a stock Mural deployment is currently visible as a block device
  but is never mounted. This feature is not viable without the installer change
  above; it is a prerequisite, not an enhancement.
- **Physical access is the authorisation model.** Anyone who can reach the USB
  port can replace what the sign displays and change when it is on. This is
  accepted deliberately: it is the same trust boundary as the keyboard already
  wired to the Pi, which can already pause the sign and quit the player. It must
  be stated in the install documentation so an operator siting a sign in a public
  space can make an informed choice about port access.
- **Volumes are processed one at a time.** Two sticks inserted together, or a
  stick inserted while an ingest is running, must not interleave.
- **Re-inserting the same stick repeats the ingest.** The operation is idempotent
  in its result; the cost of re-copying is accepted in exchange for not having to
  track stick identity.

## Edge Cases

| Scenario | Expected behaviour |
|----------|--------------------|
| Stick has usable `config.toml` and images | **Accepted.** The main journey: config adopted, rotation becomes exactly the stick's images, previous set retained recoverably, display wakes |
| Stick has usable `config.toml` and no images | **Accepted.** Config adopted and applied; rotation unchanged and **not** emptied; existing images stay in rotation and are not displaced; display wakes |
| Stick has images but **no** `config.toml` | **Ignored.** No content change, no wake, no error. The images are not ingested — the missing `config.toml` is what makes this a non-Mural stick, and images alone do not override that |
| Stick has neither `config.toml` nor images | **Ignored.** Same disposition and same code path as the row above; the absence of `config.toml` alone decides it |
| Stick has a malformed / unparseable `config.toml` | **Rejected.** Entire ingest aborted, images included; sign keeps playing what it was playing; display does not wake; failure logged as an error |
| Stick has a `config.toml` that parses but defines no on-window at all | **Rejected**, not accepted. Adopting it would switch the sign off permanently with no on-screen explanation. Treated identically to a malformed config |
| Stick has unsupported files only (PDFs, videos, `.txt`) **with** a usable `config.toml` | **Accepted** as a config-only ingest; rotation unchanged and not emptied |
| Stick has unsupported files only, no `config.toml` | **Ignored** |
| Operator's personal flash drive, camera card, or phone plugged into the port | **Ignored** — this is the case the `config.toml` requirement exists to handle |
| Stick is rejected, and the operator is standing at the sign | Nothing visibly happens; the operator cannot distinguish "wrong stick" from "broken config" without the log. Accepted limitation — an on-screen message is a non-goal |
| Stick pulled out mid-copy | Ingest fails; sign continues playing the previous content set intact; no truncated file ever displayed; partial work discarded |
| Stick content exceeds free space on the SD card | Ingest declined before any change; existing content intact; logged |
| Stick inserted during scheduled off-hours | Ingest proceeds and the display wakes, contrary to the schedule (see Delta) |
| Display woken by insertion, then left alone | Stays on until the next scheduled off event, inheriting the existing manual-wake semantics of the nav keys — no new timeout is introduced |
| Stick inserted while the display is already on and mid-rotation | Current image finishes displaying normally; rotation switches to the new set at the first slide when the ingest completes |
| Second stick inserted while an ingest is running | Queued and processed after the first completes; not interleaved |
| Same stick re-inserted immediately | Ingest runs again; result is identical; previously displaced set is superseded by the bounded-retention rule |
| Stick's images and the current content are byte-identical | Ingest runs normally; result is indistinguishable; no special-casing |
| Volume mounts but is unreadable (permissions, corrupt filesystem) | Treated as an ingest failure; existing content intact; logged |
| Stick with multiple partitions | Each mounted volume is evaluated independently as it appears, serialised per the one-at-a-time rule; each must carry its own top-level `config.toml` to be accepted |
| Stick inserted before the player has finished starting | Handled once the player is running; a volume already mounted at startup is evaluated at startup |
| Player restarted after an ingest | Ingested content displays; no dependence on the stick |
| Automount not installed (pre-existing deployment) | Nothing happens on insertion; documented as requiring an installer re-run |
| Running on Windows | Feature inactive; player otherwise unaffected |

## Open Questions

| Question | Answer |
|----------|--------|
| Copy the stick's images to the Pi, or play them directly off the stick? | **Copy.** Images are copied into the player's own content directory and persist after removal. The stick is a delivery mechanism, not a playback source. |
| When copying, delete local images that are not on the stick? | **Effectively yes, but non-destructively.** The rotation after ingest contains exactly the stick's images, so pre-existing images leave the rotation — but they must be retained in a recoverable form, not deleted. Chosen over outright deletion because a single mis-plugged stick would otherwise irreversibly destroy a sign's whole library, and over an additive merge because that would silently dilute the content the operator came to install. Retention is bounded to the most recent displaced set so the SD card cannot fill over time. |
| When copying, overwrite same-named files? | **Question dissolves.** Because the previous image set is displaced wholesale before the new one lands, the destination holds no competing images and there is no same-name collision to resolve. Within a single ingest, the stick's own top level cannot contain two files of the same name. |
| Who mounts the volume — Mural or the OS? | **The OS, arranged by the installer.** `install.sh` gains an automount mechanism; Mural stays unprivileged and only watches mount points. |
| What does "override" mean concretely, given content is copied? | **Override is achieved by the copy itself and is permanent.** There is exactly one rotation source at all times — the player's own content directory. Ingest displaces the old set and installs the new one, so "only stick content plays" is a consequence of the copy, not a separate transient source-switching mode. Nothing reverts when the stick is removed. This was the main tension between the copy decision and the override decision, and resolving it this way avoids a dual-source runtime mode entirely. |
| Does the display blank or pause during the copy? | **No.** The previously playing content continues throughout; the switch is visible only once the ingest completes. |
| Should insertion wake the display outside scheduled hours? | **Yes.** Insertion is treated as a manual wake, equivalent to pressing a nav key today. Recorded in the Delta as an intentional exception to schedule authority. |
| How long does the display stay on after an out-of-hours wake? | **Until the next scheduled off event**, inheriting the existing manual-wake behaviour exactly. No new timeout, no new concept. |
| Is Windows in scope? | **No.** Linux/Pi only, graceful no-op elsewhere. |
| Is a `config.toml` on the stick honoured? | **Yes**, including schedule and slideshow settings. |
| Is a `config.toml` on the stick *required*? | **Yes — revised at the Phase 1 review gate.** Its presence at the volume's top level is the marker that the stick is intended as Mural content. A volume without one is ignored outright, even if it is full of valid images. This exists to stop an arbitrary flash drive — a personal USB key, a camera card, a phone — from silently replacing a sign's content merely by being plugged in. |
| Does a stick with images but no `config.toml` still work? | **No, not any more.** This was permitted in the first draft of this spec and is now explicitly ignored. It is the single behavioural change made by the gate revision. |
| Is an honoured `config.toml` copied permanently or applied transiently? | **Copied permanently**, consistent with the copy model for images. A transient config would create a split-brain in which images persist but settings revert — confusing to reason about and impossible for an operator to predict. The displaced configuration is retained recoverably like the displaced images. |
| What if the stick's config is invalid? | **Reject the volume and abort the whole ingest**, images included. All-or-nothing is more predictable for a non-technical operator than partial application. Distinct from "ignored": a broken config means the stick *was* addressed to Mural, so it is logged as an error rather than passed over silently. |
| Is "no `config.toml`" the same as "invalid `config.toml`"? | **No — three distinct dispositions.** No config → *ignored*, silently, not an error. Present but unusable → *rejected*, logged as an error. Present and usable → *accepted*. The first two have the same effect on the running player and differ only in intent and logging; keeping them distinct is what makes the log actionable. |
| Does an empty or scheduleless `config.toml` count as usable? | **No — decided here, flagged for confirmation at the gate.** A `config.toml` that parses but defines no on-window anywhere is *rejected*, not accepted. Adopting one would set the sign to "never on," permanently and silently, with no on-screen explanation and no way for a non-technical operator to recover without shell access. This case is newly reachable *because* a config is now mandatory — every operator must now produce one, so a minimal or hand-typed file is a likely mistake. Chosen for consistency with the "nothing is destroyed" and "decline rather than damage" rules, but it is an inference from the requirement rather than an instruction, and can be overridden. |
| Does a stick with no images wipe the library? | **No.** Images and configuration remain independent payloads, each applied only if present — but the *configuration* is now the required one and the *images* are optional. An accepted config-only stick changes settings and leaves the rotation exactly as it was. An accepted volume must never empty the rotation. |
| Scope classification | **Complex** — new runtime domain (device and mount lifecycle), cross-cutting across the player, the scheduler, the installer, and the docs, with an unprivileged-process constraint. Full pipeline including `sdd-harden`. |

## Analyst Checklist

- [x] Goal is tied to a specific user need
- [x] Scope boundaries are explicit — what's in and what's out
- [x] All ambiguities resolved — no open questions remain
- [x] Behaviour is declarative, not prescriptive
- [x] Edge cases are identified and handled
- [x] Non-goals prevent scope creep
