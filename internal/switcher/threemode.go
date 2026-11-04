package switcher

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rs/zerolog/log"

	"msi-gpu-switcher/internal/dcvar"
)

type threeMode struct {
	env *Env
	gpu dcvar.GPU
}

func probeThreeMode(env *Env) (Backend, bool) {
	if !env.Var.Exists() {
		return nil, false
	}
	_, data, err := env.Var.Read()
	if err != nil {
		log.Debug().Msgf("three-mode probe: %v", err)
		return nil, false
	}
	gpu, err := dcvar.ParseGPU(data)
	if err != nil || !gpu.ThreeMode() {
		return nil, false
	}
	return &threeMode{env: env, gpu: gpu}, true
}

func (b *threeMode) Name() string {
	return "three-mode"
}

func (b *threeMode) Modes() []Mode {
	return gpuModes(b.gpu)
}

func (b *threeMode) Status() Status {
	var s Status
	if m, ok := codeMode(b.gpu.Applied()); ok {
		s.Current = &m
	}
	if m, ok := codeMode(b.gpu.Requested()); ok {
		s.Requested = &m
	}
	s.Details = []Detail{
		{fmt.Sprintf("%s[%d]", dcvar.Name, dcvar.GPUByte), fmt.Sprintf("0x%02x", byte(b.gpu))},
		{"EC switch (0xD1)", ecByteDetail(b.env.EC, ecSwitch)},
	}
	return s
}

func (b *threeMode) Switch(m Mode) error {
	attrs, data, err := b.env.Var.Read()
	if err != nil {
		return err
	}
	gpu, err := dcvar.ParseGPU(data)
	if err != nil {
		return err
	}
	if !slices.Contains(gpuModes(gpu), m) {
		return fmt.Errorf("this firmware does not offer %s mode", m)
	}
	code := modeCode(m)
	if gpu.Applied() == code && gpu.Requested() == code {
		return ErrAlreadyActive
	}
	if !b.env.EC.Available() {
		return errors.New("EC is not available; load ec_sys with write_support=1 and mount debugfs")
	}

	updated := gpu.WithRequest(code)
	log.Debug().Msgf("%s[%d] before=0x%02x after=0x%02x", dcvar.Name, dcvar.GPUByte, byte(gpu), byte(updated))
	data[dcvar.GPUByte] = byte(updated)
	if err := b.env.Var.Write(attrs, data); err != nil {
		return err
	}
	_, check, err := b.env.Var.Read()
	if err != nil {
		return err
	}
	if got, err := dcvar.ParseGPU(check); err != nil || got != updated {
		return fmt.Errorf("firmware did not keep the request in %s", dcvar.Name)
	}
	b.gpu = updated

	if err := requestECSwitch(b.env.EC); err != nil {
		return fmt.Errorf("EC switch request failed: %w (is ec_sys write_support=1?)", err)
	}
	return b.waitAck()
}

func (b *threeMode) waitAck() error {
	deadline := time.Now().Add(b.env.AckTimeout)
	for {
		value, err := b.env.EC.Get(ecSwitch)
		if err != nil {
			return err
		}
		if value&ecSwitchAck != 0 {
			log.Debug().Msgf("firmware acknowledged, ec switch=0x%02x", value)
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("firmware did not acknowledge the switch (EC 0x%02x=0x%02x)", ecSwitch, value)
		}
		time.Sleep(b.env.PollInterval)
	}
}

func gpuModes(g dcvar.GPU) []Mode {
	modes := []Mode{Hybrid}
	if g.DiscreteSupported() {
		modes = append(modes, Discrete)
	}
	if g.IntegratedSupported() {
		modes = append(modes, Integrated)
	}
	return modes
}

func modeCode(m Mode) byte {
	switch m {
	case Discrete:
		return dcvar.CodeDiscrete
	case Integrated:
		return dcvar.CodeIntegrated
	}
	return dcvar.CodeHybrid
}

func codeMode(code byte) (Mode, bool) {
	switch code {
	case dcvar.CodeHybrid:
		return Hybrid, true
	case dcvar.CodeDiscrete:
		return Discrete, true
	case dcvar.CodeIntegrated:
		return Integrated, true
	}
	return 0, false
}
