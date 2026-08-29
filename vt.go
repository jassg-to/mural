package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Raw VT (virtual terminal) ioctl numbers and the vt_mode struct layout,
// from <linux/vt.h>. Unlike DRM's ioctls, these are legacy fixed request
// numbers rather than ones generated via the generic _IOWR macros.
const (
	vtSetMode = 0x5602
	vtRelDisp = 0x5605

	vtModeProcess = 1
	vtAckAcq      = 2

	kdSetMode = 0x4B3A
	kdText    = 0x00
)

// vtMode mirrors struct vt_mode.
type vtMode struct {
	Mode   byte
	Waitv  byte
	Relsig int16
	Acqsig int16
	Frsig  int16
}

// vtEvent is fed into the run loop whenever ownership of the display
// changes hands with another VT.
type vtEvent int

const (
	vtEventReleased vtEvent = iota
	vtEventAcquired
)

// VT owns the process's controlling terminal, which VT-switch and
// console-mode ioctls act on.
type VT struct {
	f *os.File
}

// OpenVT opens the process's controlling terminal.
func OpenVT() (*VT, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening controlling tty: %w", err)
	}
	return &VT{f: f}, nil
}

// ioctlInt issues an ioctl whose argument is a plain integer value rather
// than a pointer to a struct (VT_RELDISP is this shape; DRM's ioctls,
// handled by drm.go's ioctl, are not).
func ioctlInt(fd int, req, val uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, val)
	if errno != 0 {
		return errno
	}
	return nil
}

// WatchSwitches puts the controlling tty into VT_PROCESS mode and returns a
// channel that emits a vtEvent every time this VT is switched away from or
// back to. On release it drops r's DRM master; on reacquire it re-acquires
// master and re-sets the CRTC mode on r's currently front buffer. The
// returned channel is closed once ctx is done.
func (v *VT) WatchSwitches(ctx context.Context, r *DRMRenderer) (<-chan vtEvent, error) {
	mode := vtMode{
		Mode:   vtModeProcess,
		Relsig: int16(syscall.SIGUSR1),
		Acqsig: int16(syscall.SIGUSR2),
	}
	if err := ioctl(int(v.f.Fd()), vtSetMode, unsafe.Pointer(&mode)); err != nil {
		return nil, fmt.Errorf("VT_SETMODE: %w", err)
	}

	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGUSR1, syscall.SIGUSR2)

	events := make(chan vtEvent, 1)
	go func() {
		defer close(events)
		defer signal.Stop(sigCh)
		for {
			select {
			case <-ctx.Done():
				return
			case sig := <-sigCh:
				var ev vtEvent
				switch sig {
				case syscall.SIGUSR1:
					if err := ioctl(r.fd, drmIoctlDropMaster, nil); err != nil {
						log.Printf("VT release: DROP_MASTER: %v", err)
					}
					if err := ioctlInt(int(v.f.Fd()), vtRelDisp, 1); err != nil {
						log.Printf("VT release: VT_RELDISP: %v", err)
					}
					ev = vtEventReleased
				case syscall.SIGUSR2:
					if err := ioctl(r.fd, drmIoctlSetMaster, nil); err != nil {
						log.Printf("VT acquire: SET_MASTER: %v", err)
					}
					if err := setCrtc(r.fd, r.crtcID, r.connID, r.buffers[r.front].fbID, r.mode); err != nil {
						log.Printf("VT acquire: SET_CRTC: %v", err)
					}
					if err := ioctlInt(int(v.f.Fd()), vtRelDisp, vtAckAcq); err != nil {
						log.Printf("VT acquire: VT_RELDISP: %v", err)
					}
					ev = vtEventAcquired
				default:
					continue
				}
				select {
				case events <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return events, nil
}

// HandleShutdownSignals calls cancel() on SIGTERM or SIGINT, tearing the
// run loop down via ctx.Done() rather than a dedicated quit channel (the
// same mechanism NavQuit uses). It does not itself call Close() or
// RestoreConsole: that graceful sequence — Close() the renderer, then
// RestoreConsole() — runs in main.go once Run() has actually returned, so
// the console is only restored after the DRM resources it would otherwise
// race against are already released.
func (v *VT) HandleShutdownSignals(ctx context.Context, cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		defer signal.Stop(sigCh)
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()
}

// RestoreConsole sets the controlling tty back to KD_TEXT, leaving a usable
// text console behind. Called on the graceful shutdown path only —
// SIGKILL cannot be handled, and relies on the kernel's own fd-close
// teardown instead (see Architect.md).
func (v *VT) RestoreConsole() error {
	if err := ioctlInt(int(v.f.Fd()), kdSetMode, kdText); err != nil {
		return fmt.Errorf("KDSETMODE(KD_TEXT): %w", err)
	}
	return nil
}

// Close releases the controlling-tty file descriptor.
func (v *VT) Close() error {
	return v.f.Close()
}
