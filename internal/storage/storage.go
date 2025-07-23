package storage

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Storage handles writing SBS messages to files
type Storage struct {
	outputDir string
	file      *os.File
	mu        sync.Mutex
	stopChan  chan struct{}
	wg        sync.WaitGroup
}

// New creates a new Storage instance
func New(outputDir string) *Storage {
	return &Storage{
		outputDir: outputDir,
		stopChan:  make(chan struct{}),
	}
}

// Start initializes the storage system and starts the rotation timer
func (s *Storage) Start() error {
	if err := s.rotateFile(); err != nil {
		return err
	}

	s.wg.Add(1)
	go s.rotationTimer()

	return nil
}

// Stop closes the current file and stops the rotation timer
func (s *Storage) Stop() error {
	close(s.stopChan)
	s.wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

// WriteMessage writes a message to the current log file
func (s *Storage) WriteMessage(message []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file == nil {
		if err := s.rotateFile(); err != nil {
			return err
		}
	}

	// Check if message already ends with newline
	if len(message) > 0 && message[len(message)-1] == '\n' {
		_, err := s.file.Write(message)
		return err
	}

	_, err := s.file.Write(append(message, '\n'))
	return err
}

// rotationTimer handles daily rotation at midnight UTC
func (s *Storage) rotationTimer() {
	defer s.wg.Done()

	for {
		now := time.Now().UTC()
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		waitTime := nextMidnight.Sub(now)

		select {
		case <-time.After(waitTime):
			if err := s.rotateAndCompress(); err != nil {
				fmt.Printf("Error during rotation: %v\n", err)
			}
		case <-s.stopChan:
			return
		}
	}
}

// rotateAndCompress rotates the current file and compresses the previous day's file
func (s *Storage) rotateAndCompress() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close current file
	if s.file != nil {
		s.file.Close()
		s.file = nil
	}

	// Compress yesterday's file
	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	yesterdayFile := filepath.Join(s.outputDir, fmt.Sprintf("sbs_%s.log", yesterday.Format("2006-01-02")))

	if _, err := os.Stat(yesterdayFile); err == nil {
		if err := s.compressFile(yesterdayFile); err != nil {
			return fmt.Errorf("failed to compress file: %w", err)
		}
	}

	// Create new file
	return s.rotateFile()
}

// compressFile compresses a file using gzip
func (s *Storage) compressFile(filepath string) error {
	// Open the source file
	source, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer source.Close()

	// Create the compressed file
	compressedFile := filepath + ".gz"
	target, err := os.Create(compressedFile)
	if err != nil {
		return err
	}
	defer target.Close()

	// Create gzip writer
	gzipWriter := gzip.NewWriter(target)
	defer gzipWriter.Close()

	// Copy the contents
	if _, err := gzipWriter.Write([]byte{}); err != nil {
		return err
	}

	// Close the gzip writer to ensure all data is written
	if err := gzipWriter.Close(); err != nil {
		return err
	}

	// Remove the original file
	return os.Remove(filepath)
}

// rotateFile creates a new log file with today's date
func (s *Storage) rotateFile() error {
	timestamp := time.Now().UTC().Format("2006-01-02")
	filename := filepath.Join(s.outputDir, fmt.Sprintf("sbs_%s.log", timestamp))

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	s.file = file
	return nil
}
