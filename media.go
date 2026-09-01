package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// parseMountinfo parses r in /proc/self/mountinfo format (proc(5)) and
// returns every mount point listed. The mount point is field index 4
// (0-based) on each line, and its position is fixed regardless of how many
// optional fields follow — the variable-length optional-field run and the
// "-" separator that terminates it both sit after the mount point, so
// locating "-" is only necessary for fields beyond it, which this function
// does not return.
//
// What does matter is escaping: the kernel encodes space, tab, newline, and
// backslash in the mount-point field as octal sequences (\040, \011, \012,
// \134) rather than emitting them literally, since mountinfo is
// whitespace-delimited. A stick labelled "MY STICK" mounts at
// "/media/mural/MY\040STICK" and must be unescaped or it is silently lost.
//
// Lines with fewer than 5 whitespace-separated fields are skipped rather
// than treated as an error — /proc files can be read mid-write by the
// kernel, and a torn line is not a reason to fail the whole parse.
func parseMountinfo(r io.Reader) ([]string, error) {
	var mounts []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}
		mounts = append(mounts, unescapeOctal(fields[4]))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return mounts, nil
}

// newMountPoints returns the entries present in cur and absent from prev,
// sorted for determinism.
func newMountPoints(prev, cur map[string]bool) []string {
	var added []string
	for mp := range cur {
		if !prev[mp] {
			added = append(added, mp)
		}
	}
	sort.Strings(added)
	return added
}

// underRoot reports whether mountPoint is strictly below root — a proper
// descendant, not root itself. Comparison is separator-aware
// (filepath.Clean plus an explicit trailing separator) rather than a raw
// string-prefix test, or a sibling directory sharing root as a string
// prefix — "/media/mural-backup" against root "/media/mural" — would be
// wrongly treated as a volume under root.
func underRoot(mountPoint, root string) bool {
	root = filepath.Clean(root)
	mountPoint = filepath.Clean(mountPoint)
	return strings.HasPrefix(mountPoint, root+string(os.PathSeparator))
}

// pollFallbackInterval is how many 1-second poll timeouts elapse between
// unconditional re-reads of mountinfo, independent of whether POLLPRI ever
// fires. This is deliberate insurance, not redundancy: if POLLPRI never
// fires on the target kernel, the feature would otherwise be silently
// inert rather than merely late. The cost is parsing ~40 lines of /proc
// every 5 seconds.
const pollFallbackInterval = 5

// MediaWatcher watches /proc/self/mountinfo for mount points appearing
// under root and reports each newly appeared one on its Events channel.
// See Architect.md "Mount detection" for why this polls mountinfo instead
// of using inotify on the media directory.
type MediaWatcher struct {
	events chan string
}

// NewMediaWatcher starts watching for mount points under root. An empty
// root disables the watcher outright: no goroutine is started, no file is
// opened, and Events() never fires.
//
// A root that does not exist is logged as a warning, not a startup
// failure, and the watcher runs anyway: detection reads
// /proc/self/mountinfo, not the root directory, so a nonexistent root
// simply means no line ever matches underRoot — there is nothing to guard
// against, and refusing to watch would leave a Mural started before an
// installer run (or before the directory is restored) latched into
// permanent inertness until a restart.
func NewMediaWatcher(ctx context.Context, root string) (*MediaWatcher, error) {
	w := &MediaWatcher{events: make(chan string)}
	if root == "" {
		return w, nil
	}

	if _, err := os.Stat(root); err != nil {
		log.Printf("media watcher: root %s: %v (will keep watching; mount points appearing under it are still detected)", root, err)
	}

	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("opening /proc/self/mountinfo: %w", err)
	}

	go w.watch(ctx, f, root)
	return w, nil
}

// Events returns the channel newly appeared mount points under root are
// sent on.
func (w *MediaWatcher) Events() <-chan string {
	return w.events
}

// watch blocks in unix.Poll waiting for mount-table changes and diffs
// mountinfo against the previously observed set on each wake, sending
// newly appeared mount points under root on w.events.
//
// Three properties here are load-bearing (Architect.md "Mount detection"):
//   - The poll timeout is finite (1s), not -1: a -1 poll blocks forever and
//     ctx.Done() cannot interrupt it, and closing the fd from another
//     goroutine — input.go's cancellation shape — does not reliably wake a
//     thread blocked in poll(2). So this loop polls with a timeout and
//     checks ctx.Err() on every expiry instead.
//   - EINTR is retried, not treated as an error: the Go runtime delivers
//     SIGURG for async preemption, which interrupts blocking syscalls as a
//     matter of routine. Treating it as fatal would kill the watcher within
//     minutes of normal running, indistinguishable from the feature never
//     having been installed.
//   - Progress does not depend on POLLPRI firing: every pollFallbackInterval
//     timeouts, the loop re-reads and diffs regardless of what unix.Poll
//     reported.
func (w *MediaWatcher) watch(ctx context.Context, f *os.File, root string) {
	defer f.Close()

	tracked := map[string]bool{}
	fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLPRI}}
	timeouts := 0

	for {
		n, err := unix.Poll(fds, 1000)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			log.Printf("media watcher: poll: %v", err)
			return
		}

		if ctx.Err() != nil {
			return
		}

		if n == 0 {
			timeouts++
			if timeouts%pollFallbackInterval != 0 {
				continue
			}
		} else if fds[0].Revents&(unix.POLLPRI|unix.POLLERR) == 0 {
			continue // spurious wake
		} else {
			timeouts = 0
		}

		cur, err := w.readMountsUnder(f, root)
		if err != nil {
			log.Printf("media watcher: reading mountinfo: %v", err)
			continue
		}

		for _, mp := range newMountPoints(tracked, cur) {
			select {
			case w.events <- mp:
			case <-ctx.Done():
				return
			}
		}
		tracked = cur
	}
}

// readMountsUnder seeks f to the start (a seq_file such as
// /proc/self/mountinfo regenerates its contents from the seek, unlike a
// regular file) and returns the set of mount points under root.
func (w *MediaWatcher) readMountsUnder(f *os.File, root string) (map[string]bool, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	mounts, err := parseMountinfo(f)
	if err != nil {
		return nil, err
	}
	cur := make(map[string]bool, len(mounts))
	for _, mp := range mounts {
		if underRoot(mp, root) {
			cur[mp] = true
		}
	}
	return cur, nil
}

// unescapeOctal decodes the \NNN octal escape sequences /proc uses for
// space, tab, newline, and backslash in path fields. Any other backslash
// sequence (which should not occur in practice) is passed through
// unchanged rather than causing an error.
func unescapeOctal(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
