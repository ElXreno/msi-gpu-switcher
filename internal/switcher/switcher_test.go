package switcher

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"msi-gpu-switcher/internal/dcvar"
	"msi-gpu-switcher/internal/efivar"
)

var (
	katanaDiscrete = []byte{
		0x07, 0x00, 0x00, 0x00,
		0x01, 0x01, 0x00, 0x04, 0x00, 0x35, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	titanDiscrete = []byte{
		0x07, 0x00, 0x00, 0x00,
		0x01, 0x01, 0x00, 0x04, 0x00, 0x35, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	alphaShort = []byte{0x07, 0x00, 0x00, 0x00, 0x01, 0x01, 0x00, 0x00}
)

const ecSwitchDiscrete = 0x04

type fakeEC struct {
	regs     [256]byte
	missing  bool
	writes   []byte
	firmware func(*fakeEC)
}

func (f *fakeEC) Available() bool {
	return !f.missing
}

func (f *fakeEC) Get(offset byte) (byte, error) {
	return f.regs[offset], nil
}

func (f *fakeEC) Set(offset, value byte) error {
	f.regs[offset] = value
	f.writes = append(f.writes, offset)
	if offset == ecSwitch && value&ecSwitchRequest != 0 && f.firmware != nil {
		f.firmware(f)
	}
	return nil
}

func sgSmm(t *testing.T, v efivar.Var, stored *byte) func(*fakeEC) {
	return func(f *fakeEC) {
		_, data, err := v.Read()
		if err != nil {
			t.Fatalf("firmware read: %v", err)
		}
		request := data[dcvar.GPUByte] & 0x03
		if request > dcvar.CodeIntegrated {
			f.regs[ecSwitch] &^= ecSwitchAck
			return
		}
		if request != *stored {
			*stored = request
			if request == dcvar.CodeDiscrete {
				f.regs[ecSwitch] |= ecSwitchDiscrete
			} else {
				f.regs[ecSwitch] &^= ecSwitchDiscrete
			}
		}
		f.regs[ecSwitch] |= ecSwitchAck
	}
}

func writeVar(t *testing.T, raw []byte) efivar.Var {
	t.Helper()
	path := filepath.Join(t.TempDir(), dcvar.Name+"-"+dcvar.GUID)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write test var: %v", err)
	}
	return efivar.Var{Path: path}
}

func readRaw(t *testing.T, v efivar.Var) []byte {
	t.Helper()
	raw, err := os.ReadFile(v.Path)
	if err != nil {
		t.Fatalf("read test var: %v", err)
	}
	return raw
}

func testEnv(v efivar.Var, c *fakeEC) *Env {
	return &Env{EC: c, Var: v, AckTimeout: 20 * time.Millisecond, PollInterval: time.Millisecond}
}

func withGPUByte(raw []byte, value byte) []byte {
	out := slices.Clone(raw)
	out[4+dcvar.GPUByte] = value
	return out
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name    string
		raw     []byte
		backend string
		modes   []Mode
	}{
		{"titan", titanDiscrete, "three-mode", []Mode{Hybrid, Discrete, Integrated}},
		{"katana", katanaDiscrete, "three-mode", []Mode{Hybrid, Discrete, Integrated}},
		{"no integrated flag", withGPUByte(katanaDiscrete, 0x15), "three-mode", []Mode{Hybrid, Discrete}},
		{"no three-mode flag", withGPUByte(katanaDiscrete, 0x00), "legacy", []Mode{Hybrid, Discrete}},
		{"short var", alphaShort, "legacy", []Mode{Hybrid, Discrete}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Detect(testEnv(writeVar(t, tt.raw), &fakeEC{}))
			if b == nil || b.Name() != tt.backend {
				t.Fatalf("Detect = %v, want %s", b, tt.backend)
			}
			if !slices.Equal(b.Modes(), tt.modes) {
				t.Fatalf("Modes = %v, want %v", b.Modes(), tt.modes)
			}
		})
	}
}

func TestDetectWithoutVar(t *testing.T) {
	env := testEnv(efivar.Var{Path: filepath.Join(t.TempDir(), "missing")}, &fakeEC{})
	if b := Detect(env); b == nil || b.Name() != "legacy" {
		t.Fatalf("Detect = %v, want legacy", b)
	}
}

func TestThreeModeRequests(t *testing.T) {
	tests := []struct {
		name     string
		from     byte
		stored   byte
		mode     Mode
		gpuByte  byte
		want     byte
		discrete bool
	}{
		{"discrete to hybrid", 0x35, dcvar.CodeDiscrete, Hybrid, 0x34, dcvar.CodeHybrid, false},
		{"hybrid to discrete", 0x30, dcvar.CodeHybrid, Discrete, 0x31, dcvar.CodeDiscrete, true},
		{"hybrid to integrated", 0x30, dcvar.CodeHybrid, Integrated, 0x32, dcvar.CodeIntegrated, false},
		{"pending hybrid back to discrete", 0x34, dcvar.CodeHybrid, Discrete, 0x35, dcvar.CodeDiscrete, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := withGPUByte(katanaDiscrete, tt.from)
			v := writeVar(t, raw)
			stored := tt.stored
			c := &fakeEC{}
			c.firmware = sgSmm(t, v, &stored)

			b := Detect(testEnv(v, c))
			if err := b.Switch(tt.mode); err != nil {
				t.Fatalf("Switch(%s): %v", tt.mode, err)
			}

			if want := withGPUByte(raw, tt.gpuByte); !bytes.Equal(readRaw(t, v), want) {
				t.Fatalf("var = % x, want % x", readRaw(t, v), want)
			}
			if !slices.Equal(c.writes, []byte{ecSwitch}) {
				t.Fatalf("EC writes = % x, want only 0xd1", c.writes)
			}
			if c.regs[ecSwitch]&ecSwitchAck == 0 {
				t.Fatalf("EC 0xd1 = 0x%02x, firmware ack missing", c.regs[ecSwitch])
			}
			if stored != tt.want {
				t.Fatalf("firmware stored %d, want %d", stored, tt.want)
			}
			if got := c.regs[ecSwitch]&ecSwitchDiscrete != 0; got != tt.discrete {
				t.Fatalf("EC 0xd1 discrete bit = %v, want %v", got, tt.discrete)
			}
		})
	}
}

func TestThreeModeAlreadyActive(t *testing.T) {
	v := writeVar(t, titanDiscrete)
	c := &fakeEC{}
	b := Detect(testEnv(v, c))
	if err := b.Switch(Discrete); !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("Switch(discrete) on discrete = %v, want ErrAlreadyActive", err)
	}
	if !bytes.Equal(readRaw(t, v), titanDiscrete) || len(c.writes) != 0 {
		t.Fatalf("no-op switch touched state: var=% x ec writes=% x", readRaw(t, v), c.writes)
	}
}

func TestThreeModeStatus(t *testing.T) {
	v := writeVar(t, katanaDiscrete)
	stored := dcvar.CodeDiscrete
	c := &fakeEC{}
	c.firmware = sgSmm(t, v, &stored)
	b := Detect(testEnv(v, c))

	st := b.Status()
	if st.Current == nil || *st.Current != Discrete || st.Requested == nil || *st.Requested != Discrete {
		t.Fatalf("status before switch: current=%v requested=%v", st.Current, st.Requested)
	}
	if err := b.Switch(Hybrid); err != nil {
		t.Fatalf("Switch: %v", err)
	}
	st = b.Status()
	if *st.Current != Discrete || *st.Requested != Hybrid {
		t.Fatalf("status after switch: current=%s requested=%s", st.Current, st.Requested)
	}
}

func TestThreeModeRejectsUnofferedMode(t *testing.T) {
	raw := withGPUByte(katanaDiscrete, 0x15)
	v := writeVar(t, raw)
	c := &fakeEC{}
	b := Detect(testEnv(v, c))
	if err := b.Switch(Integrated); err == nil {
		t.Fatalf("expected error for integrated mode without the firmware flag")
	}
	if !bytes.Equal(readRaw(t, v), raw) || len(c.writes) != 0 {
		t.Fatalf("rejected switch touched state: var=% x ec writes=% x", readRaw(t, v), c.writes)
	}
}

func TestThreeModeWithoutAck(t *testing.T) {
	v := writeVar(t, katanaDiscrete)
	b := Detect(testEnv(v, &fakeEC{}))
	err := b.Switch(Hybrid)
	if err == nil || !strings.Contains(err.Error(), "acknowledge") {
		t.Fatalf("Switch without firmware ack = %v, want acknowledge error", err)
	}
}

func TestThreeModeNeedsEC(t *testing.T) {
	v := writeVar(t, katanaDiscrete)
	b := Detect(testEnv(v, &fakeEC{missing: true}))
	if err := b.Switch(Hybrid); err == nil {
		t.Fatalf("expected error without EC")
	}
	if !bytes.Equal(readRaw(t, v), katanaDiscrete) {
		t.Fatalf("var changed without EC: % x", readRaw(t, v))
	}
}

func TestLegacySwitch(t *testing.T) {
	v := writeVar(t, alphaShort)
	c := &fakeEC{}
	c.regs[legacyECMux] = 0x5b
	b := Detect(testEnv(v, c))

	if err := b.Switch(Hybrid); err != nil {
		t.Fatalf("Switch(hybrid): %v", err)
	}
	if want := []byte{0x07, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}; !bytes.Equal(readRaw(t, v), want) {
		t.Fatalf("var = % x, want % x", readRaw(t, v), want)
	}
	if c.regs[ecSwitch]&(ecSwitchRequest|ecSwitchAck) != ecSwitchRequest {
		t.Fatalf("EC 0xd1 = 0x%02x, want request bit only", c.regs[ecSwitch])
	}
	if c.regs[legacyECMux] != 0x1b {
		t.Fatalf("EC 0x2e = 0x%02x, want 0x1b", c.regs[legacyECMux])
	}
	if err := b.Switch(Integrated); err == nil {
		t.Fatalf("expected error for integrated mode on the legacy switch")
	}
}
