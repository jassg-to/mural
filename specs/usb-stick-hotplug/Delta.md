# Delta: USB Stick Hotplug

> Specification delta — what changes relative to the current system.
> Only exists when this feature modifies existing behaviour.

> **Currency:** this delta was first written against the pre-DRM codebase (Fyne
> renderer, `SetOnTypedKey` input). The DRM/KMS + evdev migration landed the
> following day. Every code reference below has been re-anchored to the current
> HEAD, and every claim re-verified against it. No substantive statement changed:
> the fusion in `Reload`, the discarded `[slideshow]` section, the fatal empty
> content directory, and the Samba co-writer all still hold exactly as described.

## ADDED

- Removable volumes becoming available at runtime are detected while the player
  is running. No equivalent exists today; the content directory is fixed at
  startup from a positional argument and never re-pointed.
- A `config.toml` at a volume's top level is required as the marker that the
  volume is intended as Mural content. Volumes without one are ignored outright,
  images or not. Nothing in the current system distinguishes "a stick meant for
  this sign" from "any stick"; this is the first such notion.
- A volume is classified into exactly one of three dispositions before anything
  is acted on — *ignored* (no `config.toml`), *rejected* (`config.toml` present
  but unusable, logged as an error), or *accepted* (usable `config.toml`).
  *Ignored* and *rejected* have identical effect on the running player and are
  kept distinct so the log separates "not for us" from "for us, but wrong."
- A configuration that parses but would leave the display permanently off is
  treated as unusable rather than adopted.
- Images are ingested from a volume into the player's own content directory,
  wholesale replacing the previously displayed set.
- Displaced images and configuration are retained on the device in a recoverable
  form, bounded to the single most recent displaced set.
- A free-space check gates the ingest; an ingest that would not fit is declined
  before anything is changed.
- Ingest is transactional: images and configuration validate as one unit, and a
  failure at any point leaves the running player exactly as it was.
- The installer provides an automount mechanism so that removable volumes are
  mounted at all on the target OS image.
- The install documentation gains a note that physical USB port access now
  confers the ability to replace displayed content and change the schedule.
- The install documentation gains instructions that a stick must carry a
  `config.toml` at its top level to be recognised at all. The installer already
  writes a sample `config.toml` into the content directory, so the operator
  workflow becomes "copy that file onto the stick alongside your images" — but
  this only works if it is written down, because a stick of perfectly good images
  now does nothing at all without it.

## MODIFIED

- **The player is read-only with respect to its content directory** → **the
  player writes to its own content directory.** Today Mural only ever reads from
  `content/`; every write comes from outside (Samba, SSH, SD card). Ingest makes
  the player a writer. Note that the optional Samba share writes to the same
  directory, so two independent writers now exist where there was one.

- **The content directory is a flat set of images, with subdirectories skipped
  and otherwise meaningless** (`scanSlides`, slideshow.go:94-98) → **the content
  directory gains internal structure**, because displaced content must remain on
  the device while staying out of the rotation.

- **The display is turned on only by the schedule or by explicit keyboard input**
  (`Schedule.onTurnOn` → `ss.Reload` → `resume`, and the nav-key wake in
  `handleNavKey`, slideshow.go:377-389) → **device insertion also turns the
  display on**, including
  outside scheduled on-hours.

  *This is an intentional exception to schedule authority, chosen deliberately:
  the operator standing at the sign needs immediate confirmation that the stick
  was accepted, and the sign showing them the new content is that confirmation.
  The cost is that anyone with physical port access can light up a sign outside
  its hours. It is bounded by inheriting the existing manual-wake semantics
  exactly — the display returns to schedule control at the next scheduled off
  event, with no new timeout mechanism introduced.*

- **`Reload` fuses "rescan content" with "un-pause and power on the display"**
  (`Reload`, slideshow.go:275-277, posts a request whose result handler at
  slideshow.go:409-420 unconditionally ends in `resume()`) →
  **the two concerns are separable, even though the hotplug path uses both.**
  The fusion is currently invisible because the only callers are scheduled
  turn-on and the Home key, both of which want both effects. Ingest also wants
  both — so the wake decision does not by itself force the split — but the split
  is still required, because ingest must rescan at a point where the display's
  power state is decided by ingest success, not implied by the rescan. A failed
  or ignored volume must not wake the display, and today any code path that
  rescans necessarily does.

- **Slideshow settings are read once at startup and never re-read** (`main.go:32-40`
  applies `cfg.Slideshow.Interval` and `cfg.Slideshow.ThumbWidth` at construction;
  `Schedule.reload`, schedule.go:183-193, assigns only `cfg.Schedule` and discards
  the `[slideshow]` section entirely) → **slideshow settings apply live when a
  configuration is ingested.**

  *Worth flagging: this gap exists today independently of this feature. The daily
  `reload_time` re-read picks up schedule changes but silently ignores `interval`
  and `thumb_width`, so the README's "edit it without restarting" is currently
  only true of the schedule. Honouring a stick's `config.toml` for slideshow
  settings requires closing that gap.*

  *Escalated by the gate revision: because a `config.toml` is now mandatory for a
  stick to be recognised at all, every successful ingest applies a configuration.
  Live application of slideshow settings moves from an occasional path to the
  main one, and cannot be deferred or treated as a rare case.*

- **An empty content directory is a fatal startup error** (`Run`, slideshow.go:452-459,
  returns `no images found in %s`, which `main.go` reports and exits 1) → **an
  empty content directory is a valid state in which the player waits for content.**

  *This follows directly from the feature: if a USB stick is a provisioning
  mechanism, then "deploy the sign, boot it, walk up and hand it content" is a
  supported journey, and it is impossible while an empty directory kills the
  process before any volume can be seen. Flagged at the gate as the one delta
  that was not among the original confirmed answers — a consequence of them
  rather than a decision that had been put to the user — and **explicitly
  confirmed there.** The alternative (requiring at least one pre-loaded image
  before a stick would ever be read) was considered and declined.*

  *Note the knock-on: the waiting state is now reachable at runtime as well as at
  startup, so "no images" stops being an error condition anywhere in the player
  and becomes an ordinary state. `Run`'s startup check is the obvious site; the
  reload path at slideshow.go:414-417 already treats an empty rescan as a
  non-fatal no-op and is consistent with this.*

## REMOVED

- **The invariant that Mural never modifies its own content directory** is
  retired. It was never written down, but it is load-bearing for reasoning about
  the existing design — in particular for the Samba share, which currently
  assumes it is the only writer. Anything downstream that relied on the content
  directory changing only between scans must be re-examined.

- No user-facing requirement, control, or documented behaviour is removed. The
  existing keyboard controls, the schedule semantics, the daily configuration
  re-read, the Samba path, and the positional content-directory argument all
  continue to work as documented.
