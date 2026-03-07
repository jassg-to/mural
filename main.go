package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	interval := flag.Duration("interval", 30*time.Second, "time between slides")
	contentDir := flag.String("content", "content", "directory containing images and schedule.toml")
	thumbWidth := flag.Uint("thumb-width", 80, "thumbnail width in pixels")
	flag.Parse()

	cec := NewCEC()
	ss := NewSlideshow(*contentDir, *interval, *thumbWidth, cec)

	sched, err := LoadSchedule(filepath.Join(*contentDir, "schedule.toml"), ss.Reload, ss.Pause)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading schedule: %v\n", err)
		os.Exit(1)
	}
	ss.startPaused = !sched.IsOn(time.Now())
	sched.Start()

	if err := ss.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
