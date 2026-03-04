package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
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
		thumbs[i] = resize.Resize(80, 0, src, resize.Lanczos3)
	}
	return thumbs
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
	img := canvas.NewImageFromFile(paths[current])
	img.FillMode = canvas.ImageFillContain
	w.SetContent(container.NewStack(bg, img))

	var generation atomic.Int64

	show := func(index int) {
		generation.Add(1)
		current = index
		img.Image = nil
		img.File = paths[current]
		img.Refresh()
	}

	showFast := func(index int) {
		current = index
		gen := generation.Add(1)
		if thumbnails[index] != nil {
			img.File = ""
			img.Image = thumbnails[index]
			img.Refresh()
			go func() {
				if generation.Load() != gen {
					return
				}
				fyne.Do(func() {
					if generation.Load() != gen {
						return
					}
					img.Image = nil
					img.File = paths[index]
					img.Refresh()
				})
			}()
		} else {
			img.Image = nil
			img.File = paths[index]
			img.Refresh()
		}
	}

	ticker := time.NewTicker(*interval)
	go func() {
		for range ticker.C {
			fyne.Do(func() {
				show((current + 1) % len(paths))
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
