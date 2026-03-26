package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the chrome-pilot daemon",
	RunE:  runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("stop: home dir: %w", err)
	}
	pidFile := filepath.Join(home, ".chrome-pilot", "daemon.pid")

	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("{\"status\":\"not running\"}")
			return nil
		}
		return fmt.Errorf("stop: read pid file: %w", err)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		_ = os.Remove(pidFile)
		fmt.Println("{\"status\":\"not running\"}")
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		_ = os.Remove(pidFile)
		fmt.Println("{\"status\":\"not running\"}")
		return nil
	}

	// Check if process is actually alive before sending SIGTERM.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		_ = os.Remove(pidFile)
		fmt.Println("{\"status\":\"not running\"}")
		return nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop: send SIGTERM to pid %d: %w", pid, err)
	}

	fmt.Printf("{\"status\":\"stopped\",\"pid\":%d}\n", pid)
	return nil
}
