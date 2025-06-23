package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2/hclsimple"
)

// LoadConfig reads all .hcl files in the given directory path,
// parses them, and merges them into a single Config struct.
func LoadConfig(dirPath string) (*Config, error) {
	mergedConfig := &Config{}

	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config directory %s: %w", dirPath, err)
	}

	var hclFiles []string
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".hcl" {
			hclFiles = append(hclFiles, filepath.Join(dirPath, file.Name()))
		}
	}

	if len(hclFiles) == 0 {
		return nil, fmt.Errorf("no .hcl files found in %s", dirPath)
	}

	for _, filePath := range hclFiles {
		var configFile ConfigFile
		err := hclsimple.DecodeFile(filePath, nil, &configFile)
		if err != nil {
			return nil, fmt.Errorf("error decoding HCL file %s: %w", filePath, err)
		}

		// Merge decoded content
		if len(configFile.Jobs) > 0 {
			mergedConfig.Jobs = append(mergedConfig.Jobs, configFile.Jobs...)
		}
		if len(configFile.Boots) > 0 {
			mergedConfig.Boots = append(mergedConfig.Boots, configFile.Boots...)
		}
		if len(configFile.Probes) > 0 {
			mergedConfig.Probes = append(mergedConfig.Probes, configFile.Probes...)
		}
	}

	// Set default values after merging
	for _, job := range mergedConfig.Jobs {
		if job.TimestampFormat == "" {
			job.TimestampFormat = "RFC3339"
		}
		// Other defaults can be set here as needed
	}

	return mergedConfig, nil
}

// Helper function to walk directory for .hcl files (alternative to ReadDir for nested structures if needed later)
// For now, ReadDir is sufficient as we are only looking at the top-level of dirPath.
func _filepathWalkDir(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".hcl" {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
