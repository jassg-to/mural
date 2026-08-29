package main

import (
	"fmt"
	"image"
	"log"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// dumbBuffer is one mapped DRM dumb buffer plus the framebuffer object
// wrapping it.
type dumbBuffer struct {
	handle uint32
	pitch  uint32
	size   uint64
	fbID   uint32
	data   []byte
}

// DRMRenderer presents composited frames directly to a DRM/KMS display via
// a double-buffered dumb-buffer swap chain. It implements Renderer.
type DRMRenderer struct {
	f      *os.File
	fd     int
	crtcID uint32
	connID uint32
	mode   drmModeModeInfo
	width  uint32
	height uint32

	buffers [2]dumbBuffer
	front   int // index of the buffer currently scanned out
}

// ioctl issues a raw ioctl(2) syscall against fd.
func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// OpenDRMRenderer acquires DRM master on devicePath, discovers the single
// connected display's preferred mode, and allocates a double-buffered
// dumb-buffer swap chain sized to it.
//
// Every resource acquired below has its cleanup deferred immediately after
// it succeeds, in acquisition order, guarded on the named err return — a
// failure partway through unwinds everything acquired so far rather than
// leaking DRM master or a dumb buffer.
func OpenDRMRenderer(devicePath string) (r *DRMRenderer, width, height int, err error) {
	f, operr := os.OpenFile(devicePath, os.O_RDWR, 0)
	if operr != nil {
		return nil, 0, 0, fmt.Errorf("opening %s: %w", devicePath, operr)
	}
	fd := int(f.Fd())
	defer func() {
		if err != nil {
			f.Close()
		}
	}()

	if ierr := ioctl(fd, drmIoctlSetMaster, nil); ierr != nil {
		return nil, 0, 0, fmt.Errorf("becoming DRM master on %s (already held by another process?): %w", devicePath, ierr)
	}
	defer func() {
		if err != nil {
			if derr := ioctl(fd, drmIoctlDropMaster, nil); derr != nil {
				log.Printf("DROP_MASTER during OpenDRMRenderer unwind on %s: %v", devicePath, derr)
			}
		}
	}()

	connID, crtcID, mode, derr := discoverDisplay(fd)
	if derr != nil {
		return nil, 0, 0, fmt.Errorf("discovering display on %s: %w", devicePath, derr)
	}
	w, h := uint32(mode.Hdisplay), uint32(mode.Vdisplay)

	buf0, berr := createDumbBuffer(fd, w, h)
	if berr != nil {
		return nil, 0, 0, fmt.Errorf("creating framebuffer 1 on %s: %w", devicePath, berr)
	}
	defer func() {
		if err != nil {
			destroyDumbBuffer(fd, buf0)
		}
	}()

	buf1, berr := createDumbBuffer(fd, w, h)
	if berr != nil {
		return nil, 0, 0, fmt.Errorf("creating framebuffer 2 on %s: %w", devicePath, berr)
	}
	defer func() {
		if err != nil {
			destroyDumbBuffer(fd, buf1)
		}
	}()

	if serr := setCrtc(fd, crtcID, connID, buf0.fbID, mode); serr != nil {
		return nil, 0, 0, fmt.Errorf("setting CRTC mode on %s: %w", devicePath, serr)
	}

	r = &DRMRenderer{
		f:       f,
		fd:      fd,
		crtcID:  crtcID,
		connID:  connID,
		mode:    mode,
		width:   w,
		height:  h,
		buffers: [2]dumbBuffer{buf0, buf1},
		front:   0,
	}
	return r, int(w), int(h), nil
}

// discoverDisplay finds the single connected connector, its preferred
// mode, and the CRTC already driving it.
func discoverDisplay(fd int) (connID, crtcID uint32, mode drmModeModeInfo, err error) {
	var res drmModeCardRes
	if ierr := ioctl(fd, drmIoctlModeGetResources, unsafe.Pointer(&res)); ierr != nil {
		return 0, 0, drmModeModeInfo{}, fmt.Errorf("GET_RESOURCES (counts): %w", ierr)
	}
	if res.CountConnectors == 0 {
		return 0, 0, drmModeModeInfo{}, fmt.Errorf("no DRM connectors found")
	}

	connIDs := make([]uint32, res.CountConnectors)
	res.ConnectorIDPtr = uint64(uintptr(unsafe.Pointer(&connIDs[0])))
	// Only interested in the connector list; suppress the other three
	// arrays so the kernel doesn't try to write through a null pointer.
	res.CrtcIDPtr, res.CountCrtcs = 0, 0
	res.EncoderIDPtr, res.CountEncoders = 0, 0
	res.FbIDPtr, res.CountFBs = 0, 0
	if ierr := ioctl(fd, drmIoctlModeGetResources, unsafe.Pointer(&res)); ierr != nil {
		return 0, 0, drmModeModeInfo{}, fmt.Errorf("GET_RESOURCES (connectors): %w", ierr)
	}
	runtime.KeepAlive(connIDs)

	for _, cid := range connIDs {
		var conn drmModeGetConnector
		conn.ConnectorID = cid
		if ierr := ioctl(fd, drmIoctlModeGetConnector, unsafe.Pointer(&conn)); ierr != nil {
			continue
		}
		if conn.Connection != drmModeConnected || conn.CountModes == 0 {
			continue
		}

		modes := make([]drmModeModeInfo, conn.CountModes)
		conn.ModesPtr = uint64(uintptr(unsafe.Pointer(&modes[0])))
		conn.EncodersPtr, conn.CountEncoders = 0, 0
		conn.PropsPtr, conn.CountProps = 0, 0
		conn.PropValuesPtr = 0
		conn.ConnectorID = cid
		if ierr := ioctl(fd, drmIoctlModeGetConnector, unsafe.Pointer(&conn)); ierr != nil {
			return 0, 0, drmModeModeInfo{}, fmt.Errorf("GET_CONNECTOR (modes) for connector %d: %w", cid, ierr)
		}
		runtime.KeepAlive(modes)

		if conn.EncoderID == 0 {
			return 0, 0, drmModeModeInfo{}, fmt.Errorf("connector %d is connected but has no active encoder", cid)
		}
		var enc drmModeGetEncoder
		enc.EncoderID = conn.EncoderID
		if ierr := ioctl(fd, drmIoctlModeGetEncoder, unsafe.Pointer(&enc)); ierr != nil {
			return 0, 0, drmModeModeInfo{}, fmt.Errorf("GET_ENCODER %d: %w", conn.EncoderID, ierr)
		}
		if enc.CrtcID == 0 {
			return 0, 0, drmModeModeInfo{}, fmt.Errorf("encoder %d has no assigned CRTC", conn.EncoderID)
		}

		return cid, enc.CrtcID, pickPreferredMode(modes), nil
	}

	return 0, 0, drmModeModeInfo{}, fmt.Errorf("no connected DRM connector with a usable mode found")
}

// setCrtc points crtcID at fbID, driving connID with mode.
func setCrtc(fd int, crtcID, connID, fbID uint32, mode drmModeModeInfo) error {
	connIDs := []uint32{connID}
	req := drmModeCrtc{
		SetConnectorsPtr: uint64(uintptr(unsafe.Pointer(&connIDs[0]))),
		CountConnectors:  1,
		CrtcID:           crtcID,
		FbID:             fbID,
		ModeValid:        1,
		Mode:             mode,
	}
	err := ioctl(fd, drmIoctlModeSetCrtc, unsafe.Pointer(&req))
	runtime.KeepAlive(connIDs)
	return err
}

// createDumbBuffer allocates, wraps in a framebuffer object, and mmaps one
// dumb buffer sized width×height at 32bpp. On any failure it unwinds
// whatever it already acquired before returning the error.
func createDumbBuffer(fd int, width, height uint32) (dumbBuffer, error) {
	create := drmModeCreateDumb{Width: width, Height: height, Bpp: 32}
	if err := ioctl(fd, drmIoctlModeCreateDumb, unsafe.Pointer(&create)); err != nil {
		return dumbBuffer{}, fmt.Errorf("CREATE_DUMB: %w", err)
	}

	addfb := drmModeFBCmd{Width: width, Height: height, Pitch: create.Pitch, Bpp: 32, Depth: 24, Handle: create.Handle}
	if err := ioctl(fd, drmIoctlModeAddFB, unsafe.Pointer(&addfb)); err != nil {
		destroyDumbHandle(fd, create.Handle)
		return dumbBuffer{}, fmt.Errorf("ADD_FB: %w", err)
	}

	mapReq := drmModeMapDumb{Handle: create.Handle}
	if err := ioctl(fd, drmIoctlModeMapDumb, unsafe.Pointer(&mapReq)); err != nil {
		destroyDumbHandle(fd, create.Handle)
		return dumbBuffer{}, fmt.Errorf("MAP_DUMB: %w", err)
	}

	data, merr := unix.Mmap(fd, int64(mapReq.Offset), int(create.Size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if merr != nil {
		destroyDumbHandle(fd, create.Handle)
		return dumbBuffer{}, fmt.Errorf("mmap dumb buffer: %w", merr)
	}

	return dumbBuffer{handle: create.Handle, pitch: create.Pitch, size: create.Size, fbID: addfb.FbID, data: data}, nil
}

// destroyDumbBuffer unmaps and destroys buf. Used both for OpenDRMRenderer's
// own partial-failure unwind and by DRMRenderer.Close. Both call sites are
// best-effort cleanup with no error to propagate to, so failures are logged
// rather than returned.
func destroyDumbBuffer(fd int, buf dumbBuffer) {
	if buf.data != nil {
		if err := unix.Munmap(buf.data); err != nil {
			log.Printf("munmap dumb buffer (handle %d): %v", buf.handle, err)
		}
	}
	destroyDumbHandle(fd, buf.handle)
}

func destroyDumbHandle(fd int, handle uint32) {
	req := drmModeDestroyDumb{Handle: handle}
	if err := ioctl(fd, drmIoctlModeDestroyDumb, unsafe.Pointer(&req)); err != nil {
		log.Printf("DESTROY_DUMB (handle %d): %v", handle, err)
	}
}

// Present composites frame (or black, if frame is nil) into the back
// buffer, converts it to the hardware's XRGB8888 layout, and page-flips to
// it, waiting for the flip-complete event before returning. A page-flip
// failure — including one caused by no display being attached — is
// returned as an ordinary error: no special detection, no panic, no retry
// here. The caller (run loop) logs it and moves on; a later reconnect is
// picked up reactively the next time Present is called.
func (r *DRMRenderer) Present(frame *image.RGBA) error {
	src := frame
	if src == nil {
		src = compositeLetterboxed(nil, int(r.width), int(r.height))
	}

	back := 1 - r.front
	buf := &r.buffers[back]
	rgbaToXRGB8888(buf.data, int(buf.pitch), src)

	flip := drmModeCrtcPageFlip{
		CrtcID: r.crtcID,
		FbID:   buf.fbID,
		Flags:  drmModePageFlipEvent,
	}
	if err := ioctl(r.fd, drmIoctlModePageFlip, unsafe.Pointer(&flip)); err != nil {
		return fmt.Errorf("page flip: %w", err)
	}
	if err := waitForFlipEvent(r.fd); err != nil {
		return fmt.Errorf("waiting for page flip completion: %w", err)
	}

	r.front = back
	return nil
}

// Close unmaps and destroys both dumb buffers and drops DRM master.
func (r *DRMRenderer) Close() error {
	for _, buf := range r.buffers {
		destroyDumbBuffer(r.fd, buf)
	}
	if err := ioctl(r.fd, drmIoctlDropMaster, nil); err != nil {
		return fmt.Errorf("DROP_MASTER: %w", err)
	}
	return nil
}

// waitForFlipEvent blocks on fd until a DRM_EVENT_FLIP_COMPLETE event is
// read back, per the drm_event/drm_event_vblank framing the kernel writes
// in response to a page flip requested with DRM_MODE_PAGE_FLIP_EVENT.
func waitForFlipEvent(fd int) error {
	buf := make([]byte, 1024)
	for {
		n, err := unix.Read(fd, buf)
		if err != nil {
			return fmt.Errorf("reading DRM event: %w", err)
		}
		for off := 0; off+int(unsafe.Sizeof(drmEvent{})) <= n; {
			ev := (*drmEvent)(unsafe.Pointer(&buf[off]))
			if ev.Length == 0 || off+int(ev.Length) > n {
				break
			}
			if ev.Type == drmEventFlipComplete {
				return nil
			}
			off += int(ev.Length)
		}
	}
}
