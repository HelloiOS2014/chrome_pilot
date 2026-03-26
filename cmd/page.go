package cmd

import "github.com/spf13/cobra"

var pageCmd = &cobra.Command{
	Use:   "page",
	Short: "Page-level operations",
}

// --- per-command flag variables ---

var (
	pageTabID int

	// navigate
	pageNavURL string

	// screenshot
	pageScreenshotFull bool
	pageScreenshotRef  string
	pageScreenshotFile string

	// wait
	pageWaitText     string
	pageWaitTextGone string
	pageWaitTime     int

	// console
	pageConsoleLevel string

	// network
	pageNetworkIncludeStatic bool

	// content
	pageContentFormat string

	// resize
	pageResizeWidth  int
	pageResizeHeight int

	// dialog
	pageDialogAccept bool
	pageDialogText   string
)

// navigate — navigate the active tab to a URL
var pageNavigateCmd = &cobra.Command{
	Use:   "navigate <url>",
	Short: "Navigate the page to a URL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.navigate", map[string]interface{}{
			"url":   args[0],
			"tabId": pageTabID,
		})
	},
}

// back — navigate back
var pageBackCmd = &cobra.Command{
	Use:   "back",
	Short: "Navigate back in browser history",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.back", map[string]interface{}{
			"tabId": pageTabID,
		})
	},
}

// screenshot — capture a screenshot
var pageScreenshotCmd = &cobra.Command{
	Use:   "screenshot",
	Short: "Capture a screenshot of the page",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.screenshot", map[string]interface{}{
			"tabId": pageTabID,
			"full":  pageScreenshotFull,
			"ref":   pageScreenshotRef,
			"file":  pageScreenshotFile,
		})
	},
}

// wait — wait for a condition
var pageWaitCmd = &cobra.Command{
	Use:   "wait",
	Short: "Wait for a condition on the page",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.wait", map[string]interface{}{
			"tabId":     pageTabID,
			"text":      pageWaitText,
			"textGone":  pageWaitTextGone,
			"time":      pageWaitTime,
		})
	},
}

// console — retrieve console log entries
var pageConsoleCmd = &cobra.Command{
	Use:   "console",
	Short: "Retrieve console log entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.console", map[string]interface{}{
			"tabId": pageTabID,
			"level": pageConsoleLevel,
		})
	},
}

// network — retrieve captured network requests
var pageNetworkCmd = &cobra.Command{
	Use:   "network",
	Short: "Retrieve captured network requests",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.network", map[string]interface{}{
			"tabId":         pageTabID,
			"includeStatic": pageNetworkIncludeStatic,
		})
	},
}

// content — get the page source/content
var pageContentCmd = &cobra.Command{
	Use:   "content",
	Short: "Get page source content",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.content", map[string]interface{}{
			"tabId":  pageTabID,
			"format": pageContentFormat,
		})
	},
}

// resize — resize the browser viewport
var pageResizeCmd = &cobra.Command{
	Use:   "resize",
	Short: "Resize the browser viewport",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.resize", map[string]interface{}{
			"tabId":  pageTabID,
			"width":  pageResizeWidth,
			"height": pageResizeHeight,
		})
	},
}

// dialog — handle a browser dialog (alert/confirm/prompt)
var pageDialogCmd = &cobra.Command{
	Use:   "dialog",
	Short: "Handle a browser dialog (alert/confirm/prompt)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.dialog", map[string]interface{}{
			"tabId":  pageTabID,
			"accept": pageDialogAccept,
			"text":   pageDialogText,
		})
	},
}

// close — close the current page/tab
var pageCloseCmd = &cobra.Command{
	Use:   "close",
	Short: "Close the current page",
	RunE: func(cmd *cobra.Command, args []string) error {
		return callAndPrint("page.close", map[string]interface{}{
			"tabId": pageTabID,
		})
	},
}

func init() {
	// Global --tab flag shared by all page subcommands (added to pageCmd so
	// each subcommand inherits it via PersistentFlags).
	pageCmd.PersistentFlags().IntVar(&pageTabID, "tab", 0, "Target tab ID (0 = active tab)")

	// screenshot flags
	pageScreenshotCmd.Flags().BoolVar(&pageScreenshotFull, "full", false, "Capture full-page screenshot")
	pageScreenshotCmd.Flags().StringVar(&pageScreenshotRef, "ref", "", "Element ref to screenshot")
	pageScreenshotCmd.Flags().StringVar(&pageScreenshotFile, "file", "", "Save screenshot to file path")

	// wait flags
	pageWaitCmd.Flags().StringVar(&pageWaitText, "text", "", "Wait until text appears on the page")
	pageWaitCmd.Flags().StringVar(&pageWaitTextGone, "text-gone", "", "Wait until text disappears from the page")
	pageWaitCmd.Flags().IntVar(&pageWaitTime, "time", 0, "Wait for a fixed number of milliseconds")

	// console flags
	pageConsoleCmd.Flags().StringVar(&pageConsoleLevel, "level", "", "Filter by log level (log, warn, error, info)")

	// network flags
	pageNetworkCmd.Flags().BoolVar(&pageNetworkIncludeStatic, "include-static", false, "Include static asset requests")

	// content flags
	pageContentCmd.Flags().StringVar(&pageContentFormat, "format", "", "Content format (html, text, markdown)")

	// resize flags
	pageResizeCmd.Flags().IntVar(&pageResizeWidth, "width", 0, "Viewport width in pixels")
	pageResizeCmd.Flags().IntVar(&pageResizeHeight, "height", 0, "Viewport height in pixels")

	// dialog flags
	pageDialogCmd.Flags().BoolVar(&pageDialogAccept, "accept", true, "Accept the dialog (default true)")
	pageDialogCmd.Flags().StringVar(&pageDialogText, "text", "", "Text to enter into a prompt dialog")

	// register all subcommands
	pageCmd.AddCommand(
		pageNavigateCmd,
		pageBackCmd,
		pageScreenshotCmd,
		pageWaitCmd,
		pageConsoleCmd,
		pageNetworkCmd,
		pageContentCmd,
		pageResizeCmd,
		pageDialogCmd,
		pageCloseCmd,
	)

	rootCmd.AddCommand(pageCmd)
}
