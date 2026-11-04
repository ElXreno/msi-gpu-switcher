package switcher

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rs/zerolog/log"

	"msi-gpu-switcher/internal/dcvar"
	"msi-gpu-switcher/internal/ec"
	"msi-gpu-switcher/internal/efivar"
	"msi-gpu-switcher/internal/sysinfo"
)

type Mode int

const (
	Hybrid Mode = iota
	Discrete
	Integrated
)

func (m Mode) String() string {
	switch m {
	case Hybrid:
		return "hybrid"
	case Discrete:
		return "discrete"
	case Integrated:
		return "integrated"
	}
	return fmt.Sprintf("Mode(%d)", int(m))
}

func (m Mode) Label() string {
	switch m {
	case Hybrid:
		return "hybrid (iGPU drives the display, dGPU on demand)"
	case Discrete:
		return "discrete (dGPU only)"
	case Integrated:
		return "integrated (iGPU only, dGPU off)"
	}
	return m.String()
}

var ErrAlreadyActive = errors.New("mode is already active")

type Backend interface {
	Name() string
	Modes() []Mode
	Status() Status
	Switch(Mode) error
}

type Status struct {
	Current   *Mode
	Requested *Mode
	Details   []Detail
}

type Detail struct {
	Name, Value string
}

type Env struct {
	EC           ec.Controller
	Var          efivar.Var
	Machine      sysinfo.Machine
	AckTimeout   time.Duration
	PollInterval time.Duration
}

func SystemEnv() *Env {
	return &Env{
		EC:           ec.Debugfs{Path: ec.DebugfsPath},
		Var:          efivar.Var{Path: dcvar.DefaultPath},
		Machine:      sysinfo.DetectMachine(),
		AckTimeout:   5 * time.Second,
		PollInterval: 100 * time.Millisecond,
	}
}

type probe func(*Env) (Backend, bool)

var probes = []probe{
	probeThreeMode,
	probeLegacy,
}

func Detect(env *Env) Backend {
	for _, p := range probes {
		if b, ok := p(env); ok {
			log.Debug().Msgf("backend: %s", b.Name())
			return b
		}
	}
	return nil
}

func Supports(b Backend, m Mode) bool {
	return slices.Contains(b.Modes(), m)
}

const (
	ecSwitch        = 0xd1
	ecSwitchRequest = 0x01
	ecSwitchAck     = 0x02
)

func requestECSwitch(c ec.Controller) error {
	value, err := c.Get(ecSwitch)
	if err != nil {
		return err
	}
	log.Debug().Msgf("ec switch before: 0x%02x", value)
	value = value&^(ecSwitchRequest|ecSwitchAck) | ecSwitchRequest
	log.Debug().Msgf("ec switch after: 0x%02x", value)
	return c.Set(ecSwitch, value)
}

func ecByteDetail(c ec.Controller, offset byte) string {
	if !c.Available() {
		return "not available (ec_sys/debugfs)"
	}
	value, err := c.Get(offset)
	if err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("0x%02x", value)
}
