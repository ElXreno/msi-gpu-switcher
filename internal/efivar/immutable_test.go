package efivar

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWriteImmutable(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the immutable flag needs CAP_LINUX_IMMUTABLE")
	}
	path := filepath.Join(t.TempDir(), "MsiDCVarData-immutable")
	if err := os.WriteFile(path, []byte{0x07, 0, 0, 0, 0x01, 0x35}, 0o644); err != nil {
		t.Fatalf("write test var: %v", err)
	}
	setFlagForTest(t, path, true)
	t.Cleanup(func() { setFlagForTest(t, path, false) })

	if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		_ = f.Close()
		t.Fatalf("immutable file opened for writing, the test setup is broken")
	}

	if err := (Var{Path: path}).Write(0x07, []byte{0x01, 0x34}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if want := []byte{0x07, 0, 0, 0, 0x01, 0x34}; !bytes.Equal(raw, want) {
		t.Fatalf("raw = % x, want % x", raw, want)
	}
	if !flagForTest(t, path) {
		t.Fatalf("immutable flag was not restored")
	}
}

func flagForTest(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	flags, err := unix.IoctlGetUint32(int(f.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil {
		t.Fatalf("FS_IOC_GETFLAGS: %v", err)
	}
	return flags&fsImmutableFl != 0
}

func setFlagForTest(t *testing.T, path string, on bool) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	flags, err := unix.IoctlGetUint32(int(f.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil {
		t.Fatalf("FS_IOC_GETFLAGS: %v", err)
	}
	if on {
		flags |= fsImmutableFl
	} else {
		flags &^= fsImmutableFl
	}
	if err := unix.IoctlSetPointerInt(int(f.Fd()), unix.FS_IOC_SETFLAGS, int(flags)); err != nil {
		t.Fatalf("FS_IOC_SETFLAGS: %v", err)
	}
}
