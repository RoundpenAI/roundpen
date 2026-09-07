package rfbtest

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
)

func TestServeHandshake(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	errCh := make(chan error, 1)
	go func() { errCh <- Serve(a) }()

	ver := make([]byte, 12)
	if _, err := io.ReadFull(b, ver); err != nil {
		t.Fatal(err)
	}
	if string(ver) != "RFB 003.008\n" {
		t.Fatalf("version %q", ver)
	}
	if _, err := b.Write([]byte("RFB 003.008\n")); err != nil {
		t.Fatal(err)
	}
	sec := make([]byte, 2)
	if _, err := io.ReadFull(b, sec); err != nil {
		t.Fatal(err)
	}
	if sec[0] != 1 || sec[1] != 1 {
		t.Fatalf("security %v", sec)
	}
	if _, err := b.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	res := make([]byte, 4)
	if _, err := io.ReadFull(b, res); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	init := make([]byte, 24)
	if _, err := io.ReadFull(b, init); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(init[0:]) != Width || binary.BigEndian.Uint16(init[2:]) != Height {
		t.Fatalf("size %v", init[:4])
	}
	nlen := binary.BigEndian.Uint32(init[20:])
	name := make([]byte, nlen)
	if _, err := io.ReadFull(b, name); err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
	<-errCh
}
