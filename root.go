package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	api       bool
	configDir string
)

var rootCmd = &cobra.Command{
	Use:   "mittinit",
	Short: "mittinit is a simple init system",
	Long:  `mittinit is a simple init system that can run jobs defined in HCL configuration files.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Do nothing by default
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&api, "api", false, "ignored for now, for future use")
	rootCmd.PersistentFlags().StringVarP(&configDir, "config-dir", "c", "/etc/mittnite.d", "set directory to where your .hcl-configs are located")
}
