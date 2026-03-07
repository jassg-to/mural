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

func loadImagePaths(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading content directory: %w", err)
	}

	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if imageExts[ext] {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	return paths, nil
}

func loadThumbnails(paths []string) []image.Image {
	thumbs := make([]image.Image, len(paths))
	for i, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		src, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			continue
		}
		thumbs[i] = resize.Resize(48, 0, src, resize.Lanczos3)
	}
	return thumbs
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
	paths      []string
	thumbnails []image.Image
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
	paths, err := loadImagePaths(s.dir)
	if err != nil {
		log.Printf("slideshow reload: %v", err)
		return
	}
	if len(paths) == 0 {
		log.Printf("slideshow reload: no images found in %s", s.dir)
		return
	}
	thumbs := loadThumbnails(paths)
	fyne.Do(func() {
		s.paths = paths
		s.thumbnails = thumbs
		s.current = 0
		s.resume()
	})
}

// showFast must be called from the Fyne main goroutine.
func (s *Slideshow) showFast(index int) {
	s.current = index
	gen := s.generation.Add(1)
	if s.thumbnails[index] != nil {
		s.img.Image = s.thumbnails[index]
		s.img.Refresh()
	}
	paths := s.paths
	winSize := s.winSize
	go func() {
		if s.generation.Load() != gen {
			return
		}
		sz := winSize()
		fitted, err := decodeAndFit(paths[index], sz.Width, sz.Height)
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
	paths, err := loadImagePaths(s.dir)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no images found in %s", s.dir)
	}

	s.paths = paths
	s.thumbnails = loadThumbnails(paths)

	a := app.New()
	w := a.NewWindow("Mural Digital")
	w.Resize(fyne.NewSize(800, 450))
	w.SetPadded(false)

	bg := canvas.NewRectangle(color.Black)
	s.img = canvas.NewImageFromImage(s.thumbnails[0])
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
				idx := (s.current + 1) % len(s.paths)
				sz := s.winSize()
				fitted, err := decodeAndFit(s.paths[idx], sz.Width, sz.Height)
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
		n := len(s.paths)
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
