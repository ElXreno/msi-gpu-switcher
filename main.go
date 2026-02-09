package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

const (
	ecIOPath = "/sys/kernel/debug/ec/ec0/io"
)

const (
	ecMuxOffset    = 0x2e
	ecMuxMask      = 0x40
	ecSwitchOffset = 0xd1
	ecSwitchMask0  = 0x01
	ecSwitchMask1  = 0x02
)

const (
	uefiVarName  = "MsiDCVarData"
	uefiVarGuid  = "DD96BAAF-145E-4F56-B1CF-193256298E99"
	uefiDataBase = 4
	uefiModeByte = 1
)

var (
	uefiVarPath = "/sys/firmware/efi/efivars/MsiDCVarData-DD96BAAF-145E-4F56-B1CF-193256298E99"
)

type gpuInfo struct {
	addr   string
	class  string
	vendor string
	device string
	driver string
}

func main() {
	if err := rootCmd().Execute(); err != nil {
		fatal(err)
	}
}

func showStatus() error {
	printGpuDevices()
	printEcMux()
	printEcSwitch()
	printUefiVar()
	return nil
}

func printGpuDevices() {
	log.Info().Msg("GPU devices:")
	gpus, err := listGPUs()
	if err != nil {
		log.Error().Msgf("  error: %v", err)
		return
	}
	if len(gpus) == 0 {
		log.Info().Msg("  (none found)")
		return
	}
	for _, g := range gpus {
		log.Info().Msgf("  %s class=%s vendor=%s device=%s driver=%s",
			g.addr, g.class, g.vendor, g.device, g.driver)
	}
}

func printEcMux() {
	log.Info().Msg("")
	log.Info().Msg("EC MUX:")
	if !exists(ecIOPath) {
		log.Info().Msg("  not available (ec_sys/debugfs)")
		return
	}
	state, err := readEcMuxState()
	if err != nil {
		log.Error().Msgf("  error: %v", err)
		return
	}
	if state {
		log.Info().Msg("  discrete (PXCT=1)")
	} else {
		log.Info().Msg("  hybrid (PXCT=0)")
	}
}

func printEcSwitch() {
	log.Info().Msg("")
	log.Info().Msg("EC switch trigger:")
	if !exists(ecIOPath) {
		log.Info().Msg("  not available (ec_sys/debugfs)")
		return
	}
	value, err := readEcByte(ecSwitchOffset)
	if err != nil {
		log.Error().Msgf("  error: %v", err)
		return
	}
	log.Info().Msgf("  0x%02x (bits0/1=%d%d)", value, (value&ecSwitchMask1)>>1, value&ecSwitchMask0)
}

func printUefiVar() {
	log.Info().Msg("")
	log.Info().Msg("UEFI var:")
	if !exists(uefiVarPath) {
		log.Info().Msg("  not available (efivarfs)")
		return
	}
	state, err := readUefiGpuMode()
	if err != nil {
		log.Error().Msgf("  error: %v", err)
		return
	}
	if state {
		log.Info().Msg("  discrete (byte[1]=1)")
	} else {
		log.Info().Msg("  hybrid (byte[1]=0)")
	}
}

func switchIGPU() error {
	return switchGPU(false)
}

func switchDGPU() error {
	return switchGPU(true)
}

func switchGPU(discrete bool) error {
	uefiSet := false
	if exists(uefiVarPath) {
		if err := setUefiGpuMode(discrete); err != nil {
			return err
		}
		if discrete {
			log.Info().Msg("UEFI target set: dGPU (discrete)")
		} else {
			log.Info().Msg("UEFI target set: iGPU (hybrid)")
		}
		uefiSet = true
	}

	if exists(ecIOPath) {
		if uefiSet {
			if err := triggerEcSwitch(); err != nil {
				log.Warn().Msgf("EC switch trigger failed: %v (is ec_sys write_support=1?)", err)
			}
		}
		if err := setEcMux(discrete); err != nil {
			if uefiSet {
				log.Warn().Msgf("EC MUX write failed: %v (is ec_sys write_support=1?)", err)
				return nil
			}
			return err
		}
		if discrete {
			log.Info().Msg("Requested primary GPU: dGPU (EC MUX)")
		} else {
			log.Info().Msg("Requested primary GPU: iGPU (EC MUX)")
		}
		return nil
	}

	return errors.New("EC MUX is not available; cannot switch without ec_sys/debugfs")
}

func listGPUs() ([]gpuInfo, error) {
	entries, err := filepath.Glob("/sys/bus/pci/devices/*")
	if err != nil {
		return nil, err
	}

	var gpus []gpuInfo
	for _, entry := range entries {
		class := strings.TrimSpace(readFirstLine(filepath.Join(entry, "class")))
		if class == "" {
			continue
		}
		if !strings.HasPrefix(class, "0x0300") && !strings.HasPrefix(class, "0x0302") {
			continue
		}
		info := gpuInfo{
			addr:   filepath.Base(entry),
			class:  class,
			vendor: strings.TrimSpace(readFirstLine(filepath.Join(entry, "vendor"))),
			device: strings.TrimSpace(readFirstLine(filepath.Join(entry, "device"))),
			driver: readDriver(entry),
		}
		gpus = append(gpus, info)
	}

	return gpus, nil
}

func readDriver(devPath string) string {
	link := filepath.Join(devPath, "driver")
	target, err := os.Readlink(link)
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
	defer f.Close()
	r := bufio.NewReader(f)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func requireRoot() {
	if os.Geteuid() != 0 {
		fatal(errors.New("this command requires root"))
	}
}

func fatal(err error) {
	log.Error().Msgf("error: %v", err)
	os.Exit(1)
}

func readEcMuxState() (bool, error) {
	value, err := readEcByte(ecMuxOffset)
	if err != nil {
		return false, err
	}
	return (value & ecMuxMask) != 0, nil
}

func setEcMux(discrete bool) error {
	value, err := readEcByte(ecMuxOffset)
	if err != nil {
		return err
	}
	log.Debug().Msgf("ec mux before: 0x%02x", value)
	if discrete {
		value |= ecMuxMask
	} else {
		value &^= ecMuxMask
	}
	log.Debug().Msgf("ec mux after: 0x%02x", value)
	return writeEcByte(ecMuxOffset, value)
}

func readEcByte(offset int) (byte, error) {
	f, err := os.Open(ecIOPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := []byte{0}
	if _, err := f.ReadAt(buf, int64(offset)); err != nil {
		return 0, err
	}
	log.Debug().Msgf("ec read [0x%02x]=0x%02x", offset, buf[0])
	return buf[0], nil
}

func writeEcByte(offset int, value byte) error {
	f, err := os.OpenFile(ecIOPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	log.Debug().Msgf("ec write [0x%02x]=0x%02x", offset, value)
	if _, err := f.WriteAt([]byte{value}, int64(offset)); err != nil {
		return err
	}
	return nil
}

func readUefiGpuMode() (bool, error) {
	_, data, err := readUefiVar()
	if err != nil {
		return false, err
	}
	if len(data) <= uefiModeByte {
		return false, fmt.Errorf("uefi var too small: %d bytes", len(data))
	}
	return data[uefiModeByte] == 1, nil
}

func setUefiGpuMode(discrete bool) error {
	attrs, data, err := readUefiVar()
	if err != nil {
		return err
	}
	if len(data) <= uefiModeByte {
		return fmt.Errorf("uefi var too small: %d bytes", len(data))
	}
	before := data[uefiModeByte]
	if discrete {
		data[uefiModeByte] = 1
	} else {
		data[uefiModeByte] = 0
	}
	log.Debug().Msgf("uefi %s[%d] before=0x%02x after=0x%02x", uefiVarName, uefiModeByte, before, data[uefiModeByte])
	return writeUefiVar(attrs, data)
}

func readUefiVar() (uint32, []byte, error) {
	raw, err := os.ReadFile(uefiVarPath)
	if err != nil {
		return 0, nil, err
	}
	if len(raw) < uefiDataBase {
		return 0, nil, fmt.Errorf("uefi var too small: %d bytes", len(raw))
	}
	attrs := binary.LittleEndian.Uint32(raw[:uefiDataBase])
	data := make([]byte, len(raw)-uefiDataBase)
	copy(data, raw[uefiDataBase:])
	log.Debug().Msgf("uefi %s attrs=0x%08x len=%d", uefiVarName, attrs, len(data))
	return attrs, data, nil
}

func writeUefiVar(attrs uint32, data []byte) error {
	payload := make([]byte, uefiDataBase+len(data))
	binary.LittleEndian.PutUint32(payload[:uefiDataBase], attrs)
	copy(payload[uefiDataBase:], data)
	restore, err := makeUefiVarMutable()
	if err != nil {
		return fmt.Errorf("prepare uefi var failed: %w", err)
	}
	if err := os.WriteFile(uefiVarPath, payload, 0o644); err != nil {
		if restore != nil {
			restore()
		}
		return fmt.Errorf("write uefi var failed: %w", err)
	}
	if restore != nil {
		restore()
	}
	return nil
}

func triggerEcSwitch() error {
	value, err := readEcByte(ecSwitchOffset)
	if err != nil {
		return err
	}
	log.Debug().Msgf("ec switch before: 0x%02x", value)
	value &^= (ecSwitchMask0 | ecSwitchMask1)
	value |= ecSwitchMask0
	log.Debug().Msgf("ec switch after: 0x%02x", value)
	return writeEcByte(ecSwitchOffset, value)
}

func init() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}

func rootCmd() *cobra.Command {
	var debug bool

	cmd := &cobra.Command{
		Use:   "gpu-switcher",
		Short: "GPU MUX switcher for MSI laptops",
		Long: "Switch primary GPU output using UEFI vars and EC trigger.",
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			if debug {
				zerolog.SetGlobalLevel(zerolog.DebugLevel)
			}
		},
	}

	cmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging")

	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show current GPU/MUX/UEFI status",
			RunE: func(_ *cobra.Command, _ []string) error {
				return showStatus()
			},
		},
		&cobra.Command{
			Use:   "igpu",
			Short: "Switch to iGPU (hybrid)",
			RunE: func(_ *cobra.Command, _ []string) error {
				requireRoot()
				return switchIGPU()
			},
		},
		&cobra.Command{
			Use:   "dgpu",
			Short: "Switch to dGPU (discrete)",
			RunE: func(_ *cobra.Command, _ []string) error {
				requireRoot()
				return switchDGPU()
			},
		},
	)

	return cmd
}

func makeUefiVarMutable() (func(), error) {
	fd, err := os.Open(uefiVarPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	// Try ioctl first
	flags, err := unix.IoctlGetInt(int(fd.Fd()), unix.FS_IOC_GETFLAGS)
	if err == nil {
		if flags&int(unix.STATX_ATTR_IMMUTABLE) == 0 {
			_ = fd.Close()
			return nil, nil
		}
		newFlags := flags &^ int(unix.STATX_ATTR_IMMUTABLE)
		if err := unix.IoctlSetInt(int(fd.Fd()), unix.FS_IOC_SETFLAGS, newFlags); err == nil {
			log.Debug().Msg("cleared immutable flag via ioctl")
			restore := func() {
				if err := unix.IoctlSetInt(int(fd.Fd()), unix.FS_IOC_SETFLAGS, flags); err != nil {
					log.Warn().Msgf("ioctl restore failed (%v), falling back to chattr +i", err)
					_ = fd.Close()
					cmd := exec.Command("chattr", "+i", uefiVarPath)
					if out, err := cmd.CombinedOutput(); err != nil {
						log.Warn().Msgf("chattr +i failed: %v (%s)", err, strings.TrimSpace(string(out)))
					}
					return
				}
				_ = fd.Close()
			}
			return restore, nil
		}
	}
	_ = fd.Close()

	// Fallback to chattr
	log.Debug().Msg("ioctl failed, falling back to chattr -i")
	cmd := exec.Command("chattr", "-i", uefiVarPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("chattr -i failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	restore := func() {
		rfd, err := os.Open(uefiVarPath)
		if err == nil {
			flags, err := unix.IoctlGetInt(int(rfd.Fd()), unix.FS_IOC_GETFLAGS)
			if err == nil {
				newFlags := flags | int(unix.STATX_ATTR_IMMUTABLE)
				if err := unix.IoctlSetInt(int(rfd.Fd()), unix.FS_IOC_SETFLAGS, newFlags); err == nil {
					log.Debug().Msg("restored immutable flag via ioctl")
					_ = rfd.Close()
					return
				}
			}
			_ = rfd.Close()
		}
		log.Debug().Msg("ioctl restore failed, falling back to chattr +i")
		cmd := exec.Command("chattr", "+i", uefiVarPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Warn().Msgf("chattr +i failed: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	}
	return restore, nil
}

