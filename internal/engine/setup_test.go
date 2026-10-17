package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestEmbeddedFileCompareStreamsAcrossChunks(t *testing.T) {
	data := bytes.Repeat([]byte("winws-"), setupChunkSize/3)
	source := fstest.MapFS{"resources/winws.exe": {Data: data}}
	dest := filepath.Join(t.TempDir(), "winws.exe")
	buffers := make([]byte, 2*setupChunkSize)

	if sameEmbeddedFile(source, "resources/winws.exe", dest, int64(len(data)), buffers) {
		t.Fatal("a missing file compared equal")
	}
	if err := writeEmbeddedFile(source, "resources/winws.exe", dest); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !sameEmbeddedFile(source, "resources/winws.exe", dest, int64(len(data)), buffers) {
		t.Fatal("an identical file compared different")
	}

	changed := append([]byte(nil), data...)
	changed[len(changed)-setupChunkSize/2] ^= 0xff
	if err := os.WriteFile(dest, changed, 0644); err != nil {
		t.Fatal(err)
	}
	if sameEmbeddedFile(source, "resources/winws.exe", dest, int64(len(data)), buffers) {
		t.Fatal("a one-byte difference in a later chunk was missed")
	}

	if err := os.WriteFile(dest, data[:len(data)-1], 0644); err != nil {
		t.Fatal(err)
	}
	if sameEmbeddedFile(source, "resources/winws.exe", dest, int64(len(data)), buffers) {
		t.Fatal("a truncated file compared equal")
	}

	if err := writeEmbeddedFile(source, "resources/winws.exe", dest); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	written, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(written, data) {
		t.Fatal("rewriting over a longer stale file left different content")
	}
}

func TestSetupCopiesOnlyTheNativeBinarySet(t *testing.T) {
	useTempTargetDir(t)
	other := archX86
	if nativeArch == archX86 {
		other = archX64
	}
	source := fstest.MapFS{
		"resources/" + nativeArch + "/winws.exe":          {Data: []byte("native")},
		"resources/" + nativeArch + "/" + driverFile():    {Data: []byte("driver")},
		"resources/" + other + "/" + driverFileFor(other): {Data: []byte("other")},
	}
	if err := Setup(source); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(getTargetDir(), "winws.exe")); err != nil || string(data) != "native" {
		t.Fatalf("winws.exe = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(getTargetDir(), driverFileFor(other))); err == nil {
		t.Fatal("the driver for the other architecture must not be installed")
	}
	if !GetBootInfo().DriverReady {
		t.Fatal("the native driver must be reported as ready")
	}
}
