# Analyst: Video Playback

> Phase 1 — Problem definition. Approved before architecture begins.

## Goal / Outcome

**Scope classification:** Complex

Mural currently cycles only through static images (JPG/PNG), each shown for a single globally-configured `interval`. This feature lets the `content/` directory also contain video files, which play as slides in the rotation: the slide's preview/thumbnail is the video's first frame, and the slide's on-screen duration is exactly the video's own playback length rather than the configured interval. This addresses the case where signage content includes short video clips (e.g., product demos, animated promos) that should play to completion rather than being shown as a static frame or cut off/extended by a fixed timer.

This is a Complex feature: it introduces a new decoding domain (video, vs. the existing static-image pipeline) and is cross-cutting — it touches content scanning, the `Slide` data model, the rendering/fit pipeline, and the auto-advance timing mechanism.

## Scope

**Included:**
- Recognizing MP4 (H.264) files in `content/` as valid slides, alongside existing image types.
- Extracting a video's first frame as its thumbnail, used the same way image thumbnails are used today (instant-nav preview).
- Extracting a video's exact playback duration during scanning.
- Playing video slides in the display area, fit to the window the same way images are fit today.
- Auto-advancing to the next slide when a video finishes playing, using the video's own duration instead of the configured `interval`.
- Interrupting video playback immediately on manual navigation (Left/Right/Home) or pause (schedule off / Delete key), consistent with existing image slide behavior.
- Excluding undecodable video files from the rotation, with an error logged, without halting the slideshow.
- Reusing a previously-scanned video's cached thumbnail and duration on `Reload` when the file's size and mtime are unchanged (mirrors existing image reuse behavior).

**Excluded (non-goals):**
- Video audio output. Default behavior is muted playback (simplest, lowest-risk choice for a passive signage display); no per-install audio configuration in this phase.
- Any video format/container/codec other than MP4 (H.264) — no MOV, WebM, HEVC, etc.
- Looping or repeating a video within a single slide viewing — a video plays once, and that single playthrough is the slide's duration.
- Any minimum or maximum slide-duration clamp on video length — the configured `interval` is not applied to video slides in any form.
- Video editing, trimming, or transcoding tooling.
- Resuming a video mid-playback after a pause/interrupt — a resumed or re-shown video slide restarts from the beginning, consistent with how image slides are redisplayed today.
- Guaranteeing Windows parity for video playback in this phase (see Rules & Constraints and Open Questions).

## Behaviour

- When the content directory is scanned, the system must recognize files with the `.mp4` extension as video slides, in addition to the existing image extensions.
- When a video slide is scanned, the system must extract its first frame as the slide's thumbnail image, used the same way an image slide's thumbnail is used today.
- When a video slide is scanned, the system must determine the video's exact playback duration.
- When a video slide becomes the current slide, the system must play the video in the display area, fit to the window the same way images are fit today.
- When a video slide finishes playing to completion, the system must automatically advance to the next slide, mirroring the existing ticker-driven auto-advance used for images.
- While a video slide is the current slide, the system must not advance to the next slide before the video's actual duration has elapsed, and must advance at that point regardless of the globally configured image `interval`.
- When the user navigates away from a currently playing video slide (Left/Right/Home), the system must stop that video's playback immediately.
- When the slideshow is paused (schedule off, or Delete key), the system must stop any currently playing video and black the screen, consistent with existing image pause behavior.
- When the slideshow resumes on a video slide, the system must redisplay that slide from the beginning, consistent with how image slides are redisplayed on resume.
- When a video file cannot be decoded, the system must exclude it from the slide rotation and log an error, without halting the slideshow.
- When the content directory is rescanned (`Reload`) and a previously-scanned video file's size and mtime are unchanged, the system must reuse its cached thumbnail and duration without re-processing it, consistent with existing image reuse behavior.

## Rules & Constraints

- Only MP4 (H.264) video files are supported in this phase.
- Video audio must not be relied upon; default behavior is muted playback.
- The configured `interval` setting continues to govern only image slide duration; it must never be applied to a video slide.
- The existing generation-counter mechanism that guards against a stale background load overwriting the currently displayed slide must continue to apply to video preparation/loading as well.
- Video playback must degrade gracefully — a failing or undecodable video must never crash the slideshow or block advancing through the rest of the rotation.
- Per project convention, Linux (Raspberry Pi) is the primary target and Windows is a secondary target; the chosen video decoding approach's cross-platform feasibility is a Phase 2 (Architect) concern and is not resolved here.

## Edge Cases

| Scenario | Expected behaviour |
|----------|--------------------|
| Video file has zero-length or unreadable duration | Excluded from rotation, logged as invalid |
| The only slide in the rotation is a video | Video plays, finishes, then replays itself (mirrors current single-image behavior, where the ticker just re-displays index 0) |
| Rapid manual navigation lands on a video slide before a previous video finished preparing | Only the final target slide begins playing; in-flight preparation for any skipped-past video is cancelled (mirrors existing generation-counter cancellation used for images) |
| Video slide during `Reload` (content directory rescanned) | If the file is unchanged (size/mtime), reuse cached thumbnail/duration; if changed, re-extract first frame and duration |
| Very short video (e.g., under 1 second) | Plays and advances per its actual duration; no minimum duration is enforced |
| Very long video (e.g., 10+ minutes) | Plays in full; no maximum duration is enforced |
| `content/` directory has zero valid slides (all images and videos invalid) | Unchanged from current behavior: treated as no images found |
| An `.mp4` file whose internal codec is not H.264 (e.g., HEVC-in-MP4) | Treated as undecodable — excluded from rotation, logged as invalid, per the MP4(H.264)-only constraint |

## Open Questions

Questions resolved during this phase, with confirmed answers.

| Question | Answer |
|----------|--------|
| Which video formats/codecs must be supported? | MP4 (H.264) only |
| Should video audio play? | Muted by default — the simplest option for a passive signage display; no per-install audio config in this phase |
| What happens when a video can't be decoded? | Excluded from rotation entirely, error logged, slideshow continues unaffected |
| Does the video decode approach need to support Windows as well as Linux? | Not resolved here — carried forward to Phase 2 as a feasibility question against the project's Linux-primary/Windows-secondary target convention |

## Analyst Checklist

- [x] Goal is tied to a specific user need
- [x] Scope boundaries are explicit — what's in and what's out
- [x] All ambiguities resolved — no open questions remain
- [x] Behaviour is declarative, not prescriptive
- [x] Edge cases are identified and handled
- [x] Non-goals prevent scope creep
