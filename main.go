package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	headless := flag.Bool("headless", false, "use the headless (PNG-dump) renderer for development off the sign, instead of DRM")
	flag.Parse()

	contentDir := "content"
	if flag.NArg() > 0 {
		contentDir = flag.Arg(0)
	}

	configPath := filepath.Join(contentDir, "config.toml")
	cfg, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	// Apply defaults for unset slideshow settings.
	interval := time.Duration(cfg.Slideshow.Interval)
	if interval == 0 {
		interval = 30 * time.Second
	}
	thumbWidth := cfg.Slideshow.ThumbWidth
	if thumbWidth == 0 {
		thumbWidth = 80
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		renderer    Renderer
		drmRenderer *DRMRenderer
		vt          *VT
		vtEvents    <-chan vtEvent
		width       int
		height      int
	)

	if *headless {
		hr, w, h := NewHeadlessRenderer()
		renderer, width, height = hr, w, h
	} else {
		// No fallback to headless on failure: a missing/unopenable
		// /dev/dri, a permission error, or another process already
		// holding DRM master must fail loudly on the sign, not silently
		// degrade to a mode that draws nothing anyone sees.
		dr, w, h, err := OpenDRMRenderer("/dev/dri/card0")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening DRM display: %v\n", err)
			os.Exit(1)
		}
		drmRenderer, renderer, width, height = dr, dr, w, h

		vt, err = OpenVT()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening controlling tty: %v\n", err)
			os.Exit(1)
		}
		vtEvents, err = vt.WatchSwitches(ctx, drmRenderer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error watching VT switches: %v\n", err)
			os.Exit(1)
		}
		vt.HandleShutdownSignals(ctx, cancel)
	}

	inputWatcher, err := NewInputWatcher(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error watching input devices: %v\n", err)
		os.Exit(1)
	}

	cec := NewCEC()
	ss := NewSlideshow(ctx, contentDir, interval, thumbWidth, cec, renderer, width, height)

	sched := NewSchedule(configPath, cfg.Schedule, ss.Reload, ss.Pause)
	ss.startPaused = !sched.IsOn(time.Now())
	sched.Start()

	runErr := ss.Run(cancel, inputWatcher.Events(), vtEvents)

	if err := renderer.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "error closing renderer: %v\n", err)
	}
	if vt != nil {
		if err := vt.RestoreConsole(); err != nil {
			fmt.Fprintf(os.Stderr, "error restoring console: %v\n", err)
		}
		if err := vt.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing controlling tty: %v\n", err)
		}
	}

	if runErr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", runErr)
		os.Exit(1)
	}
}
