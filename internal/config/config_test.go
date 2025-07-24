package config

import (
	"os"
	"testing"
)

func TestLoad_WithValidSources(t *testing.T) {
	// Set up test environment
	os.Setenv("SOURCES", "source1,source2,source3")
	os.Setenv("OUTPUT_DIR", "/test/output")
	defer func() {
		os.Unsetenv("SOURCES")
		os.Unsetenv("OUTPUT_DIR")
	}()

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if config == nil {
		t.Fatal("Load() returned nil config")
	}

	expectedSources := []string{"source1", "source2", "source3"}
	if len(config.Sources) != len(expectedSources) {
		t.Errorf("Expected %d sources, got %d", len(expectedSources), len(config.Sources))
	}

	for i, source := range expectedSources {
		if config.Sources[i] != source {
			t.Errorf("Expected source[%d] = %s, got %s", i, source, config.Sources[i])
		}
	}

	if config.OutputDir != "/test/output" {
		t.Errorf("Expected OutputDir = /test/output, got %s", config.OutputDir)
	}
}

func TestLoad_WithDefaultOutputDir(t *testing.T) {
	// Set up test environment
	os.Setenv("SOURCES", "source1")
	os.Unsetenv("OUTPUT_DIR")
	defer func() {
		os.Unsetenv("SOURCES")
	}()

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if config == nil {
		t.Fatal("Load() returned nil config")
	}

	if config.OutputDir != "./logs" {
		t.Errorf("Expected default OutputDir = ./logs, got %s", config.OutputDir)
	}
}

func TestLoad_WithMissingSources(t *testing.T) {
	// Set up test environment
	os.Unsetenv("SOURCES")
	os.Unsetenv("OUTPUT_DIR")

	config, err := Load()
	if err == nil {
		t.Fatal("Load() should have failed with missing SOURCES")
	}

	if config != nil {
		t.Fatal("Load() should have returned nil config")
	}

	expectedError := "SOURCES environment variable is required"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestLoad_WithEmptySources(t *testing.T) {
	// Set up test environment
	os.Setenv("SOURCES", "")
	os.Unsetenv("OUTPUT_DIR")
	defer func() {
		os.Unsetenv("SOURCES")
	}()

	config, err := Load()
	if err == nil {
		t.Fatal("Load() should have failed with empty SOURCES")
	}

	if config != nil {
		t.Fatal("Load() should have returned nil config")
	}

	expectedError := "SOURCES environment variable is required"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestLoad_WithSingleSource(t *testing.T) {
	// Set up test environment
	os.Setenv("SOURCES", "single-source")
	os.Unsetenv("OUTPUT_DIR")
	defer func() {
		os.Unsetenv("SOURCES")
	}()

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if config == nil {
		t.Fatal("Load() returned nil config")
	}

	if len(config.Sources) != 1 {
		t.Errorf("Expected 1 source, got %d", len(config.Sources))
	}

	if config.Sources[0] != "single-source" {
		t.Errorf("Expected source = single-source, got %s", config.Sources[0])
	}
}

func TestLoad_WithSpacesInSources(t *testing.T) {
	// Set up test environment
	os.Setenv("SOURCES", "source1, source2 , source3")
	os.Unsetenv("OUTPUT_DIR")
	defer func() {
		os.Unsetenv("SOURCES")
	}()

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if config == nil {
		t.Fatal("Load() returned nil config")
	}

	expectedSources := []string{"source1", " source2 ", " source3"}
	if len(config.Sources) != len(expectedSources) {
		t.Errorf("Expected %d sources, got %d", len(expectedSources), len(config.Sources))
	}

	for i, source := range expectedSources {
		if config.Sources[i] != source {
			t.Errorf("Expected source[%d] = '%s', got '%s'", i, source, config.Sources[i])
		}
	}
} 