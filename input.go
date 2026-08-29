package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// NavKey is a recognized navigation input, decoded from a raw kernel key
// event.
type NavKey int

const (
	NavLeft  NavKey = iota // previous slide
	NavRight               // next slide
	NavHome                // jump to the first slide
	NavSleep               // sleep the display (Delete)
	NavQuit                // quit (Escape)
	NavWake                // any other recognized key-down; wakes a paused sign
)

// Linux key event type and the key codes this feature binds, from
// <linux/input-event-codes.h>.
const (
	evKey = 0x01

	keyEsc    = 1
	keyHome   = 102
	keyLeft   = 105
	keyRight  = 106
	keyDelete = 111
)

// parseInputEvent decodes one raw Linux input_event struct (from
// <linux/input.h>) into a NavKey. The struct's leading `struct timeval`
// field is 8 bytes on 32-bit builds (arm) and 16 bytes on 64-bit builds
// (amd64, arm64), but its trailing type/code/value fields are always the
// last 8 bytes of the struct regardless — so this reads from the end of
// raw rather than an assumed offset, making it correct on every
// architecture this project ships without needing a build tag.
//
// Kernel autorepeat (value == 2) is treated identically to an initial
// key-down (value == 1): both are key-down states, and a held nav key must
// keep advancing without lag rather than stalling after one step. Only
// value == 0 (key-up) and non-EV_KEY events return ok == false.
func parseInputEvent(raw []byte) (key NavKey, ok bool) {
	if len(raw) < 8 {
		return 0, false
	}
	tail := raw[len(raw)-8:]
	evType := binary.LittleEndian.Uint16(tail[0:2])
	code := binary.LittleEndian.Uint16(tail[2:4])
	value := int32(binary.LittleEndian.Uint32(tail[4:8]))

	if evType != evKey || (value != 1 && value != 2) {
		return 0, false
	}

	switch code {
	case keyLeft:
		return NavLeft, true
	case keyRight:
		return NavRight, true
	case keyHome:
		return NavHome, true
	case keyDelete:
		return NavSleep, true
	case keyEsc:
		return NavQuit, true
	default:
		return NavWake, true
	}
}

// inputEventRaw mirrors struct input_event's layout for the native build's
// word size: Go's int is 4 bytes on GOARCH=arm and 8 bytes on amd64/arm64,
// matching the width of C's `long` — and therefore of the kernel's
// `struct timeval` — on each of those Linux targets. Only used to size a
// single-event read buffer; parseInputEvent itself doesn't depend on this.
type inputEventRaw struct {
	Sec, Usec int
	Type      uint16
	Code      uint16
	Value     int32
}

var inputEventSize = int(unsafe.Sizeof(inputEventRaw{}))

// InputWatcher enumerates /dev/input/event* devices, watches /dev/input via
// inotify for hotplug, and forwards parsed NavKeys from every open device
// into one channel. It holds no pause-state logic of its own — it only
// recognizes and names keys.
type InputWatcher struct {
	events chan NavKey
}

// NewInputWatcher opens every currently-present /dev/input/event* device
// and starts watching for new ones. A nav device absent at startup is not
// an error: Mural must still start and run the schedule.
func NewInputWatcher(ctx context.Context) (*InputWatcher, error) {
	w := &InputWatcher{events: make(chan NavKey)}

	entries, err := os.ReadDir("/dev/input")
	if err != nil {
		return nil, fmt.Errorf("reading /dev/input: %w", err)
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "event") {
			continue
		}
		w.watchDevice(ctx, filepath.Join("/dev/input", e.Name()))
	}

	if err := w.watchHotplug(ctx); err != nil {
		// Not fatal: hotplug just won't be picked up, but devices present
		// at startup (or none at all) still work.
		log.Printf("input hotplug watch: %v", err)
	}

	return w, nil
}

// Events returns the channel every open input device's parsed NavKeys are
// forwarded into.
func (w *InputWatcher) Events() <-chan NavKey {
	return w.events
}

// watchDevice opens path and, if successful, starts a goroutine forwarding
// its parsed key events into w.events until ctx is done or the device
// disappears (unplug), at which point its watcher is dropped quietly
// without affecting any other open device.
func (w *InputWatcher) watchDevice(ctx context.Context, path string) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		if os.IsPermission(err) {
			log.Printf("input device %s: permission denied (is this user in the 'input' group?): %v", path, err)
		}
		return
	}

	readerDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			f.Close()
		case <-readerDone:
		}
	}()

	go func() {
		defer close(readerDone)
		defer f.Close()
		buf := make([]byte, inputEventSize)
		for {
			n, err := f.Read(buf)
			if err != nil {
				return // unplugged, or ctx cancellation closed f — either way, stop quietly
			}
			key, ok := parseInputEvent(buf[:n])
			if !ok {
				continue
			}
			select {
			case w.events <- key:
			case <-ctx.Done():
				return
			}
		}
	}()
}

// watchHotplug watches /dev/input for newly created device nodes and opens
// each one via watchDevice as it appears.
func (w *InputWatcher) watchHotplug(ctx context.Context) error {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		return fmt.Errorf("inotify_init1: %w", err)
	}
	if _, err := unix.InotifyAddWatch(fd, "/dev/input", unix.IN_CREATE); err != nil {
		if cerr := unix.Close(fd); cerr != nil {
			log.Printf("closing inotify fd after failed add_watch: %v", cerr)
		}
		return fmt.Errorf("inotify_add_watch on /dev/input: %w", err)
	}

	go func() {
		<-ctx.Done()
		if err := unix.Close(fd); err != nil {
			log.Printf("closing inotify fd: %v", err)
		}
	}()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := unix.Read(fd, buf)
			if err != nil {
				return // ctx cancellation closed fd, or a fatal read error either way
			}
			for offset := 0; offset+unix.SizeofInotifyEvent <= n; {
				ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[offset]))
				nameStart := offset + unix.SizeofInotifyEvent
				nameEnd := nameStart + int(ev.Len)
				if nameEnd > n {
					break
				}
				name := string(bytes.TrimRight(buf[nameStart:nameEnd], "\x00"))
				offset = nameEnd
				if !strings.HasPrefix(name, "event") {
					continue
				}
				w.watchDevice(ctx, filepath.Join("/dev/input", name))
			}
		}
	}()

	return nil
}
