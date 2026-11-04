package switcher

import (
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"

	"msi-gpu-switcher/internal/dcvar"
)

const (
	legacyECMux       = 0x2e
	legacyECMuxMask   = 0x40
	legacyVarModeByte = 1
)

type legacy struct {
	env *Env
}

func probeLegacy(env *Env) (Backend, bool) {
	return &legacy{env: env}, true
}

func (b *legacy) Name() string {
	return "legacy"
}

func (b *legacy) Modes() []Mode {
	return []Mode{Hybrid, Discrete}
}

func (b *legacy) Status() Status {
	var s Status
	mux := "not available (ec_sys/debugfs)"
	trigger := mux
	if b.env.EC.Available() {
		if value, err := b.env.EC.Get(legacyECMux); err != nil {
			mux = "error: " + err.Error()
		} else {
			m := Hybrid
			mux = "hybrid (PXCT=0)"
			if value&legacyECMuxMask != 0 {
				m = Discrete
				mux = "discrete (PXCT=1)"
			}
			s.Current = &m
		}
		if value, err := b.env.EC.Get(ecSwitch); err != nil {
			trigger = "error: " + err.Error()
		} else {
			trigger = fmt.Sprintf("0x%02x (bits0/1=%d%d)", value, (value&ecSwitchAck)>>1, value&ecSwitchRequest)
		}
	}

	uefi := "not available (efivarfs)"
	if b.env.Var.Exists() {
		_, data, err := b.env.Var.Read()
		switch {
		case err != nil:
			uefi = "error: " + err.Error()
		case len(data) <= legacyVarModeByte:
			uefi = fmt.Sprintf("error: uefi var too small: %d bytes", len(data))
		case data[legacyVarModeByte] == 1:
			uefi = "discrete (byte[1]=1)"
		default:
			uefi = "hybrid (byte[1]=0)"
		}
	}

	s.Details = []Detail{
		{"EC MUX (0x2E)", mux},
		{"EC switch (0xD1)", trigger},
		{fmt.Sprintf("%s[%d]", dcvar.Name, legacyVarModeByte), uefi},
	}
	return s
}

func (b *legacy) Switch(m Mode) error {
	if m != Hybrid && m != Discrete {
		return fmt.Errorf("the legacy switch does not offer %s mode", m)
	}
	discrete := m == Discrete
	uefiSet := false

	if b.env.Var.Exists() {
		if err := b.setVarMode(discrete); err != nil {
			return err
		}
		log.Info().Msgf("UEFI target set: %s", m.Label())
		uefiSet = true
	}

	if b.env.EC.Available() {
		if uefiSet {
			if err := requestECSwitch(b.env.EC); err != nil {
				log.Warn().Msgf("EC switch trigger failed: %v (is ec_sys write_support=1?)", err)
			}
		}
		if err := b.setMux(discrete); err != nil {
			if uefiSet {
				log.Warn().Msgf("EC MUX write failed: %v (is ec_sys write_support=1?)", err)
				return nil
			}
			return err
		}
		return nil
	}

	return errors.New("EC MUX is not available; cannot switch without ec_sys/debugfs")
}

func (b *legacy) setVarMode(discrete bool) error {
	attrs, data, err := b.env.Var.Read()
	if err != nil {
		return err
	}
	if len(data) <= legacyVarModeByte {
		return fmt.Errorf("uefi var too small: %d bytes", len(data))
	}
	before := data[legacyVarModeByte]
	data[legacyVarModeByte] = 0
	if discrete {
		data[legacyVarModeByte] = 1
	}
	log.Debug().Msgf("uefi %s[%d] before=0x%02x after=0x%02x", dcvar.Name, legacyVarModeByte, before, data[legacyVarModeByte])
	return b.env.Var.Write(attrs, data)
}

func (b *legacy) setMux(discrete bool) error {
	value, err := b.env.EC.Get(legacyECMux)
	if err != nil {
		return err
	}
	log.Debug().Msgf("ec mux before: 0x%02x", value)
	if discrete {
		value |= legacyECMuxMask
	} else {
		value &^= legacyECMuxMask
	}
	log.Debug().Msgf("ec mux after: 0x%02x", value)
	return b.env.EC.Set(legacyECMux, value)
}
