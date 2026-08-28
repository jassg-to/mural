package main

import (
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
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"github.com/nfnt/resize"
)

var imageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
}

// isImageExt reports whether ext (case-insensitive) is a recognized image
// file extension.
func isImageExt(ext string) bool {
	return imageExts[strings.ToLower(ext)]
}

// Slide holds a content file's path along with its cached thumbnail and the
// file stats used to detect whether the file has changed since last load.
type Slide struct {
	path  string
	thumb image.Image
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

// scanSlides scans dir for images and returns a []Slide. existing slides
// whose path, size, and mtime are unchanged are reused as-is, without
// re-decoding a thumbnail.
//
// existing is a snapshot passed in by the caller and never mutated in
// place: scanSlides runs from Run() on the Fyne main goroutine and from
// Reload() on a background goroutine, so a shared mutable map would be a
// concurrent map write — an unrecoverable Go runtime fatal, not a benign
// race.
func (s *Slideshow) scanSlides(existing []Slide) ([]Slide, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("reading content directory: %w", err)
	}

	prev := make(map[string]Slide, len(existing))
	for _, sl := range existing {
		prev[sl.path] = sl
	}

	var slides []Slide
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if !isImageExt(ext) {
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

		// A corrupt image is still included in the rotation with a nil
		// thumbnail rather than being excluded (loadThumbnail returns nil
		// silently on decode failure).
		slides = append(slides, Slide{
			path:  path,
			thumb: loadThumbnail(path, s.thumbWidth),
			size:  size,
			mtime: mtime,
		})
	}
	return slides, nil
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

// Slideshow loads images from dir and displays them as a fullscreen
// slideshow.
type Slideshow struct {
	dir        string
	interval   time.Duration
	thumbWidth uint

	// fields below are set during Run and accessed only on the Fyne main goroutine,
	// except via Pause/Reload which marshal through fyne.Do.
	slides       []Slide
	current      int
	paused       bool
	generation   atomic.Int64
	img          *canvas.Image
	advanceTimer *time.Timer
	winSize      func() fyne.Size

	cec         *CEC
	startPaused bool
}

func NewSlideshow(dir string, interval time.Duration, thumbWidth uint, cec *CEC) *Slideshow {
	return &Slideshow{dir: dir, interval: interval, thumbWidth: thumbWidth, cec: cec}
}

// Pause stops the slideshow, blacks out the display, and turns off the
// connected display via CEC. Safe to call from any goroutine.
func (s *Slideshow) Pause() {
	fyne.Do(s.pause)
}

// pause blacks out the display, stops the advance timer, and sends CEC
// standby. Must be called from the Fyne main goroutine.
func (s *Slideshow) pause() {
	s.paused = true
	s.generation.Add(1) // cancel any in-flight background load
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

// resume un-pauses, redisplays the current slide, and sends CEC power-on.
// Must be called from the Fyne main goroutine.
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

// Reload rescans the content directory, resets to slide 0, and un-pauses.
// Safe to call from any goroutine.
func (s *Slideshow) Reload() {
	// Snapshot current slides on the main goroutine so scanSlides can reuse
	// unchanged entries without a lock.
	var existing []Slide
	fyne.Do(func() {
		existing = make([]Slide, len(s.slides))
		copy(existing, s.slides)
	})

	slides, err := s.scanSlides(existing)
	if err != nil {
		log.Printf("slideshow reload: %v", err)
		return
	}
	if len(slides) == 0 {
		log.Printf("slideshow reload: no images found in %s", s.dir)
		return
	}
	fyne.Do(func() {
		s.slides = slides
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
// Must be called from the Fyne main goroutine.
func (s *Slideshow) advanceToNext() {
	n := len(s.slides)
	idx := (s.current + 1) % n
	s.show(idx, false)
}

// show displays the slide at index and arms its auto-advance. Must be
// called from the Fyne main goroutine.
//
// instant=true shows the thumbnail immediately (manual nav, resume);
// instant=false swaps straight to the finished frame with no thumbnail
// flash (auto-advance) — this preserves pre-existing visible behaviour now
// that decoding always happens off the UI thread (previously the ticker
// goroutine decoded inline inside fyne.Do).
func (s *Slideshow) show(index int, instant bool) {
	s.current = index
	gen := s.generation.Add(1)
	sl := s.slides[index]

	if instant && sl.thumb != nil {
		s.img.Image = sl.thumb
		s.img.Refresh()
	}

	s.startImageDecode(sl.path, gen)
	s.scheduleAdvance(s.interval)
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

// Run loads images, opens the window, and blocks until the user quits.
func (s *Slideshow) Run() error {
	slides, err := s.scanSlides(nil)
	if err != nil {
		return err
	}
	if len(slides) == 0 {
		return fmt.Errorf("no images found in %s", s.dir)
	}

	s.slides = slides

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
		// pre-layout placeholder size if called inline here, which the
		// slide self-corrects at its next decode.
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
