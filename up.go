package cmd

import (
	"encoding/json"
	"fmt"
	"mittinit/config"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(upCmd)
}

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Run jobs defined in HCL configuration files",
	Long:  `The up command is the core of mittinit. It reads HCL configuration files and runs the defined jobs.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Loading configuration from directory: %s\n", configDir)
		cfg, err := config.LoadConfig(configDir)
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			return
		}

		fmt.Println("Configuration loaded successfully. Verifying content...")
		if cfg == nil {
			fmt.Println("Config is nil after loading.")
			return
		}

		// Print the loaded configuration as JSON for verification
		cfgJSON, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			fmt.Printf("Error marshalling config to JSON: %v\n", err)
			return
		}
		fmt.Println(string(cfgJSON))
	},
}
