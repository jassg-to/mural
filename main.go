package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"image/color"

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
			continue // SVGs and unsupported formats get nil
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

func main() {
	interval := flag.Duration("interval", 30*time.Second, "time between slides")
	flag.Parse()

	paths, err := loadImagePaths("content")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "error: no images found in content/")
		os.Exit(1)
	}

	thumbnails := loadThumbnails(paths)

	a := app.New()
	w := a.NewWindow("Mural Digital")
	w.Resize(fyne.NewSize(800, 450))
	w.SetPadded(false)

	bg := canvas.NewRectangle(color.Black)
	current := 0
	img := canvas.NewImageFromImage(thumbnails[current])
	img.FillMode = canvas.ImageFillContain
	w.SetContent(container.NewStack(bg, img))

	winSize := w.Canvas().Size
	var generation atomic.Int64

	showFast := func(index int) {
		current = index
		gen := generation.Add(1)
		if thumbnails[index] != nil {
			img.Image = thumbnails[index]
			img.Refresh()
		}
		go func() {
			if generation.Load() != gen {
				return
			}
			sz := winSize()
			fitted, err := decodeAndFit(paths[index], sz.Width, sz.Height)
			if err != nil || generation.Load() != gen {
				return
			}
			fyne.Do(func() {
				if generation.Load() != gen {
					return
				}
				img.Image = fitted
				img.Refresh()
			})
		}()
	}

	showFast(current)

	ticker := time.NewTicker(*interval)
	go func() {
		for range ticker.C {
			idx := (current + 1) % len(paths)
			sz := winSize()
			fitted, err := decodeAndFit(paths[idx], sz.Width, sz.Height)
			if err != nil {
				continue
			}
			fyne.Do(func() {
				generation.Add(1)
				current = idx
				img.Image = fitted
				img.Refresh()
			})
		}
	}()

	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		switch ev.Name {
		case fyne.KeyEscape:
			a.Quit()
		case fyne.KeyRight:
			showFast((current + 1) % len(paths))
			ticker.Reset(*interval)
		case fyne.KeyLeft:
			showFast((current - 1 + len(paths)) % len(paths))
			ticker.Reset(*interval)
		case fyne.KeyHome:
			showFast(0)
			ticker.Reset(*interval)
		}
	})

	w.ShowAndRun()
}
