package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

func main() {
	interval := flag.Duration("interval", 30*time.Second, "time between slides")
	contentDir := flag.String("content", "content", "directory containing images and schedule.toml")
	flag.Parse()

	cec := NewCEC()
	ss := NewSlideshow(*contentDir, *interval)
	ss.SetOnResume(func() {
		if err := cec.TurnOn(); err != nil {
			log.Printf("CEC TurnOn (manual resume): %v", err)
		}
	})

	sched, err := LoadSchedule(filepath.Join(*contentDir, "schedule.toml"), cec, ss.Reload, ss.Pause)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading schedule: %v\n", err)
		os.Exit(1)
	}
	sched.Start()

	if err := ss.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
