package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

var imageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".bmp":  true,
	".svg":  true,
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

	a := app.New()
	w := a.NewWindow("Mural Digital")
	w.Resize(fyne.NewSize(800, 450))
	w.SetPadded(false)

	bg := canvas.NewRectangle(color.Black)
	current := 0
	img := canvas.NewImageFromFile(paths[current])
	img.FillMode = canvas.ImageFillContain
	w.SetContent(container.NewStack(bg, img))

	show := func(index int) {
		current = index
		img.File = paths[current]
		img.Refresh()
	}

	ticker := time.NewTicker(*interval)
	go func() {
		for range ticker.C {
			show((current + 1) % len(paths))
		}
	}()

	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		switch ev.Name {
		case fyne.KeyEscape:
			a.Quit()
		case fyne.KeyRight:
			show((current + 1) % len(paths))
			ticker.Reset(*interval)
		case fyne.KeyLeft:
			show((current - 1 + len(paths)) % len(paths))
			ticker.Reset(*interval)
		case fyne.KeyHome:
			show(0)
			ticker.Reset(*interval)
		}
	})

	w.ShowAndRun()
}
