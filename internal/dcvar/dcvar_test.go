package dcvar

import "testing"

func TestParseGPUKatanaDumps(t *testing.T) {
	tests := []struct {
		name               string
		value              byte
		applied, requested byte
	}{
		{"discrete", 0x35, CodeDiscrete, CodeDiscrete},
		{"hybrid", 0x30, CodeHybrid, CodeHybrid},
		{"integrated", 0x3a, CodeIntegrated, CodeIntegrated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte{0x01, 0x01, 0x00, 0x04, 0x00, tt.value, 0x00}
			gpu, err := ParseGPU(data)
			if err != nil {
				t.Fatalf("ParseGPU: %v", err)
			}
			if !gpu.ThreeMode() || !gpu.IntegratedSupported() || !gpu.DiscreteSupported() {
				t.Fatalf("flags of 0x%02x: three-mode=%v integrated=%v discrete=%v",
					tt.value, gpu.ThreeMode(), gpu.IntegratedSupported(), gpu.DiscreteSupported())
			}
			if gpu.Applied() != tt.applied || gpu.Requested() != tt.requested {
				t.Fatalf("0x%02x: applied=%d requested=%d, want %d %d",
					tt.value, gpu.Applied(), gpu.Requested(), tt.applied, tt.requested)
			}
		})
	}
}

func TestWithRequestKeepsOtherBits(t *testing.T) {
	gpu := GPU(0x35)
	for code, want := range map[byte]GPU{CodeHybrid: 0x34, CodeDiscrete: 0x35, CodeIntegrated: 0x36} {
		if got := gpu.WithRequest(code); got != want {
			t.Fatalf("WithRequest(%d) = 0x%02x, want 0x%02x", code, byte(got), byte(want))
		}
	}
}

func TestSupportFlags(t *testing.T) {
	if GPU(0x15).IntegratedSupported() {
		t.Fatalf("0x15 has no integrated flag")
	}
	if GPU(0x55).DiscreteSupported() {
		t.Fatalf("0x55 sets the no-discrete flag")
	}
	if GPU(0x00).ThreeMode() {
		t.Fatalf("0x00 has no three-mode flag")
	}
}

func TestParseGPUTooShort(t *testing.T) {
	if _, err := ParseGPU([]byte{0x01, 0x01, 0x00, 0x00}); err == nil {
		t.Fatalf("expected error on data without the GPU byte")
	}
}
