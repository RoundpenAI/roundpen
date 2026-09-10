// Package rfbtest serves a minimal RFB 3.8 desktop over a raw connection
// (Unix socket or TCP). Used so noVNC can complete a real handshake without QEMU.
package rfbtest

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
)

const (
	Width  = 64
	Height = 64
	// Pixel is 32-bit little-endian RGB (red shift 16): distinctive for canvas probes.
	Red   = 0xCC
	Green = 0x22
	Blue  = 0x44
)

// ListenUnix serves RFB on a Unix socket, replacing any existing file.
func ListenUnix(path string) (net.Listener, error) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	go accept(ln)
	return ln, nil
}

func accept(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			_ = Serve(c)
		}()
	}
}

// Serve runs one RFB 3.8 session with Security None and a solid framebuffer.
func Serve(c net.Conn) error {
	defer c.Close()
	if _, err := c.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	ver := make([]byte, 12)
	if _, err := io.ReadFull(c, ver); err != nil {
		return fmt.Errorf("version: %w", err)
	}
	if _, err := c.Write([]byte{1, 1}); err != nil { // 1 type, None
		return err
	}
	sec := make([]byte, 1)
	if _, err := io.ReadFull(c, sec); err != nil {
		return fmt.Errorf("security: %w", err)
	}
	if _, err := c.Write([]byte{0, 0, 0, 0}); err != nil {
		return err
	}
	clientInit := make([]byte, 1)
	if _, err := io.ReadFull(c, clientInit); err != nil {
		return fmt.Errorf("clientInit: %w", err)
	}
	if err := writeServerInit(c); err != nil {
		return err
	}
	return readLoop(c)
}

func writeServerInit(w io.Writer) error {
	name := []byte("roundpen-uismoke")
	buf := make([]byte, 24+4+len(name))
	binary.BigEndian.PutUint16(buf[0:], Width)
	binary.BigEndian.PutUint16(buf[2:], Height)
	buf[4] = 32 // bits-per-pixel
	buf[5] = 24 // depth
	buf[6] = 0  // little endian
	buf[7] = 1  // true color
	binary.BigEndian.PutUint16(buf[8:], 255)
	binary.BigEndian.PutUint16(buf[10:], 255)
	binary.BigEndian.PutUint16(buf[12:], 255)
	buf[14] = 16 // red-shift
	buf[15] = 8
	buf[16] = 0
	binary.BigEndian.PutUint32(buf[20:], uint32(len(name)))
	copy(buf[24:], name)
	_, err := w.Write(buf)
	return err
}

func readLoop(c net.Conn) error {
	var mu sync.Mutex
	for {
		hdr := make([]byte, 1)
		if _, err := io.ReadFull(c, hdr); err != nil {
			return err
		}
		switch hdr[0] {
		case 0: // SetPixelFormat
			if _, err := io.CopyN(io.Discard, c, 19); err != nil {
				return err
			}
		case 2: // SetEncodings
			pad := make([]byte, 3)
			if _, err := io.ReadFull(c, pad); err != nil {
				return err
			}
			n := binary.BigEndian.Uint16(pad[1:])
			if _, err := io.CopyN(io.Discard, c, int64(n)*4); err != nil {
				return err
			}
		case 3: // FramebufferUpdateRequest
			rest := make([]byte, 9)
			if _, err := io.ReadFull(c, rest); err != nil {
				return err
			}
			mu.Lock()
			err := writeFramebuffer(c)
			mu.Unlock()
			if err != nil {
				return err
			}
		case 4: // KeyEvent
			if _, err := io.CopyN(io.Discard, c, 7); err != nil {
				return err
			}
		case 5: // PointerEvent
			if _, err := io.CopyN(io.Discard, c, 5); err != nil {
				return err
			}
		case 6: // ClientCutText
			pad := make([]byte, 7)
			if _, err := io.ReadFull(c, pad); err != nil {
				return err
			}
			n := binary.BigEndian.Uint32(pad[3:])
			if _, err := io.CopyN(io.Discard, c, int64(n)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("rfb: unknown client message %d", hdr[0])
		}
	}
}

func writeFramebuffer(w io.Writer) error {
	// FramebufferUpdate: 1 rect, raw encoding, solid color.
	hdr := make([]byte, 4+12)
	hdr[0] = 0
	binary.BigEndian.PutUint16(hdr[2:], 1)
	binary.BigEndian.PutUint16(hdr[8:], Width)
	binary.BigEndian.PutUint16(hdr[10:], Height)
	// encoding raw = 0
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	// noVNC typically SetPixelFormat to 32-bit RGB byte order after ServerInit.
	pix := []byte{Red, Green, Blue, 0}
	row := make([]byte, Width*4)
	for i := 0; i < Width; i++ {
		copy(row[i*4:], pix)
	}
	for y := 0; y < Height; y++ {
		if _, err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}
