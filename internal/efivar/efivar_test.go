package efivar

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTooSmall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MsiDCVarData-small")
	if err := os.WriteFile(path, []byte{0x01, 0x02, 0x03}, 0o644); err != nil {
		t.Fatalf("write test var: %v", err)
	}
	if _, _, err := (Var{Path: path}).Read(); err == nil {
		t.Fatalf("expected error on short uefi var")
	}
}

func TestWritePreservesAttrsAndData(t *testing.T) {
	v := Var{Path: filepath.Join(t.TempDir(), "MsiDCVarData-write")}
	attrs := uint32(0x07)
	data := []byte{0x01, 0x02, 0x03, 0x04}
	if err := v.Write(attrs, data); err != nil {
		t.Fatalf("Write: %v", err)
	}

	gotAttrs, gotData, err := v.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if gotAttrs != attrs {
		t.Fatalf("attrs mismatch: got 0x%08x want 0x%08x", gotAttrs, attrs)
	}
	if !bytes.Equal(gotData, data) {
		t.Fatalf("data mismatch: got % x want % x", gotData, data)
	}
}

func TestRewriteExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MsiDCVarData-rewrite")
	if err := os.WriteFile(path, []byte{0x07, 0, 0, 0, 0x01, 0x01}, 0o644); err != nil {
		t.Fatalf("write test var: %v", err)
	}
	v := Var{Path: path}
	if err := v.Write(0x07, []byte{0x01, 0x00}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if want := []byte{0x07, 0, 0, 0, 0x01, 0x00}; !bytes.Equal(raw, want) {
		t.Fatalf("raw mismatch: got % x want % x", raw, want)
	}
}
