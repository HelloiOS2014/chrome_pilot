package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

var domCmd = &cobra.Command{Use: "dom", Short: "Interact with page DOM elements"}

var (
	domRef      string
	domButton   string
	domDouble   bool
	domText     string
	domSlowly   bool
	domSubmit   bool
	domStartRef string
	domEndRef   string
	domValues   string
	domFields   string
	domPaths    string
	domJS       string
	domTabID    int
)

var domClickCmd = &cobra.Command{
	Use:   "click",
	Short: "Click a DOM element",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]interface{}{
			"ref":    domRef,
			"button": domButton,
			"double": domDouble,
			"tabID":  domTabID,
		}
		return callAndPrint("dom.click", params)
	},
}

var domTypeCmd = &cobra.Command{
	Use:   "type",
	Short: "Type text into a DOM element",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]interface{}{
			"ref":    domRef,
			"text":   domText,
			"slowly": domSlowly,
			"submit": domSubmit,
			"tabID":  domTabID,
		}
		return callAndPrint("dom.type", params)
	},
}

var domHoverCmd = &cobra.Command{
	Use:   "hover",
	Short: "Hover over a DOM element",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]interface{}{
			"ref":   domRef,
			"tabID": domTabID,
		}
		return callAndPrint("dom.hover", params)
	},
}

var domDragCmd = &cobra.Command{
	Use:   "drag",
	Short: "Drag from one DOM element to another",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]interface{}{
			"startRef": domStartRef,
			"endRef":   domEndRef,
			"tabID":    domTabID,
		}
		return callAndPrint("dom.drag", params)
	},
}

var domKeyCmd = &cobra.Command{
	Use:   "key <key>",
	Short: "Press a keyboard key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]interface{}{
			"key":   args[0],
			"tabID": domTabID,
		}
		return callAndPrint("dom.key", params)
	},
}

var domSelectCmd = &cobra.Command{
	Use:   "select",
	Short: "Select options in a select element",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]interface{}{
			"ref":    domRef,
			"values": strings.Split(domValues, ","),
			"tabID":  domTabID,
		}
		return callAndPrint("dom.select", params)
	},
}

var domFillCmd = &cobra.Command{
	Use:   "fill",
	Short: "Fill multiple form fields from JSON",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]interface{}{
			"fields": domFields,
			"tabID":  domTabID,
		}
		return callAndPrint("dom.fill", params)
	},
}

var domUploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "Upload files to a file input element",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]interface{}{
			"paths": strings.Split(domPaths, ","),
			"tabID": domTabID,
		}
		return callAndPrint("dom.upload", params)
	},
}

var domEvalCmd = &cobra.Command{
	Use:   "eval",
	Short: "Evaluate JavaScript on the page",
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]interface{}{
			"js":    domJS,
			"ref":   domRef,
			"tabID": domTabID,
		}
		return callAndPrint("dom.eval", params)
	},
}

func init() {
	// click
	domClickCmd.Flags().StringVar(&domRef, "ref", "", "Element reference")
	domClickCmd.Flags().StringVar(&domButton, "button", "left", "Mouse button (left|right|middle)")
	domClickCmd.Flags().BoolVar(&domDouble, "double", false, "Double-click")
	domClickCmd.Flags().IntVar(&domTabID, "tab", 0, "Tab ID")

	// type
	domTypeCmd.Flags().StringVar(&domRef, "ref", "", "Element reference")
	domTypeCmd.Flags().StringVar(&domText, "text", "", "Text to type")
	domTypeCmd.Flags().BoolVar(&domSlowly, "slowly", false, "Type slowly (character by character)")
	domTypeCmd.Flags().BoolVar(&domSubmit, "submit", false, "Submit after typing")
	domTypeCmd.Flags().IntVar(&domTabID, "tab", 0, "Tab ID")

	// hover
	domHoverCmd.Flags().StringVar(&domRef, "ref", "", "Element reference")
	domHoverCmd.Flags().IntVar(&domTabID, "tab", 0, "Tab ID")

	// drag
	domDragCmd.Flags().StringVar(&domStartRef, "start-ref", "", "Source element reference")
	domDragCmd.Flags().StringVar(&domEndRef, "end-ref", "", "Target element reference")
	domDragCmd.Flags().IntVar(&domTabID, "tab", 0, "Tab ID")

	// key
	domKeyCmd.Flags().IntVar(&domTabID, "tab", 0, "Tab ID")

	// select
	domSelectCmd.Flags().StringVar(&domRef, "ref", "", "Element reference")
	domSelectCmd.Flags().StringVar(&domValues, "values", "", "Comma-separated values to select")
	domSelectCmd.Flags().IntVar(&domTabID, "tab", 0, "Tab ID")

	// fill
	domFillCmd.Flags().StringVar(&domFields, "fields", "", "JSON object mapping field refs to values")
	domFillCmd.Flags().IntVar(&domTabID, "tab", 0, "Tab ID")

	// upload
	domUploadCmd.Flags().StringVar(&domPaths, "paths", "", "Comma-separated file paths to upload")
	domUploadCmd.Flags().IntVar(&domTabID, "tab", 0, "Tab ID")

	// eval
	domEvalCmd.Flags().StringVar(&domJS, "js", "", "JavaScript expression or function")
	domEvalCmd.Flags().StringVar(&domRef, "ref", "", "Element reference (optional)")
	domEvalCmd.Flags().IntVar(&domTabID, "tab", 0, "Tab ID")

	domCmd.AddCommand(
		domClickCmd,
		domTypeCmd,
		domHoverCmd,
		domDragCmd,
		domKeyCmd,
		domSelectCmd,
		domFillCmd,
		domUploadCmd,
		domEvalCmd,
	)
	rootCmd.AddCommand(domCmd)
}
