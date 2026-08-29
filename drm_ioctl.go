package main

import (
	"image"
	"unsafe"
)

// Raw Linux DRM ioctl request numbers and kernel-ABI struct layouts, from
// <drm/drm.h> and <drm/drm_mode.h>. No third-party Go DRM library exists
// (per Analyst.md's investigation), so this is hand-transcribed from the
// kernel UAPI headers. Struct field order and widths must match the C
// layout exactly — these are the wire format the kernel ioctl handler
// reads and writes.

const (
	// ioctl request-number encoding, from <asm-generic/ioctl.h>. Shared by
	// every Linux architecture this project targets (amd64, arm64, arm).
	iocNRBits   = 8
	iocTypeBits = 8
	iocSizeBits = 14

	iocNRShift   = 0
	iocTypeShift = iocNRShift + iocNRBits
	iocSizeShift = iocTypeShift + iocTypeBits
	iocDirShift  = iocSizeShift + iocSizeBits

	iocNone  = 0
	iocWrite = 1
	iocRead  = 2

	drmIoctlBase = 'd'
)

func ioc(dir, typ, nr, size uintptr) uintptr {
	return (dir << iocDirShift) | (typ << iocTypeShift) | (nr << iocNRShift) | (size << iocSizeShift)
}

func drmIO(nr uintptr) uintptr {
	return ioc(iocNone, drmIoctlBase, nr, 0)
}

func drmIOWR(nr uintptr, size uintptr) uintptr {
	return ioc(iocRead|iocWrite, drmIoctlBase, nr, size)
}

var (
	drmIoctlSetMaster        = drmIO(0x1e)
	drmIoctlDropMaster       = drmIO(0x1f)
	drmIoctlModeGetResources = drmIOWR(0xA0, unsafe.Sizeof(drmModeCardRes{}))
	drmIoctlModeSetCrtc      = drmIOWR(0xA2, unsafe.Sizeof(drmModeCrtc{}))
	drmIoctlModeGetEncoder   = drmIOWR(0xA6, unsafe.Sizeof(drmModeGetEncoder{}))
	drmIoctlModeGetConnector = drmIOWR(0xA7, unsafe.Sizeof(drmModeGetConnector{}))
	drmIoctlModeAddFB        = drmIOWR(0xAE, unsafe.Sizeof(drmModeFBCmd{}))
	drmIoctlModePageFlip     = drmIOWR(0xB0, unsafe.Sizeof(drmModeCrtcPageFlip{}))
	drmIoctlModeCreateDumb   = drmIOWR(0xB2, unsafe.Sizeof(drmModeCreateDumb{}))
	drmIoctlModeMapDumb      = drmIOWR(0xB3, unsafe.Sizeof(drmModeMapDumb{}))
	drmIoctlModeDestroyDumb  = drmIOWR(0xB4, unsafe.Sizeof(drmModeDestroyDumb{}))
)

// DRM connector "connection" status values (drm_mode_get_connector.connection).
const (
	drmModeConnected    = 1
	drmModeDisconnected = 2
)

// Page-flip request/event flags and types.
const (
	drmModePageFlipEvent = 0x01
	drmEventFlipComplete = 0x02
)

const drmDisplayModeLen = 32

// drmModeCardRes mirrors struct drm_mode_card_res.
type drmModeCardRes struct {
	FbIDPtr         uint64
	CrtcIDPtr       uint64
	ConnectorIDPtr  uint64
	EncoderIDPtr    uint64
	CountFBs        uint32
	CountCrtcs      uint32
	CountConnectors uint32
	CountEncoders   uint32
	MinWidth        uint32
	MaxWidth        uint32
	MinHeight       uint32
	MaxHeight       uint32
}

// drmModeGetEncoder mirrors struct drm_mode_get_encoder.
type drmModeGetEncoder struct {
	EncoderID      uint32
	EncoderType    uint32
	CrtcID         uint32
	PossibleCrtcs  uint32
	PossibleClones uint32
}

// drmModeModeInfo mirrors struct drm_mode_modeinfo.
type drmModeModeInfo struct {
	Clock uint32

	Hdisplay   uint16
	HsyncStart uint16
	HsyncEnd   uint16
	Htotal     uint16
	Hskew      uint16

	Vdisplay   uint16
	VsyncStart uint16
	VsyncEnd   uint16
	Vtotal     uint16
	Vscan      uint16

	Vrefresh uint32

	Flags uint32
	Type  uint32
	Name  [drmDisplayModeLen]byte
}

// drmModeGetConnector mirrors struct drm_mode_get_connector.
type drmModeGetConnector struct {
	EncodersPtr   uint64
	ModesPtr      uint64
	PropsPtr      uint64
	PropValuesPtr uint64

	CountModes    uint32
	CountProps    uint32
	CountEncoders uint32

	EncoderID       uint32
	ConnectorID     uint32
	ConnectorType   uint32
	ConnectorTypeID uint32

	Connection uint32
	MMWidth    uint32
	MMHeight   uint32
	Subpixel   uint32

	Pad uint32
}

// drmModeCreateDumb mirrors struct drm_mode_create_dumb.
type drmModeCreateDumb struct {
	Height uint32
	Width  uint32
	Bpp    uint32
	Flags  uint32

	Handle uint32
	Pitch  uint32
	Size   uint64
}

// drmModeMapDumb mirrors struct drm_mode_map_dumb.
type drmModeMapDumb struct {
	Handle uint32
	Pad    uint32
	Offset uint64
}

// drmModeDestroyDumb mirrors struct drm_mode_destroy_dumb.
type drmModeDestroyDumb struct {
	Handle uint32
}

// drmModeFBCmd mirrors struct drm_mode_fb_cmd.
type drmModeFBCmd struct {
	FbID   uint32
	Width  uint32
	Height uint32
	Pitch  uint32
	Bpp    uint32
	Depth  uint32
	Handle uint32
}

// drmModeCrtc mirrors struct drm_mode_crtc.
type drmModeCrtc struct {
	SetConnectorsPtr uint64
	CountConnectors  uint32

	CrtcID uint32
	FbID   uint32

	X uint32
	Y uint32

	GammaSize uint32
	ModeValid uint32
	Mode      drmModeModeInfo
}

// drmModeCrtcPageFlip mirrors struct drm_mode_crtc_page_flip.
type drmModeCrtcPageFlip struct {
	CrtcID   uint32
	FbID     uint32
	Flags    uint32
	Reserved uint32
	UserData uint64
}

// drmEvent mirrors struct drm_event, the common header read back off the
// DRM fd after a page-flip completes.
type drmEvent struct {
	Type   uint32
	Length uint32
}

// drmEventVblank mirrors struct drm_event_vblank, which follows a drmEvent
// of Type == drmEventFlipComplete.
type drmEventVblank struct {
	Base     drmEvent
	UserData uint64
	TVSec    uint32
	TVUsec   uint32
	Sequence uint32
	CrtcID   uint32
}

// pickPreferredMode returns the connector's preferred mode. The kernel
// lists a connector's modes with its preferred mode first — confirmed
// against the deployed board's `modetest -c` output during the Phase 1
// spike — so this is a plain index-0 selection rather than a search for
// the DRM_MODE_TYPE_PREFERRED flag.
func pickPreferredMode(modes []drmModeModeInfo) drmModeModeInfo {
	return modes[0]
}

// rgbaToXRGB8888 writes src's pixels into dst in the DRM_FORMAT_XRGB8888
// byte layout — little-endian, so B,G,R,X per pixel in memory — honoring
// dst's row pitch, which may exceed width*4 due to hardware buffer
// alignment. src is assumed fully opaque (true for every frame
// compositeLetterboxed produces), so premultiplied alpha is a no-op here.
func rgbaToXRGB8888(dst []byte, pitch int, src *image.RGBA) {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	for y := 0; y < h; y++ {
		srcOff := src.PixOffset(bounds.Min.X, bounds.Min.Y+y)
		dstOff := y * pitch
		for x := 0; x < w; x++ {
			r := src.Pix[srcOff+0]
			g := src.Pix[srcOff+1]
			b := src.Pix[srcOff+2]
			dst[dstOff+0] = b
			dst[dstOff+1] = g
			dst[dstOff+2] = r
			dst[dstOff+3] = 0
			srcOff += 4
			dstOff += 4
		}
	}
}
