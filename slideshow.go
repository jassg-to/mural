package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"github.com/nfnt/resize"
)

// minReplayGap is the minimum time between consecutive playback starts when
// a lone video slide advances to itself. It does not clamp or extend the
// clip's own duration; it only stops a sub-second clip from spawning
// ffmpeg dozens of times a second.
const minReplayGap = 1 * time.Second

var imageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
}

var videoExts = map[string]bool{
	".mp4": true,
}

type slideKindT int

const (
	slideKindImage slideKindT = iota
	slideKindVideo
)

// slideKind classifies a file extension (case-insensitive). ok is false for
// an extension that is neither a recognized image nor video type.
func slideKind(ext string) (kind slideKindT, ok bool) {
	ext = strings.ToLower(ext)
	switch {
	case imageExts[ext]:
		return slideKindImage, true
	case videoExts[ext]:
		return slideKindVideo, true
	default:
		return 0, false
	}
}

// Slide holds a content file's path along with its cached thumbnail and the
// file stats used to detect whether the file has changed since last load.
// duration/vidW/vidH are zero for image slides.
type Slide struct {
	path     string
	kind     slideKindT
	thumb    image.Image
	duration time.Duration
	vidW     int
	vidH     int
	size     int64
	mtime    time.Time
}

// rejectedEntry records a file that failed video probing/first-frame
// extraction, keyed the same way as the positive reuse cache, so an
// unchanged bad file is not re-probed on every scan.
type rejectedEntry struct {
	size  int64
	mtime time.Time
}

func loadThumbnail(path string, width uint) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return nil
	}
	return resize.Resize(width, 0, src, resize.Lanczos3)
}

// scanSlides scans dir for images and videos and returns a []Slide plus the
// updated negative-result (rejected) cache. existing slides and rejected
// entries whose path, size, and mtime are unchanged are reused as-is.
//
// existing and prevRejected are snapshots passed in by the caller and never
// mutated in place: scanSlides runs from Run() on the Fyne main goroutine
// and from Reload() on a background goroutine, so a shared mutable map
// would be a concurrent map write — an unrecoverable Go runtime fatal, not
// a benign race.
func (s *Slideshow) scanSlides(existing []Slide, prevRejected map[string]rejectedEntry) ([]Slide, map[string]rejectedEntry, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, nil, fmt.Errorf("reading content directory: %w", err)
	}

	prev := make(map[string]Slide, len(existing))
	for _, sl := range existing {
		prev[sl.path] = sl
	}

	rejected := make(map[string]rejectedEntry)

	var slides []Slide
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		kind, ok := slideKind(ext)
		if !ok {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		fi, err := e.Info()
		if err != nil {
			continue
		}
		size, mtime := fi.Size(), fi.ModTime()

		if old, ok := prev[path]; ok && old.size == size && old.mtime.Equal(mtime) {
			slides = append(slides, old)
			continue
		}
		if rej, ok := prevRejected[path]; ok && rej.size == size && rej.mtime.Equal(mtime) {
			rejected[path] = rej
			continue
		}

		switch kind {
		case slideKindVideo:
			vinfo, err := s.vid.probe(path)
			if err != nil {
				log.Printf("skipping video %s: %v", path, err)
				rejected[path] = rejectedEntry{size: size, mtime: mtime}
				continue
			}
			thumb, err := s.vid.firstFrame(path, s.thumbWidth)
			if err != nil {
				log.Printf("skipping video %s: %v", path, err)
				rejected[path] = rejectedEntry{size: size, mtime: mtime}
				continue
			}
			slides = append(slides, Slide{
				path:     path,
				kind:     slideKindVideo,
				thumb:    thumb,
				duration: vinfo.duration,
				vidW:     vinfo.width,
				vidH:     vinfo.height,
				size:     size,
				mtime:    mtime,
			})
		default:
			slides = append(slides, Slide{
				path:  path,
				kind:  slideKindImage,
				thumb: loadThumbnail(path, s.thumbWidth),
				size:  size,
				mtime: mtime,
			})
		}
	}
	return slides, rejected, nil
}

func decodeAndFit(path string, width, height float32) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening image: %w", err)
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}

	bounds := src.Bounds()
	imgW := float64(bounds.Dx())
	imgH := float64(bounds.Dy())
	scale := math.Min(float64(width)/imgW, float64(height)/imgH)
	targetW := uint(math.Round(imgW * scale))
	targetH := uint(math.Round(imgH * scale))

	return resize.Resize(targetW, targetH, src, resize.Lanczos3), nil
}

// advanceDuration returns how long a slide should remain on screen before
// auto-advancing: interval for an image slide, the slide's own probed
// duration for a video slide. interval must never govern a video slide.
// Pure function; the seam that makes advance-duration selection testable
// without a live Fyne canvas.
func advanceDuration(sl Slide, interval time.Duration) time.Duration {
	if sl.kind == slideKindVideo {
		return sl.duration
	}
	return interval
}

// Slideshow loads images and videos from dir and displays them as a
// fullscreen slideshow.
type Slideshow struct {
	dir        string
	interval   time.Duration
	thumbWidth uint

	// fields below are set during Run and accessed only on the Fyne main goroutine,
	// except via Pause/Reload which marshal through fyne.Do.
	slides        []Slide
	rejected      map[string]rejectedEntry
	current       int
	paused        bool
	generation    atomic.Int64
	img           *canvas.Image
	advanceTimer  *time.Timer
	playerCancel  context.CancelFunc
	lastShowStart time.Time
	winSize       func() fyne.Size

	cec         *CEC
	vid         *Video
	startPaused bool
}

func NewSlideshow(dir string, interval time.Duration, thumbWidth uint, cec *CEC, vid *Video) *Slideshow {
	return &Slideshow{dir: dir, interval: interval, thumbWidth: thumbWidth, cec: cec, vid: vid}
}

// Pause stops the slideshow, blacks out the display, and turns off the
// connected display via CEC. Safe to call from any goroutine.
func (s *Slideshow) Pause() {
	fyne.Do(s.pause)
}

// pause blacks out the display, stops any playback and the advance timer,
// and sends CEC standby. Must be called from the Fyne main goroutine.
func (s *Slideshow) pause() {
	s.paused = true
	s.generation.Add(1) // cancel any in-flight background load
	s.stopPlayback()
	if s.advanceTimer != nil {
		s.advanceTimer.Stop()
	}
	if s.img != nil {
		s.img.Image = nil
		s.img.Refresh()
	}
	go func() {
		if err := s.cec.TurnOff(); err != nil {
			log.Printf("CEC TurnOff: %v", err)
		}
	}()
}

// resume un-pauses, redisplays the current slide from the beginning (a
// video slide always restarts rather than resuming mid-playback, matching
// how image slides are redisplayed), and sends CEC power-on. Must be
// called from the Fyne main goroutine.
func (s *Slideshow) resume() {
	s.paused = false
	if s.img != nil {
		s.show(s.current, true)
	}
	go func() {
		if err := s.cec.TurnOn(); err != nil {
			log.Printf("CEC TurnOn: %v", err)
		}
	}()
}

// stopPlayback cancels any active video playback, killing the ffmpeg
// process, and clears playerCancel. Safe to call with no active playback.
// Must be called from the Fyne main goroutine — playerCancel is written
// only there; a caller on another goroutine must wrap this in fyne.Do.
func (s *Slideshow) stopPlayback() {
	if s.playerCancel != nil {
		s.playerCancel()
		s.playerCancel = nil
	}
}

// Reload rescans the content directory, resets to slide 0, and un-pauses.
// Safe to call from any goroutine.
func (s *Slideshow) Reload() {
	// Snapshot current slides/rejected set on the main goroutine so
	// scanSlides can reuse unchanged entries without a lock.
	var existing []Slide
	var prevRejected map[string]rejectedEntry
	fyne.Do(func() {
		existing = make([]Slide, len(s.slides))
		copy(existing, s.slides)
		prevRejected = s.rejected
	})

	slides, rejected, err := s.scanSlides(existing, prevRejected)
	if err != nil {
		log.Printf("slideshow reload: %v", err)
		return
	}
	if len(slides) == 0 {
		log.Printf("slideshow reload: no images found in %s", s.dir)
		return
	}
	fyne.Do(func() {
		// Stop any in-flight playback before the slide slice is replaced,
		// in the same main-goroutine critical section as the swap —
		// Reload runs on a background goroutine, so a bare stopPlayback()
		// here would race show()'s writes to playerCancel.
		s.stopPlayback()
		s.slides = slides
		s.rejected = rejected
		s.current = 0
		s.resume()
	})
}

// scheduleAdvance stops any pending advance timer and arms a new one that
// advances to the next slide after d elapses. Must be called from the Fyne
// main goroutine; advanceTimer is only ever touched there, so it needs no
// lock.
func (s *Slideshow) scheduleAdvance(d time.Duration) {
	if s.advanceTimer != nil {
		s.advanceTimer.Stop()
	}
	s.advanceTimer = time.AfterFunc(d, func() {
		fyne.Do(func() {
			if s.paused {
				return
			}
			s.advanceToNext()
		})
	})
}

// advanceToNext shows the next slide (auto-advance, no thumbnail flash).
// When the rotation has only one slide, it enforces minReplayGap between
// consecutive playback starts rather than clamping or extending the clip
// itself. Must be called from the Fyne main goroutine.
func (s *Slideshow) advanceToNext() {
	n := len(s.slides)
	idx := (s.current + 1) % n
	if n == 1 {
		if gap := minReplayGap - time.Since(s.lastShowStart); gap > 0 {
			s.advanceTimer = time.AfterFunc(gap, func() {
				fyne.Do(func() {
					if s.paused {
						return
					}
					s.show(idx, false)
				})
			})
			return
		}
	}
	s.show(idx, false)
}

// show displays the slide at index and arms its auto-advance. Must be
// called from the Fyne main goroutine.
//
// instant=true shows the thumbnail immediately (manual nav, resume);
// instant=false swaps straight to the finished frame with no thumbnail
// flash (auto-advance) — this preserves pre-existing visible behaviour now
// that decoding always happens off the UI thread (previously the ticker
// goroutine decoded inline inside fyne.Do). A video slide always shows its
// first-frame thumbnail while ffmpeg warms up, regardless of instant, per
// the Delta.
func (s *Slideshow) show(index int, instant bool) {
	s.stopPlayback()
	s.current = index
	s.lastShowStart = time.Now()
	gen := s.generation.Add(1)
	sl := s.slides[index]

	if (instant || sl.kind == slideKindVideo) && sl.thumb != nil {
		s.img.Image = sl.thumb
		s.img.Refresh()
	}

	d := advanceDuration(sl, s.interval)
	if sl.kind == slideKindVideo {
		s.startVideoPlayback(sl, gen)
		d += watchdogGrace
	} else {
		s.startImageDecode(sl.path, gen)
	}
	s.scheduleAdvance(d)
}

// startImageDecode decodes and fits path in the background, then swaps it
// in on the Fyne main goroutine if gen is still current.
func (s *Slideshow) startImageDecode(path string, gen int64) {
	winSize := s.winSize
	go func() {
		if s.generation.Load() != gen {
			return
		}
		sz := winSize()
		fitted, err := decodeAndFit(path, sz.Width, sz.Height)
		if err != nil {
			log.Printf("decoding %s: %v", path, err)
			return
		}
		if s.generation.Load() != gen {
			return
		}
		fyne.Do(func() {
			if s.generation.Load() != gen {
				return
			}
			s.img.Image = fitted
			s.img.Refresh()
		})
	}()
}

// startVideoPlayback starts sl's playback under a cancellable context
// stored in playerCancel, streaming frames into s.img through a
// single-slot latest-frame-wins handoff (Architect.md: Frame delivery) so
// that hardware which cannot keep up drops frames and stays real-time
// instead of queuing fyne.Do closures until it dies.
//
// A video slide advances at max(end-of-stream, probed duration): ffmpeg's
// -re pacing measurably finishes slightly early, so onDone alone would cut
// every clip short. If playback finishes before sl.duration has elapsed
// since the slide was shown, the advance is deferred to the duration mark
// instead of firing immediately.
func (s *Slideshow) startVideoPlayback(sl Slide, gen int64) {
	ctx, cancel := context.WithCancel(context.Background())
	s.playerCancel = cancel

	sz := s.winSize()
	w, h := fitDimensions(sl.vidW, sl.vidH, sz.Width, sz.Height)

	var mu sync.Mutex
	var pending image.Image
	var drainScheduled bool

	onFrame := func(frame image.Image) {
		mu.Lock()
		pending = frame
		alreadyScheduled := drainScheduled
		drainScheduled = true
		mu.Unlock()
		if alreadyScheduled {
			return
		}
		fyne.Do(func() {
			mu.Lock()
			f := pending
			pending = nil
			drainScheduled = false
			mu.Unlock()
			if s.generation.Load() != gen {
				return
			}
			s.img.Image = f
			s.img.Refresh()
		})
	}

	onDone := func(err error) {
		if err != nil && ctx.Err() == nil {
			log.Printf("video %s: playback: %v", sl.path, err)
		}
		fyne.Do(func() {
			if s.generation.Load() != gen || s.paused {
				return
			}
			remaining := sl.duration - time.Since(s.lastShowStart)
			if remaining <= 0 {
				s.advanceToNext()
				return
			}
			// Playback finished before the probed duration elapsed (the
			// common case, per measurement) — hold the last frame and
			// defer the advance to the duration mark instead of cutting
			// the clip short.
			if s.advanceTimer != nil {
				s.advanceTimer.Stop()
			}
			s.advanceTimer = time.AfterFunc(remaining, func() {
				fyne.Do(func() {
					if s.generation.Load() != gen || s.paused {
						return
					}
					s.advanceToNext()
				})
			})
		})
	}

	go s.vid.play(ctx, sl.path, w, h, onFrame, onDone)
}

// Run loads images and videos, opens the window, and blocks until the user quits.
func (s *Slideshow) Run() error {
	slides, rejected, err := s.scanSlides(nil, nil)
	if err != nil {
		return err
	}
	if len(slides) == 0 {
		return fmt.Errorf("no images found in %s", s.dir)
	}

	s.slides = slides
	s.rejected = rejected

	a := app.New()
	w := a.NewWindow("Mural Digital")
	w.Resize(fyne.NewSize(800, 450))
	w.SetPadded(false)

	bg := canvas.NewRectangle(color.Black)
	s.img = canvas.NewImageFromImage(s.slides[0].thumb)
	s.img.FillMode = canvas.ImageFillContain
	w.SetContent(container.NewStack(bg, s.img))

	s.winSize = w.Canvas().Size

	if s.startPaused {
		s.pause()
	} else {
		// Defer the initial show onto the event loop so it runs after the
		// first layout pass: w.Canvas().Size() still reports the
		// pre-layout placeholder size if called inline here, which an
		// image slide self-corrects at its next decode but a video slide
		// would bake into ffmpeg's scale filter for its entire playthrough.
		fyne.Do(func() {
			s.show(0, true)
		})
		go func() {
			if err := s.cec.TurnOn(); err != nil {
				log.Printf("CEC TurnOn (startup): %v", err)
			}
		}()
	}

	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		// Handle non-nav keys.
		switch ev.Name {
		case fyne.KeyEscape:
			a.Quit()
			return
		case fyne.KeyDelete:
			s.pause() // simulate schedule off
			return
		}
		// Any other key wakes the display.
		if s.paused {
			s.resume() // also sends CEC TurnOn
			// fall through so the key also performs its nav action
		}
		n := len(s.slides)
		switch ev.Name {
		case fyne.KeyRight:
			s.show((s.current+1)%n, true)
		case fyne.KeyLeft:
			s.show((s.current-1+n)%n, true)
		case fyne.KeyHome:
			s.show(0, true)
			go s.Reload()
		}
	})

	w.ShowAndRun()
	return nil
}
