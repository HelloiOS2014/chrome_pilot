package cmd

import "github.com/spf13/cobra"

var cookieCmd = &cobra.Command{Use: "cookie", Short: "Manage cookies"}

var cookieListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cookies",
	RunE: func(cmd *cobra.Command, args []string) error {
		domain, _ := cmd.Flags().GetString("domain")
		params := map[string]interface{}{}
		if domain != "" {
			params["domain"] = domain
		}
		return callAndPrint("cookie.list", params)
	},
}

var cookieGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get a specific cookie",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		domain, _ := cmd.Flags().GetString("domain")
		return callAndPrint("cookie.get", map[string]string{"name": name, "domain": domain})
	},
}

func init() {
	cookieListCmd.Flags().String("domain", "", "Filter by domain")
	cookieGetCmd.Flags().String("name", "", "Cookie name")
	cookieGetCmd.Flags().String("domain", "", "Cookie domain")
	cookieCmd.AddCommand(cookieListCmd, cookieGetCmd)
	rootCmd.AddCommand(cookieCmd)
}
