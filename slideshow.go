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

// Slide holds a content image path along with its cached thumbnail and the
// file stats used to detect whether the image has changed since last load.
type Slide struct {
	path  string
	thumb image.Image
	size  int64
	mtime time.Time
}

func loadThumbnail(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return nil
	}
	return resize.Resize(48, 0, src, resize.Lanczos3)
}

// scanSlides scans dir for images and returns a []Slide. existing slides whose
// path, size, and mtime are unchanged are reused as-is (thumbnail not reloaded).
func scanSlides(dir string, existing []Slide) ([]Slide, error) {
	entries, err := os.ReadDir(dir)
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
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !imageExts[ext] {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if old, ok := prev[path]; ok && old.size == info.Size() && old.mtime.Equal(info.ModTime()) {
			slides = append(slides, old)
			continue
		}
		slides = append(slides, Slide{
			path:  path,
			thumb: loadThumbnail(path),
			size:  info.Size(),
			mtime: info.ModTime(),
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

// Slideshow loads images from dir and displays them as a fullscreen slideshow.
type Slideshow struct {
	dir      string
	interval time.Duration

	// fields below are set during Run and accessed only on the Fyne main goroutine,
	// except via Pause/Reload which marshal through fyne.Do.
	slides     []Slide
	current    int
	paused     bool
	generation atomic.Int64
	img        *canvas.Image
	ticker     *time.Ticker
	winSize    func() fyne.Size

	// onResume is called (in a goroutine) when the user manually resumes from pause.
	onResume func()
}

func NewSlideshow(dir string, interval time.Duration) *Slideshow {
	return &Slideshow{dir: dir, interval: interval}
}

// SetOnResume sets a callback invoked when the user presses a key to wake the display.
// Intended for CEC TurnOn. Safe to call before Run.
func (s *Slideshow) SetOnResume(f func()) { s.onResume = f }

// Pause stops the slideshow and blacks out the display.
// Safe to call from any goroutine.
func (s *Slideshow) Pause() {
	fyne.Do(func() {
		s.paused = true
		s.generation.Add(1) // cancel any in-flight background load
		if s.ticker != nil {
			s.ticker.Stop()
		}
		if s.img != nil {
			s.img.Image = nil
			s.img.Refresh()
		}
	})
}

// resume un-pauses. Must be called from the Fyne main goroutine.
func (s *Slideshow) resume() {
	s.paused = false
	if s.ticker != nil {
		s.ticker.Reset(s.interval)
	}
	if s.img != nil {
		s.showFast(s.current)
	}
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

	slides, err := scanSlides(s.dir, existing)
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

// showFast must be called from the Fyne main goroutine.
func (s *Slideshow) showFast(index int) {
	s.current = index
	gen := s.generation.Add(1)
	if s.slides[index].thumb != nil {
		s.img.Image = s.slides[index].thumb
		s.img.Refresh()
	}
	path := s.slides[index].path
	winSize := s.winSize
	go func() {
		if s.generation.Load() != gen {
			return
		}
		sz := winSize()
		fitted, err := decodeAndFit(path, sz.Width, sz.Height)
		if err != nil || s.generation.Load() != gen {
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
	slides, err := scanSlides(s.dir, nil)
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
	s.ticker = time.NewTicker(s.interval)

	s.showFast(0)

	go func() {
		for range s.ticker.C {
			fyne.Do(func() {
				if s.paused {
					return
				}
				idx := (s.current + 1) % len(s.slides)
				sz := s.winSize()
				fitted, err := decodeAndFit(s.slides[idx].path, sz.Width, sz.Height)
				if err != nil {
					return
				}
				s.generation.Add(1)
				s.current = idx
				s.img.Image = fitted
				s.img.Refresh()
			})
		}
	}()

	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if ev.Name == fyne.KeyEscape {
			a.Quit()
			return
		}
		if s.paused {
			// Any nav key wakes the display.
			if s.onResume != nil {
				go s.onResume() // CEC TurnOn; run in goroutine as it's slow
			}
			s.resume()
			// fall through so the key also performs its nav action
		}
		n := len(s.slides)
		switch ev.Name {
		case fyne.KeyRight:
			s.showFast((s.current + 1) % n)
			s.ticker.Reset(s.interval)
		case fyne.KeyLeft:
			s.showFast((s.current - 1 + n) % n)
			s.ticker.Reset(s.interval)
		case fyne.KeyHome:
			s.showFast(0)
			s.ticker.Reset(s.interval)
		}
	})

	w.ShowAndRun()
	return nil
}
