package docker

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"testing"
	"time"
)

type tarEntry struct {
	header tar.Header
	body   string
}

func makeTar(t *testing.T, entries ...tarEntry) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		h := e.header
		h.Size = int64(len(e.body))
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

// Mirrors the entry shape the Docker daemon returns for /workspace
// (names rebased to the listed directory's basename, root entry included).
func TestDirEntriesFromTarRootListing(t *testing.T) {
	mod := time.Date(2026, 9, 12, 10, 59, 0, 0, time.UTC)
	r := makeTar(t,
		tarEntry{header: tar.Header{Name: "workspace/", Typeflag: tar.TypeDir, Mode: 0o755}},
		tarEntry{header: tar.Header{Name: "workspace/.roundpen/", Typeflag: tar.TypeDir, Mode: 0o755}},
		tarEntry{header: tar.Header{Name: "workspace/.roundpen/env", Typeflag: tar.TypeReg, Mode: 0o664}, body: "x"},
		tarEntry{header: tar.Header{Name: "workspace/note.txt", Typeflag: tar.TypeReg, Mode: 0o644, ModTime: mod}, body: "hello"},
		tarEntry{header: tar.Header{Name: "workspace/sub/", Typeflag: tar.TypeDir, Mode: 0o755}},
		tarEntry{header: tar.Header{Name: "workspace/sub/inner.txt", Typeflag: tar.TypeReg, Mode: 0o644}, body: "y"},
		tarEntry{header: tar.Header{Name: "workspace/link", Typeflag: tar.TypeSymlink, Linkname: "note.txt", Mode: 0o777}},
	)
	entries, err := dirEntriesFromTar("workspace", r)
	if err != nil {
		t.Fatalf("dirEntriesFromTar: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("want 4 entries, got %d: %+v", len(entries), entries)
	}
	// Sorted by name; nested children are excluded.
	want := []struct {
		name  string
		isDir bool
		size  int64
	}{
		{".roundpen", true, 0},
		{"link", false, 0},
		{"note.txt", false, 5},
		{"sub", true, 0},
	}
	for i, w := range want {
		got := entries[i]
		if got.Name != w.name || got.IsDir != w.isDir || got.Size != w.size {
			t.Errorf("entry %d = %+v, want name=%q isDir=%v size=%d", i, got, w.name, w.isDir, w.size)
		}
	}
	if !entries[2].ModTime.Equal(mod) {
		t.Errorf("modtime = %v, want %v", entries[2].ModTime, mod)
	}
}

func TestDirEntriesFromTarSubdirListing(t *testing.T) {
	r := makeTar(t,
		tarEntry{header: tar.Header{Name: ".roundpen/", Typeflag: tar.TypeDir, Mode: 0o755}},
		tarEntry{header: tar.Header{Name: ".roundpen/env", Typeflag: tar.TypeReg, Mode: 0o664}, body: "x"},
	)
	entries, err := dirEntriesFromTar(".roundpen", r)
	if err != nil {
		t.Fatalf("dirEntriesFromTar: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "env" || entries[0].IsDir {
		t.Fatalf("got %+v, want single file env", entries)
	}
}

func TestDirEntriesFromTarEmptyDir(t *testing.T) {
	r := makeTar(t, tarEntry{header: tar.Header{Name: "workspace/", Typeflag: tar.TypeDir, Mode: 0o755}})
	entries, err := dirEntriesFromTar("workspace", r)
	if err != nil {
		t.Fatalf("dirEntriesFromTar: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 entries, got %+v", entries)
	}
}

func TestGuestPathErrMapsDaemonNotFound(t *testing.T) {
	daemonErr := errors.New("Error response from daemon: Could not find the file /workspace/nope in container roundpen-admin")
	err := guestPathErr("/workspace/nope", daemonErr)
	if !os.IsNotExist(err) {
		t.Fatalf("want not-exist error, got %v", err)
	}
	other := errors.New("Error response from daemon: something else")
	if os.IsNotExist(guestPathErr("/workspace/x", other)) {
		t.Fatal("generic error must not map to not-exist")
	}
}
