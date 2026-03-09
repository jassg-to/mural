package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// TimeOfDay is minutes since midnight, parsed from "HH:MM".
type TimeOfDay int

// UnmarshalText implements encoding.TextUnmarshaler for "HH:MM" strings.
func (t *TimeOfDay) UnmarshalText(text []byte) error {
	mins, err := parseHHMM(string(text))
	if err != nil {
		return err
	}
	*t = TimeOfDay(mins)
	return nil
}

// toTime returns the TimeOfDay as a time.Time on the given date.
func (t TimeOfDay) toTime(year int, month time.Month, day int, loc *time.Location) time.Time {
	return time.Date(year, month, day, int(t)/60, int(t)%60, 0, 0, loc)
}

// Window is an on/off pair, parsed from "HH:MM-HH:MM".
type Window struct {
	On  TimeOfDay
	Off TimeOfDay
}

// UnmarshalText implements encoding.TextUnmarshaler so TOML strings
// like "18:30-19:15" decode directly into a Window.
func (w *Window) UnmarshalText(text []byte) error {
	s := string(text)
	onStr, offStr, ok := strings.Cut(s, "-")
	if !ok {
		return fmt.Errorf("invalid window %q: missing '-'", s)
	}
	if err := w.On.UnmarshalText([]byte(onStr)); err != nil {
		return err
	}
	if err := w.Off.UnmarshalText([]byte(offStr)); err != nil {
		return err
	}
	return nil
}

// DayConfig holds the on/off windows for a single day of the week,
// grouped by occurrence within the month.
type DayConfig struct {
	All    []Window `toml:"all"`
	First  []Window `toml:"first"`
	Second []Window `toml:"second"`
	Third  []Window `toml:"third"`
	Fourth []Window `toml:"fourth"`
	Last   []Window `toml:"last"`
}

// windows returns the applicable windows for the given day of the month.
func (dc DayConfig) windows(year int, month time.Month, day int, weekday time.Weekday) []Window {
	wins := append([]Window{}, dc.All...)
	type occField struct {
		n    int
		list []Window
	}
	for _, of := range []occField{
		{1, dc.First},
		{2, dc.Second},
		{3, dc.Third},
		{4, dc.Fourth},
		{-1, dc.Last},
	} {
		if len(of.list) == 0 {
			continue
		}
		nth, ok := nthWeekdayOfMonth(year, month, weekday, of.n)
		if ok && nth.Day() == day {
			wins = append(wins, of.list...)
		}
	}
	return wins
}

// ScheduleConfig holds the per-day on/off windows and reload time.
type ScheduleConfig struct {
	ReloadTime *TimeOfDay `toml:"reload_time"` // HH:MM to auto-reload the schedule daily; defaults to "01:00"
	Monday     DayConfig  `toml:"monday"`
	Tuesday    DayConfig  `toml:"tuesday"`
	Wednesday  DayConfig  `toml:"wednesday"`
	Thursday   DayConfig  `toml:"thursday"`
	Friday     DayConfig  `toml:"friday"`
	Saturday   DayConfig  `toml:"saturday"`
	Sunday     DayConfig  `toml:"sunday"`
}

// SlideshowConfig holds slideshow display settings from the [slideshow] section.
type SlideshowConfig struct {
	Interval   Duration `toml:"interval"`    // time between slides, e.g. "30s", "1m"
	ThumbWidth uint     `toml:"thumb_width"` // thumbnail width in pixels
}

// Duration wraps time.Duration with TOML string unmarshalling.
type Duration time.Duration

func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

// Config is the top-level TOML structure for config.toml.
type Config struct {
	Schedule  ScheduleConfig  `toml:"schedule"`
	Slideshow SlideshowConfig `toml:"slideshow"`
}

func (cfg *ScheduleConfig) forWeekday(d time.Weekday) *DayConfig {
	switch d {
	case time.Monday:
		return &cfg.Monday
	case time.Tuesday:
		return &cfg.Tuesday
	case time.Wednesday:
		return &cfg.Wednesday
	case time.Thursday:
		return &cfg.Thursday
	case time.Friday:
		return &cfg.Friday
	case time.Saturday:
		return &cfg.Saturday
	default:
		return &cfg.Sunday
	}
}

// event is a single scheduled CEC action.
type event struct {
	at     time.Time
	turnOn bool
}

// Schedule fires on/off commands according to a TOML config file.
type Schedule struct {
	path      string
	cfg       ScheduleConfig
	mu        sync.RWMutex
	onTurnOn  func()
	onTurnOff func()
}

// LoadConfig parses the config.toml file at path and returns the full Config.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// NewSchedule creates a Schedule from a ScheduleConfig.
// onTurnOn is called at scheduled turn-on (use ss.Reload to reload content and un-pause).
// onTurnOff is called at scheduled turn-off (use ss.Pause to blank the display).
func NewSchedule(path string, cfg ScheduleConfig, onTurnOn func(), onTurnOff func()) *Schedule {
	return &Schedule{path: path, cfg: cfg, onTurnOn: onTurnOn, onTurnOff: onTurnOff}
}

// reload re-reads config.toml from disk and updates the schedule config atomically.
func (s *Schedule) reload() error {
	cfg, err := LoadConfig(s.path)
	if err != nil {
		return fmt.Errorf("reloading config: %w", err)
	}
	s.mu.Lock()
	s.cfg = cfg.Schedule
	s.mu.Unlock()
	log.Printf("schedule: reloaded from %s", s.path)
	return nil
}

// Start launches a background goroutine that fires CEC commands at the scheduled times.
// A second goroutine reloads the schedule file daily at the configured reload_hour (default 1am).
func (s *Schedule) Start() {
	go func() {
		for {
			now := time.Now()
			s.mu.RLock()
			rt := s.cfg.ReloadTime
			s.mu.RUnlock()
			defaultRT := TimeOfDay(60) // 01:00
			if rt == nil {
				rt = &defaultRT
			}
			year, month, day := now.Date()
			next := rt.toTime(year, month, day, now.Location())
			if !next.After(now) {
				next = next.AddDate(0, 0, 1)
			}
			time.Sleep(time.Until(next))
			if err := s.reload(); err != nil {
				log.Printf("schedule: auto-reload: %v", err)
			}
		}
	}()

	go func() {
		for {
			now := time.Now()
			events := s.eventsForDate(now)

			// drop events already in the past
			future := events[:0]
			for _, e := range events {
				if e.at.After(now) {
					future = append(future, e)
				}
			}

			// compute next midnight
			tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.Local)

			for _, e := range future {
				time.Sleep(time.Until(e.at))
				if e.turnOn {
					s.onTurnOn()
				} else {
					s.onTurnOff()
				}
			}

			time.Sleep(time.Until(tomorrow))
		}
	}()
}

// IsOn reports whether the display should be on at time t according to the schedule.
// It returns true if t falls within an active window, false otherwise.
func (s *Schedule) IsOn(t time.Time) bool {
	events := s.eventsForDate(t)
	on := false
	for _, e := range events {
		if e.at.After(t) {
			break
		}
		on = e.turnOn
	}
	return on
}

// eventsForDate returns the merged, sorted list of on/off events for the calendar day of t.
func (s *Schedule) eventsForDate(t time.Time) []event {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	year, month, day := t.Date()
	weekday := t.Weekday()
	dc := cfg.forWeekday(weekday)

	return windowsToEvents(t, dc.windows(year, month, day, weekday))
}

// windowsToEvents converts a slice of Window values into a sorted, merged
// list of events anchored to the calendar day of base.
func windowsToEvents(base time.Time, windows []Window) []event {
	if len(windows) == 0 {
		return nil
	}

	sorted := make([]Window, len(windows))
	copy(sorted, windows)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].On < sorted[j].On
	})

	// merge overlapping/adjacent windows
	merged := sorted[:1]
	for _, w := range sorted[1:] {
		last := &merged[len(merged)-1]
		if w.On <= last.Off {
			if w.Off > last.Off {
				last.Off = w.Off
			}
		} else {
			merged = append(merged, w)
		}
	}

	year, month, day := base.Date()
	loc := base.Location()

	var events []event
	for _, w := range merged {
		events = append(events, event{at: w.On.toTime(year, month, day, loc), turnOn: true})
		events = append(events, event{at: w.Off.toTime(year, month, day, loc), turnOn: false})
	}
	return events
}

// parseHHMM parses "HH:MM" and returns minutes since midnight.
func parseHHMM(s string) (int, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0, fmt.Errorf("invalid time %q: %w", s, err)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("time out of range: %q", s)
	}
	return h*60 + m, nil
}

// nthWeekdayOfMonth returns the date of the nth occurrence of weekday in year/month.
// n > 0: from start (1 = first); n < 0: from end (-1 = last).
// Returns ok=false if the occurrence doesn't exist in that month.
func nthWeekdayOfMonth(year int, month time.Month, weekday time.Weekday, n int) (time.Time, bool) {
	if n == 0 {
		return time.Time{}, false
	}
	if n > 0 {
		first := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
		diff := (int(weekday) - int(first.Weekday()) + 7) % 7
		day := first.AddDate(0, 0, diff+(n-1)*7)
		if day.Month() != month {
			return time.Time{}, false
		}
		return day, true
	}
	// n < 0
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.Local)
	diff := (int(last.Weekday()) - int(weekday) + 7) % 7
	day := last.AddDate(0, 0, -diff+(n+1)*7)
	if day.Month() != month {
		return time.Time{}, false
	}
	return day, true
}
