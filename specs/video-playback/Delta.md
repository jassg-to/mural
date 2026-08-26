# Delta: Video Playback

> Specification delta — what changes relative to the current system.
> Only exists when this feature modifies existing behaviour.

## ADDED

- Recognition of `.mp4` video files as a valid slide type during content directory scanning (currently only `.jpg`/`.jpeg`/`.png` are recognized).
- First-frame extraction as a video slide's thumbnail.
- Per-slide video duration extraction during scanning.
- Playback-driven auto-advance for video slides, based on the video's own duration.

## MODIFIED

- **Content directory scan** — previously recognized only image extensions and produced a `Slide` with a thumbnail plus size/mtime → now also recognizes `.mp4` and, for video files, produces a `Slide` whose thumbnail is the first frame and which additionally carries the video's exact duration.
- **Slide auto-advance timing** — previously every slide advances on a single fixed global ticker (`interval`) → image slides continue to use the fixed global `interval` unchanged; video slides instead advance when their own playback reaches its exact duration.
- **Slide rendering** — previously every slide is decoded and fit to the window as a single static image (`decodeAndFit`) → a video slide instead renders as playing video content, fit to the window the same way, with the first-frame image used only as the instant-nav thumbnail and while the video is preparing to play.
- **Resume-from-pause redisplay** — previously resuming redisplays the current slide's thumbnail then full image → for a video slide, resuming redisplays it from the beginning (thumbnail, then playback from frame zero), not from wherever playback previously stopped.

## REMOVED

- None.
