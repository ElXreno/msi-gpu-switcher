package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestSetUefiGpuModeUpdatesByte(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MsiDCVarData-test")
	originalPath := uefiVarPath
	uefiVarPath = path
	t.Cleanup(func() { uefiVarPath = originalPath })

	attrs := uint32(0x07)
	data := []byte{0x01, 0x01, 0x00, 0x00}
	payload := make([]byte, uefiDataBase+len(data))
	binary.LittleEndian.PutUint32(payload[:uefiDataBase], attrs)
	copy(payload[uefiDataBase:], data)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write test var: %v", err)
	}

	if err := setUefiGpuMode(false); err != nil {
		t.Fatalf("setUefiGpuMode: %v", err)
	}

	_, updated, err := readUefiVar()
	if err != nil {
		t.Fatalf("readUefiVar: %v", err)
	}
	if len(updated) <= uefiModeByte {
		t.Fatalf("updated data too small: %d", len(updated))
	}
	if got := updated[uefiModeByte]; got != 0 {
		t.Fatalf("expected mode byte 0, got 0x%02x", got)
	}
}

func TestReadUefiVarTooSmall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MsiDCVarData-small")
	originalPath := uefiVarPath
	uefiVarPath = path
	t.Cleanup(func() { uefiVarPath = originalPath })

	if err := os.WriteFile(path, []byte{0x01, 0x02, 0x03}, 0o644); err != nil {
		t.Fatalf("write test var: %v", err)
	}

	if _, _, err := readUefiVar(); err == nil {
		t.Fatalf("expected error on short uefi var")
	}
}

func TestWriteUefiVarPreservesAttrsAndData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MsiDCVarData-write")
	originalPath := uefiVarPath
	uefiVarPath = path
	t.Cleanup(func() { uefiVarPath = originalPath })

	attrs := uint32(0x07)
	data := []byte{0x01, 0x02, 0x03, 0x04}
	if err := writeUefiVar(attrs, data); err != nil {
		t.Fatalf("writeUefiVar: %v", err)
	}

	readAttrs, readData, err := readUefiVar()
	if err != nil {
		t.Fatalf("readUefiVar: %v", err)
	}
	if readAttrs != attrs {
		t.Fatalf("attrs mismatch: got 0x%08x want 0x%08x", readAttrs, attrs)
	}
	if len(readData) != len(data) {
		t.Fatalf("data length mismatch: got %d want %d", len(readData), len(data))
	}
	for i := range data {
		if readData[i] != data[i] {
			t.Fatalf("data mismatch at %d: got 0x%02x want 0x%02x", i, readData[i], data[i])
		}
	}
}

func TestReadUefiGpuMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MsiDCVarData-mode")
	originalPath := uefiVarPath
	uefiVarPath = path
	t.Cleanup(func() { uefiVarPath = originalPath })

	attrs := uint32(0x07)
	data := []byte{0x00, 0x01, 0x00}
	payload := make([]byte, uefiDataBase+len(data))
	binary.LittleEndian.PutUint32(payload[:uefiDataBase], attrs)
	copy(payload[uefiDataBase:], data)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write test var: %v", err)
	}

	mode, err := readUefiGpuMode()
	if err != nil {
		t.Fatalf("readUefiGpuMode: %v", err)
	}
	if !mode {
		t.Fatalf("expected discrete mode true")
	}
}
