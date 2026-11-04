package sysinfo

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const (
	dmiPath   = "/sys/class/dmi/id"
	msiECPath = "/sys/devices/platform/msi-ec"
	pciPath   = "/sys/bus/pci/devices"
)

type Machine struct {
	Vendor, Product, Board, BIOS, EC string
}

func DetectMachine() Machine {
	return Machine{
		Vendor:  readFirstLine(filepath.Join(dmiPath, "sys_vendor")),
		Product: readFirstLine(filepath.Join(dmiPath, "product_name")),
		Board:   readFirstLine(filepath.Join(dmiPath, "board_name")),
		BIOS:    readFirstLine(filepath.Join(dmiPath, "bios_version")),
		EC:      readFirstLine(filepath.Join(msiECPath, "fw_version")),
	}
}

type GPU struct {
	Addr, Class, Vendor, Device, Driver string
}

func GPUs() ([]GPU, error) {
	entries, err := filepath.Glob(filepath.Join(pciPath, "*"))
	if err != nil {
		return nil, err
	}
	var gpus []GPU
	for _, entry := range entries {
		class := readFirstLine(filepath.Join(entry, "class"))
		if !strings.HasPrefix(class, "0x0300") && !strings.HasPrefix(class, "0x0302") {
			continue
		}
		gpus = append(gpus, GPU{
			Addr:   filepath.Base(entry),
			Class:  class,
			Vendor: readFirstLine(filepath.Join(entry, "vendor")),
			Device: readFirstLine(filepath.Join(entry, "device")),
			Driver: readDriver(entry),
		})
	}
	return gpus, nil
}

func readDriver(devPath string) string {
	target, err := os.Readlink(filepath.Join(devPath, "driver"))
	if err != nil {
		return "unknown"
	}
	return filepath.Base(target)
}

func readFirstLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	line, _ := bufio.NewReader(f).ReadString('\n')
	return strings.TrimSpace(line)
}
