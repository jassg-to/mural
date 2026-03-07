package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/BurntSushi/toml"
)

// Window is an on/off time pair in HH:MM format.
type Window struct {
	On  string `toml:"on"`
	Off string `toml:"off"`
}

// WeekdayConfig holds the default on/off windows for each day of the week.
type WeekdayConfig struct {
	Monday    []Window `toml:"monday"`
	Tuesday   []Window `toml:"tuesday"`
	Wednesday []Window `toml:"wednesday"`
	Thursday  []Window `toml:"thursday"`
	Friday    []Window `toml:"friday"`
	Saturday  []Window `toml:"saturday"`
	Sunday    []Window `toml:"sunday"`
}

func (wc *WeekdayConfig) forWeekday(d time.Weekday) []Window {
	switch d {
	case time.Monday:
		return wc.Monday
	case time.Tuesday:
		return wc.Tuesday
	case time.Wednesday:
		return wc.Wednesday
	case time.Thursday:
		return wc.Thursday
	case time.Friday:
		return wc.Friday
	case time.Saturday:
		return wc.Saturday
	default:
		return wc.Sunday
	}
}

// SpecialRule adds extra on/off windows on specific occurrences of a weekday within a month.
type SpecialRule struct {
	Name       string   `toml:"name"`
	Weekday    string   `toml:"weekday"`    // "Monday", "Tuesday", …
	Occurrence int      `toml:"occurrence"` // 1 = first, -1 = last, etc.
	Windows    []Window `toml:"windows"`
}

// ScheduleConfig is the top-level TOML structure.
type ScheduleConfig struct {
	Weekday WeekdayConfig `toml:"weekday"`
	Special []SpecialRule `toml:"special"`
}

// event is a single scheduled CEC action.
type event struct {
	at     time.Time
	turnOn bool
}

// Schedule fires CEC on/off commands according to a TOML config file.
type Schedule struct {
	cfg       ScheduleConfig
	cec       *CEC
	onTurnOn  func()
	onTurnOff func()
}

// LoadSchedule parses the TOML file at path and returns a Schedule.
// onTurnOn is called before CEC TurnOn (use ss.Reload to reload content and un-pause).
// onTurnOff is called before CEC TurnOff (use ss.Pause to blank the display).
func LoadSchedule(path string, cec *CEC, onTurnOn func(), onTurnOff func()) (*Schedule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading schedule: %w", err)
	}
	var cfg ScheduleConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing schedule: %w", err)
	}
	return &Schedule{cfg: cfg, cec: cec, onTurnOn: onTurnOn, onTurnOff: onTurnOff}, nil
}

// Start launches a background goroutine that fires CEC commands at the scheduled times.
func (s *Schedule) Start() {
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
					if err := s.cec.TurnOn(); err != nil {
						log.Printf("schedule: CEC TurnOn: %v", err)
					}
				} else {
					s.onTurnOff()
					if err := s.cec.TurnOff(); err != nil {
						log.Printf("schedule: CEC TurnOff: %v", err)
					}
				}
			}

			time.Sleep(time.Until(tomorrow))
		}
	}()
}

// eventsForDate returns the merged, sorted list of on/off events for the calendar day of t.
func (s *Schedule) eventsForDate(t time.Time) []event {
	year, month, day := t.Date()
	weekday := t.Weekday()

	windows := append([]Window{}, s.cfg.Weekday.forWeekday(weekday)...)

	for _, rule := range s.cfg.Special {
		ruleDay, ok := parseWeekday(rule.Weekday)
		if !ok {
			log.Printf("schedule: unknown weekday %q in special rule %q", rule.Weekday, rule.Name)
			continue
		}
		nth, ok := nthWeekdayOfMonth(year, month, ruleDay, rule.Occurrence)
		if ok && nth.Day() == day {
			windows = append(windows, rule.Windows...)
		}
	}

	return windowsToEvents(t, windows)
}

// windowsToEvents converts a slice of Window values into a sorted, merged list of events
// anchored to the calendar day of base.
func windowsToEvents(base time.Time, windows []Window) []event {
	type interval struct{ on, off int } // minutes since midnight

	var intervals []interval
	for _, w := range windows {
		on, err1 := parseHHMM(w.On)
		off, err2 := parseHHMM(w.Off)
		if err1 != nil || err2 != nil {
			log.Printf("schedule: invalid window %q-%q: %v %v", w.On, w.Off, err1, err2)
			continue
		}
		intervals = append(intervals, interval{on, off})
	}

	if len(intervals) == 0 {
		return nil
	}

	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].on < intervals[j].on
	})

	// merge overlapping/adjacent intervals
	merged := intervals[:1]
	for _, iv := range intervals[1:] {
		last := &merged[len(merged)-1]
		if iv.on <= last.off {
			if iv.off > last.off {
				last.off = iv.off
			}
		} else {
			merged = append(merged, iv)
		}
	}

	year, month, day := base.Date()
	loc := base.Location()

	var events []event
	for _, iv := range merged {
		onTime := time.Date(year, month, day, iv.on/60, iv.on%60, 0, 0, loc)
		offTime := time.Date(year, month, day, iv.off/60, iv.off%60, 0, 0, loc)
		events = append(events, event{at: onTime, turnOn: true})
		events = append(events, event{at: offTime, turnOn: false})
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

// parseWeekday maps a weekday name to time.Weekday.
func parseWeekday(s string) (time.Weekday, bool) {
	switch s {
	case "Sunday":
		return time.Sunday, true
	case "Monday":
		return time.Monday, true
	case "Tuesday":
		return time.Tuesday, true
	case "Wednesday":
		return time.Wednesday, true
	case "Thursday":
		return time.Thursday, true
	case "Friday":
		return time.Friday, true
	case "Saturday":
		return time.Saturday, true
	default:
		return 0, false
	}
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
