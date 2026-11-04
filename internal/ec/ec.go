package ec

import (
	"os"

	"github.com/rs/zerolog/log"
)

const DebugfsPath = "/sys/kernel/debug/ec/ec0/io"

type Controller interface {
	Available() bool
	Get(offset byte) (byte, error)
	Set(offset, value byte) error
}

type Debugfs struct {
	Path string
}

func (d Debugfs) Available() bool {
	_, err := os.Stat(d.Path)
	return err == nil
}

func (d Debugfs) Get(offset byte) (byte, error) {
	f, err := os.Open(d.Path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	buf := []byte{0}
	if _, err := f.ReadAt(buf, int64(offset)); err != nil {
		return 0, err
	}
	log.Debug().Msgf("ec read [0x%02x]=0x%02x", offset, buf[0])
	return buf[0], nil
}

func (d Debugfs) Set(offset, value byte) error {
	f, err := os.OpenFile(d.Path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	log.Debug().Msgf("ec write [0x%02x]=0x%02x", offset, value)
	_, err = f.WriteAt([]byte{value}, int64(offset))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
