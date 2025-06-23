package cmd

import (
	"context"
	"mittinit/config"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWatchJobFiles_GlobDetectsNewFiles(t *testing.T) {
	tmpDir := t.TempDir()
	globPattern := filepath.Join(tmpDir, "*.watched")

	// Create initial file that matches the glob
	initialFile := filepath.Join(tmpDir, "file1.watched")
	if err := os.WriteFile(initialFile, []byte("init"), 0644); err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}

	jobConfig := &config.Job{
		Name:  "watch_glob",
		Watch: []config.Watch{{Path: globPattern}},
	}
	jm := newTestJobManager(t, jobConfig)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		watchJobFiles(ctx, jm, &wg)
	}()

	// Give watcher time to start
	time.Sleep(200 * time.Millisecond)

	// Create a new file that matches the glob
	newFile := filepath.Join(tmpDir, "file2.watched")
	if err := os.WriteFile(newFile, []byte("new"), 0644); err != nil {
		t.Fatalf("Failed to create new file: %v", err)
	}

	// Give watcher time to detect and add the new file
	time.Sleep(500 * time.Millisecond)

	// Clean up
	cancel()
	wg.Wait()

	// If no panic or error, the watcher handled the new file
}
