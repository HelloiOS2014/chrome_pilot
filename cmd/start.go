package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/joyy/chrome-pilot/internal/daemon"
	"github.com/spf13/cobra"
)

var foreground bool

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the chrome-pilot daemon",
	RunE:  runStart,
}

func init() {
	startCmd.Flags().BoolVar(&foreground, "foreground", false, "Run in the foreground")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	cfgPath, _ := config.ConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	dataDir, err := config.DataDir()
	if err != nil {
		return err
	}

	if foreground {
		d, err := daemon.New(cfg, dataDir)
		if err != nil {
			return err
		}
		return d.Start()
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	child := exec.Command(self, "start", "--foreground")
	child.Stdout = nil
	child.Stderr = nil
	child.Stdin = nil
	if err := child.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	fmt.Printf("{\"status\":\"started\",\"pid\":%d}\n", child.Process.Pid)
	return nil
}
