package cmd

import "github.com/spf13/cobra"

var (
	snapRef          string
	snapDepth        int
	snapRole         string
	snapSearch       string
	snapInteractable bool
	snapTabID        int
)

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Capture and query page accessibility snapshot",
	RunE:  runSnapshot,
}

var snapshotInfoCmd = &cobra.Command{
	Use: "info", Short: "Show snapshot cache status",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("snapshot.info", nil)
	},
}

var snapshotClearCmd = &cobra.Command{
	Use: "clear", Short: "Clear snapshot cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("snapshot.clear", map[string]int{"tabId": snapTabID})
	},
}

func init() {
	snapshotCmd.Flags().StringVar(&snapRef, "ref", "", "Expand subtree by ref")
	snapshotCmd.Flags().IntVar(&snapDepth, "depth", 0, "Limit expansion depth")
	snapshotCmd.Flags().StringVar(&snapRole, "role", "", "Filter by ARIA role")
	snapshotCmd.Flags().StringVar(&snapSearch, "search", "", "Search by text")
	snapshotCmd.Flags().BoolVar(&snapInteractable, "interactable", false, "Show only interactable elements")
	snapshotCmd.Flags().IntVar(&snapTabID, "tab", 0, "Target tab ID")
	snapshotCmd.AddCommand(snapshotInfoCmd, snapshotClearCmd)
	rootCmd.AddCommand(snapshotCmd)
}

func runSnapshot(cmd *cobra.Command, args []string) error {
	if snapRef != "" || snapRole != "" || snapSearch != "" || snapInteractable {
		return callAndPrint("snapshot.query", map[string]interface{}{
			"ref": snapRef, "depth": snapDepth, "role": snapRole,
			"search": snapSearch, "interactable": snapInteractable, "tabId": snapTabID,
		})
	}
	return callAndPrint("snapshot", map[string]interface{}{"tabId": snapTabID})
}
