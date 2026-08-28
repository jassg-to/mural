package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
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

	cec := NewCEC()
	ss := NewSlideshow(contentDir, interval, thumbWidth, cec)

	sched := NewSchedule(configPath, cfg.Schedule, ss.Reload, ss.Pause)
	ss.startPaused = !sched.IsOn(time.Now())
	sched.Start()

	if err := ss.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
