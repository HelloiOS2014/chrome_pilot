package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/joyy/chrome-pilot/internal/config"
	"github.com/joyy/chrome-pilot/internal/sockutil"
	"github.com/spf13/cobra"
)

// callAndPrint is the shared helper used by all CLI commands. It loads the
// config, calls the daemon RPC over the Unix socket, and prints the result
// (or an error JSON object) to stdout.
func callAndPrint(method string, params interface{}) error {
	cfgPath, _ := config.ConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	result, err := sockutil.Call(cfg.SocketPath, method, params)
	if err != nil {
		errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Println(string(errJSON))
		return nil
	}
	fmt.Println(string(result))
	return nil
}

var tabCmd = &cobra.Command{
	Use:   "tab",
	Short: "Manage browser tabs",
}

var tabListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all open tabs",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("tab.list", nil)
	},
}

var tabNewCmd = &cobra.Command{
	Use:   "new <url>",
	Short: "Open a new tab",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("tab.new", map[string]string{"url": args[0]})
	},
}

var tabSelectCmd = &cobra.Command{
	Use:   "select <index>",
	Short: "Switch to a tab by index",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("tab.select", map[string]string{"index": args[0]})
	},
}

var tabCloseCmd = &cobra.Command{
	Use:   "close [index]",
	Short: "Close a tab",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]interface{}{}
		if len(args) > 0 {
			params["index"] = args[0]
		}
		return callAndPrint("tab.close", params)
	},
}

func init() {
	tabCmd.AddCommand(tabListCmd, tabNewCmd, tabSelectCmd, tabCloseCmd)
	rootCmd.AddCommand(tabCmd)
}
