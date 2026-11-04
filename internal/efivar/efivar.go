package efivar

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

const (
	attrsLen      = 4
	fsImmutableFl = 0x10
)

type Var struct {
	Path string
}

func (v Var) Exists() bool {
	_, err := os.Stat(v.Path)
	return err == nil
}

func (v Var) Read() (uint32, []byte, error) {
	raw, err := os.ReadFile(v.Path)
	if err != nil {
		return 0, nil, err
	}
	if len(raw) < attrsLen {
		return 0, nil, fmt.Errorf("uefi var too small: %d bytes", len(raw))
	}
	attrs := binary.LittleEndian.Uint32(raw[:attrsLen])
	data := make([]byte, len(raw)-attrsLen)
	copy(data, raw[attrsLen:])
	log.Debug().Msgf("uefi %s attrs=0x%08x len=%d", v.Path, attrs, len(data))
	return attrs, data, nil
}

func (v Var) Write(attrs uint32, data []byte) error {
	payload := make([]byte, attrsLen+len(data))
	binary.LittleEndian.PutUint32(payload[:attrsLen], attrs)
	copy(payload[attrsLen:], data)

	wasImmutable, err := v.setImmutable(false)
	if err != nil {
		return fmt.Errorf("clear immutable flag: %w", err)
	}
	if wasImmutable {
		defer func() {
			if _, err := v.setImmutable(true); err != nil {
				log.Warn().Msgf("restore immutable flag on %s: %v", v.Path, err)
			}
		}()
	}

	if err := os.WriteFile(v.Path, payload, 0o644); err != nil {
		return fmt.Errorf("write uefi var failed: %w", err)
	}
	return nil
}

func (v Var) setImmutable(on bool) (bool, error) {
	f, err := os.Open(v.Path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	flags, err := unix.IoctlGetUint32(int(f.Fd()), unix.FS_IOC_GETFLAGS)
	if errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.EOPNOTSUPP) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	was := flags&fsImmutableFl != 0
	if was == on {
		return was, nil
	}
	flags ^= fsImmutableFl
	if err := unix.IoctlSetPointerInt(int(f.Fd()), unix.FS_IOC_SETFLAGS, int(flags)); err != nil {
		return was, err
	}
	log.Debug().Msgf("immutable flag on %s set to %v", v.Path, on)
	return was, nil
}
