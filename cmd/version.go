package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of mittinit",
	Long:  `All software has versions. This is mittinit's`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("mittinit v0.1 -- HEAD")
	},
}
