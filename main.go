package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"msi-gpu-switcher/internal/switcher"
	"msi-gpu-switcher/internal/sysinfo"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fatal(err)
	}
}

func init() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}

func rootCmd() *cobra.Command {
	var debug bool

	cmd := &cobra.Command{
		Use:           "msi-gpu-switcher",
		Short:         "GPU MUX switcher for MSI laptops",
		Long:          "Switch the GPU mode of MSI laptops through the firmware's UEFI variable and EC interface.",
		SilenceUsage:  true,
		SilenceErrors: true,
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
			Args:  cobra.NoArgs,
			RunE:  func(_ *cobra.Command, _ []string) error { return showStatus() },
		},
		switchCmd("hybrid", []string{"igpu"}, switcher.Hybrid),
		switchCmd("dgpu", []string{"discrete"}, switcher.Discrete),
		switchCmd("integrated", []string{"uma"}, switcher.Integrated),
	)
	return cmd
}

func switchCmd(use string, aliases []string, m switcher.Mode) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Aliases: aliases,
		Short:   "Switch to " + m.Label(),
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			requireRoot()
			return switchMode(m)
		},
	}
}

func switchMode(m switcher.Mode) error {
	b := switcher.Detect(switcher.SystemEnv())
	if b == nil {
		return errors.New("no supported GPU switch found on this machine")
	}
	if !switcher.Supports(b, m) {
		return fmt.Errorf("%s does not offer %s mode (available: %s)", b.Name(), m, joinModes(b.Modes()))
	}
	if err := b.Switch(m); err != nil {
		if errors.Is(err, switcher.ErrAlreadyActive) {
			log.Info().Msgf("Already in %s, nothing to do.", m.Label())
			return nil
		}
		return err
	}
	log.Info().Msgf("Requested %s via %s. Reboot to apply.", m.Label(), b.Name())
	return nil
}

func showStatus() error {
	env := switcher.SystemEnv()
	printMachine(env.Machine)
	printGPUs()

	b := switcher.Detect(env)
	if b == nil {
		log.Info().Msg("Backend: none")
		return nil
	}
	st := b.Status()
	log.Info().Msgf("Backend: %s", b.Name())
	log.Info().Msgf("Modes: %s", joinModes(b.Modes()))
	log.Info().Msgf("Current: %s", modeOrUnknown(st.Current))
	if st.Requested != nil {
		log.Info().Msgf("Requested: %s", st.Requested)
	}
	for _, d := range st.Details {
		log.Info().Msgf("%s: %s", d.Name, d.Value)
	}
	return nil
}

func printMachine(m sysinfo.Machine) {
	ec := m.EC
	if ec == "" {
		ec = "unknown (msi-ec not loaded)"
	}
	log.Info().Msgf("Machine: %s %s (board %s, BIOS %s, EC %s)", m.Vendor, m.Product, m.Board, m.BIOS, ec)
}

func printGPUs() {
	gpus, err := sysinfo.GPUs()
	if err != nil {
		log.Error().Msgf("GPU devices: error: %v", err)
		return
	}
	if len(gpus) == 0 {
		log.Info().Msg("GPU devices: none found")
		return
	}
	for _, g := range gpus {
		log.Info().Msgf("GPU: %s class=%s vendor=%s device=%s driver=%s", g.Addr, g.Class, g.Vendor, g.Device, g.Driver)
	}
}

func joinModes(modes []switcher.Mode) string {
	names := make([]string, len(modes))
	for i, m := range modes {
		names[i] = m.String()
	}
	return strings.Join(names, ", ")
}

func modeOrUnknown(m *switcher.Mode) string {
	if m == nil {
		return "unknown"
	}
	return m.Label()
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
