package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"
)

func main() {
	headless := flag.Bool("headless", false, "use the headless (PNG-dump) renderer for development off the sign, instead of DRM")
	cpuprofile := flag.String("cpuprofile", "", "write a CPU profile covering the interactive run to this file")
	memprofile := flag.String("memprofile", "", "write a heap profile to this file on exit")
	// The default here must agree with install.sh's udev rule, which mounts
	// USB volumes under this same literal path — the two are independent
	// strings that have to be kept in sync by hand.
	mediaDir := flag.String("media-dir", "/media/mural", "root directory under which the installer's automounter mounts removable USB volumes; empty disables USB hotplug detection. Deliberately a flag, not a config.toml key: a stick's config replaces config.toml, so the one setting governing whether sticks are read at all must not itself be settable from a stick")
	flag.Parse()

	contentDir := "content"
	if flag.NArg() > 0 {
		contentDir = flag.Arg(0)
	}
	// Resolved once, up front: this feature adds unix.Statfs, os.Link, and
	// long os.Rename sequences against contentDir from a background
	// goroutine, possibly days after startup. A relative path would
	// silently retarget those at whatever the process's cwd happens to be
	// by then.
	absContentDir, err := filepath.Abs(contentDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving content directory: %v\n", err)
		os.Exit(1)
	}
	contentDir = absContentDir
	log.Printf("using content directory %s", contentDir)

	configPath := filepath.Join(contentDir, "config.toml")
	cfg, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	interval, thumbWidth := slideshowDefaults(cfg.Slideshow)

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

		// Not fatal: mural still owns DRM master and drives the display
		// either way. Without this the console keeps rendering kernel
		// messages underneath/behind mural's own frames, visible the
		// moment the console reappears at exit; RestoreConsole is what
		// undoes it. (Measured on hardware: this alone does not stop
		// keystroke echo — see DisableEcho below for that.)
		if err := vt.EnterGraphicsMode(); err != nil {
			log.Printf("entering console graphics mode: %v", err)
		}

		// Not fatal, same reasoning as above: this is what actually stops
		// physical keystrokes from being drawn on the console live while
		// mural runs (see DisableEcho's doc comment) — mural still works
		// without it, just with the visual bug this whole fix targets.
		if err := vt.DisableEcho(); err != nil {
			log.Printf("disabling console tty echo: %v", err)
		}
	}

	inputWatcher, err := NewInputWatcher(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error watching input devices: %v\n", err)
		os.Exit(1)
	}

	// Not fatal on failure: a platform where /proc/self/mountinfo can't be
	// opened (i.e. not Linux) must start and run normally with USB hotplug
	// detection inactive, per the Analyst's non-goal. An empty or
	// nonexistent *mediaDir is handled inside NewMediaWatcher itself and
	// never reaches this branch as an error.
	var mediaEvents <-chan string
	mediaWatcher, err := NewMediaWatcher(ctx, *mediaDir)
	if err != nil {
		log.Printf("media watcher: %v (USB hotplug detection disabled)", err)
	} else {
		mediaEvents = mediaWatcher.Events()
	}

	cec := NewCEC()
	ss := NewSlideshow(ctx, contentDir, interval, thumbWidth, cec, renderer, width, height)

	sched := NewSchedule(configPath, cfg.Schedule, ss.Reload, ss.Pause)
	// Assigned post-construction rather than passed into NewSlideshow: ss
	// is built before sched (sched's own constructor consumes ss.Reload),
	// so sched.ApplyConfig cannot exist yet as a constructor argument.
	// Following the ss.startPaused precedent below — a field set after
	// construction but before Run, so it never races the run loop.
	ss.onScheduleConfig = sched.ApplyConfig
	ss.startPaused = !sched.IsOn(time.Now())
	sched.Start()

	// Profiling brackets only the interactive run itself, not setup — so a
	// setup failure's os.Exit(1) above never needs to skip flushing it, and
	// the profile only contains the parts anyone actually wants to look at.
	var profFile *os.File
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating cpuprofile file: %v\n", err)
			os.Exit(1)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "error starting cpu profile: %v\n", err)
			os.Exit(1)
		}
		profFile = f
	}

	runErr := ss.Run(cancel, inputWatcher.Events(), vtEvents, mediaEvents)

	if profFile != nil {
		pprof.StopCPUProfile()
		if err := profFile.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing cpuprofile file: %v\n", err)
		}
	}

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

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating memprofile file: %v\n", err)
		} else {
			runtime.GC()
			if err := pprof.WriteHeapProfile(f); err != nil {
				fmt.Fprintf(os.Stderr, "error writing heap profile: %v\n", err)
			}
			if err := f.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "error closing memprofile file: %v\n", err)
			}
		}
	}

	if runErr != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", runErr)
		os.Exit(1)
	}
}
