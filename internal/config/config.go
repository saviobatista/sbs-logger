package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds the application configuration
type Config struct {
	Sources    []string
	OutputDir  string
}

// Load loads the configuration from environment variables and .env file
func Load() (*Config, error) {
	// Try to load .env file, but don't fail if it doesn't exist
	_ = godotenv.Load()

	sources := os.Getenv("SOURCES")
	if sources == "" {
		return nil, fmt.Errorf("SOURCES environment variable is required")
	}

	outputDir := os.Getenv("OUTPUT_DIR")
	if outputDir == "" {
		outputDir = "./logs" // Default output directory
	}

	return &Config{
		Sources:   strings.Split(sources, ","),
		OutputDir: outputDir,
	}, nil
} 