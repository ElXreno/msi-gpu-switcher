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

// EC
const (
	ecIOPath       = "/sys/kernel/debug/ec/ec0/io"
	ecMuxOffset    = 0x2e
	ecMuxMask      = 0x40
	ecSwitchOffset = 0xd1
	ecSwitchMask0  = 0x01
	ecSwitchMask1  = 0x02
)

// UEFI
const (
	uefiVarName  = "MsiDCVarData"
	uefiVarGuid  = "DD96BAAF-145E-4F56-B1CF-193256298E99"
	uefiDataBase = 4
	uefiModeByte = 1
)

// UEFI too
var uefiVarPath = "/sys/firmware/efi/efivars/" + uefiVarName + "-" + uefiVarGuid

type gpuInfo struct {
	addr, class, vendor, device, driver string
}

func main() {
	if err := rootCmd().Execute(); err != nil {
		fatal(err)
	}
}

func gpuLabel(discrete bool) string {
	if discrete {
		return "dGPU (discrete)"
	}
	return "iGPU (hybrid)"
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
	label := "hybrid (PXCT=0)"
	if state {
		label = "discrete (PXCT=1)"
	}
	log.Info().Msgf("  %s", label)
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
	label := "hybrid (byte[1]=0)"
	if state {
		label = "discrete (byte[1]=1)"
	}
	log.Info().Msgf("  %s", label)
}

func switchGPU(discrete bool) error {
	label := gpuLabel(discrete)
	uefiSet := false

	if exists(uefiVarPath) {
		if err := setUefiGpuMode(discrete); err != nil {
			return err
		}
		log.Info().Msgf("UEFI target set: %s", label)
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
		log.Info().Msgf("Requested primary GPU: %s (EC MUX)", label)
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
		gpus = append(gpus, gpuInfo{
			addr:   filepath.Base(entry),
			class:  class,
			vendor: strings.TrimSpace(readFirstLine(filepath.Join(entry, "vendor"))),
			device: strings.TrimSpace(readFirstLine(filepath.Join(entry, "device"))),
			driver: readDriver(entry),
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
	defer f.Close()
	line, _ := bufio.NewReader(f).ReadString('\n')
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
	_, err = f.WriteAt([]byte{value}, int64(offset))
	return err
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
	if restore != nil {
		defer restore()
	}

	if err := os.WriteFile(uefiVarPath, payload, 0o644); err != nil {
		return fmt.Errorf("write uefi var failed: %w", err)
	}
	return nil
}

func triggerEcSwitch() error {
	value, err := readEcByte(ecSwitchOffset)
	if err != nil {
		return err
	}
	log.Debug().Msgf("ec switch before: 0x%02x", value)
	value &^= ecSwitchMask0 | ecSwitchMask1
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
		Use:   "msi-gpu-switcher",
		Short: "GPU MUX switcher for MSI laptops",
		Long:  "Switch primary GPU output using UEFI vars and EC trigger.",
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
			RunE:  func(_ *cobra.Command, _ []string) error { return showStatus() },
		},
		&cobra.Command{
			Use:   "igpu",
			Short: "Switch to iGPU (hybrid)",
			RunE: func(_ *cobra.Command, _ []string) error {
				requireRoot()
				return switchGPU(false)
			},
		},
		&cobra.Command{
			Use:   "dgpu",
			Short: "Switch to dGPU (discrete)",
			RunE: func(_ *cobra.Command, _ []string) error {
				requireRoot()
				return switchGPU(true)
			},
		},
	)
	return cmd
}

func restoreImmutable(path string) {
	f, err := os.Open(path)
	if err == nil {
		flags, err := unix.IoctlGetInt(int(f.Fd()), unix.FS_IOC_GETFLAGS)
		if err == nil {
			if err := unix.IoctlSetInt(int(f.Fd()), unix.FS_IOC_SETFLAGS, flags|int(unix.STATX_ATTR_IMMUTABLE)); err == nil {
				log.Debug().Msg("restored immutable flag via ioctl")
				_ = f.Close()
				return
			}
		}
		_ = f.Close()
	}
	log.Debug().Msg("ioctl restore failed, falling back to chattr +i")
	if out, err := exec.Command("chattr", "+i", path).CombinedOutput(); err != nil {
		log.Warn().Msgf("chattr +i failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
}

func makeUefiVarMutable() (func(), error) {
	fd, err := os.Open(uefiVarPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer fd.Close()

	flags, err := unix.IoctlGetInt(int(fd.Fd()), unix.FS_IOC_GETFLAGS)
	if err == nil {
		if flags&int(unix.STATX_ATTR_IMMUTABLE) == 0 {
			return nil, nil
		}
		newFlags := flags &^ int(unix.STATX_ATTR_IMMUTABLE)
		if err := unix.IoctlSetInt(int(fd.Fd()), unix.FS_IOC_SETFLAGS, newFlags); err == nil {
			log.Debug().Msg("cleared immutable flag via ioctl")
			return func() { restoreImmutable(uefiVarPath) }, nil
		}
	}

	log.Debug().Msg("ioctl failed, falling back to chattr -i")
	if out, err := exec.Command("chattr", "-i", uefiVarPath).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("chattr -i failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return func() { restoreImmutable(uefiVarPath) }, nil
}
