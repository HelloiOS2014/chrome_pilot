package cmd

import (
	"fmt"

	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/joyy/chrome-pilot/internal/sockutil"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the status of the chrome-pilot daemon",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfgPath, _ := config.ConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	result, err := sockutil.Call(cfg.SocketPath, "status", nil)
	if err != nil {
		fmt.Println("{\"daemon\":\"not running\",\"extension\":\"unknown\"}")
		return nil
	}

	fmt.Println(string(result))
	return nil
}
