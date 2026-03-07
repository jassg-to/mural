package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	interval     := flag.Duration("interval", 30*time.Second, "time between slides")
	scheduleFile := flag.String("schedule", "schedule.toml", "schedule config file")
	flag.Parse()

	cec := NewCEC()
	ss := NewSlideshow("content", *interval)

	sched, err := LoadSchedule(*scheduleFile, cec, ss.Reload)
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
