package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"mittinit/config"
	"os"
	"os/exec" // Added for TestMain
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Helper to create a dummy logger
func newTestLogger(t *testing.T) *log.Logger {
	// For verbose test output, replace io.Discard with a writer that uses t.Logf
	// writer := &testLogWriter{t: t}
	// return log.New(writer, "", 0)
	return log.New(io.Discard, "", 0)
}

// testLogWriter adapts t.Logf to io.Writer interface for logging
type testLogWriter struct {
	t *testing.T
}

func (tlw *testLogWriter) Write(p []byte) (n int, err error) {
	tlw.t.Logf("%s", p)
	return len(p), nil
}


// Helper to create a basic JobManager for testing
func newTestJobManager(t *testing.T, jobConfig *config.Job) *JobManager {
	if jobConfig == nil {
		jobConfig = &config.Job{
			Name:    "test_job",
			Command: "echo", // Using "echo" might be problematic on Windows if not in PATH
			Args:    []string{"hello"},
		}
	}
	return NewJobManager(jobConfig, newTestLogger(t))
}

// TestMain is used to compile the output_generator helper if it's not already built.
// This helps ensure tests that depend on it can run without manual pre-steps.
func TestMain(m *testing.M) {
	// If in a special test mode (child process for re-execution tests),
	// skip the build logic and just run the tests. The specific child test
	// logic is handled within the test functions themselves (e.g., TestJobManager_Restart).
	if os.Getenv("GO_TEST_MODE_RETRY") == "1" ||
		os.Getenv("GO_TEST_MODE_FAIL_ALWAYS") == "1" ||
		os.Getenv("GO_TEST_MODE_RESTART") == "1" {
		// When the test binary is re-executed, m.Run() will effectively filter
		// for the specific function that matches the -test.run flag, which won't
		// be TestMain itself. The child logic is within the actual test functions.
		os.Exit(m.Run())
		return
	}

	// Determine path relative to the package directory (cmd)
	// Assumes `go test` is run from the `cmd` directory or project root with `./cmd/...`
	wd, _ := os.Getwd() // Get current working directory

	// Try to determine project root. This is a bit heuristic.
	// If "go.mod" is in wd, wd is project root.
	// If "cmd" is a subdir of wd, wd is project root.
	// If wd ends with "cmd", then parent is project root.
	projectRoot := wd
	if strings.HasSuffix(strings.ToLower(wd), "cmd") {
		projectRoot = filepath.Dir(wd)
	} else if _, err := os.Stat(filepath.Join(wd, "go.mod")); err != nil {
		// If go.mod not in current wd, try to find it in parent if wd is like /tmp/go-build...
		// This is fragile. Best to run tests from project root or module root.
		// For simplicity, assume projectRoot is correctly determined or CWD is usable.
	}


	helperSource := filepath.Join(projectRoot, "test_helpers", "output_generator", "main.go")
	helperBinary := filepath.Join(projectRoot, "test_helpers", "output_generator_bin")

	// Check if helper binary exists
	_, err := os.Stat(helperBinary)
	if os.IsNotExist(err) {
		fmt.Printf("Test helper binary not found. Building: %s -> %s\n", helperSource, helperBinary)
		// Ensure the target directory exists
		if err := os.MkdirAll(filepath.Dir(helperBinary), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create directory for test helper: %v\n", err)
			os.Exit(1)
		}

		buildCmd := exec.Command("go", "build", "-o", helperBinary, helperSource)
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		// buildCmd.Dir = projectRoot // Run 'go build' from project root
		errBuild := buildCmd.Run()
		if errBuild != nil {
			fmt.Fprintf(os.Stderr, "Failed to build test_helpers/output_generator_bin: %v\n", errBuild)
			// os.Exit(1) // Exit if helper is critical for many tests
		} else {
			fmt.Println("Test helper built successfully.")
		}
	}

	exitCode := m.Run()
	os.Exit(exitCode)
}


func TestNewJobManager(t *testing.T) {
	cfg := &config.Job{Name: "my_job"}
	jm := NewJobManager(cfg, newTestLogger(t))
	if jm == nil {
		t.Fatal("NewJobManager returned nil")
	}
	if jm.JobConfig.Name != "my_job" {
		t.Errorf("Expected job name 'my_job', got '%s'", jm.JobConfig.Name)
	}
	if jm.logger == nil {
		t.Error("JobManager logger is nil")
	}
}

func TestJobManager_GetName(t *testing.T) {
	jm := newTestJobManager(t, &config.Job{Name: "specific_name"})
	if name := jm.GetName(); name != "specific_name" {
		t.Errorf("Expected GetName to return 'specific_name', got '%s'", name)
	}
}

func TestJobManager_Start_Stop_SuccessfulCommand(t *testing.T) {
	tmpDir := t.TempDir()
	stdoutPath := filepath.Join(tmpDir, "stdout.log")

	jobConfig := &config.Job{
		Name:    "successful_echo",
		Command: "echo",
		Args:    []string{"hello test"},
		Stdout:  stdoutPath,
		MaxAttempts: 1,
	}
	jm := newTestJobManager(t, jobConfig)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var overallWg sync.WaitGroup
	overallWg.Add(1)
	jm.Start(ctx, &overallWg)

	// Give the job a moment to start and write output
	time.Sleep(200 * time.Millisecond)

	jm.Stop()      // Request stop
	overallWg.Wait() // Wait for the job's main goroutine (and thus the command) to finish
	jm.Wait()      // Wait for the JobManager's own goroutine to finish processing

	// Verify output
	content, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("Failed to read stdout log: %v", err)
	}
	expectedOutput := "hello test"
	// Output might contain timestamps, so check for substring
	if !strings.Contains(string(content), expectedOutput) {
		t.Errorf("Expected stdout log to contain '%s', got '%s'", expectedOutput, string(content))
	}

	if jm.running {
		t.Error("JobManager still marked as running after Stop and Wait")
	}
}

func TestJobManager_Start_FailingCommand_WithRetries(t *testing.T) {
	if os.Getenv("GO_TEST_MODE_RETRY") == "1" { // In child process
		filePath := os.Getenv("ATTEMPT_COUNTER_FILE")
		failUntilTarget := 0 // How many executions until success
		fmt.Sscan(os.Getenv("FAIL_UNTIL_ATTEMPT"), &failUntilTarget)

		currentExecution := 1 // Default to 1st execution
		content, err := os.ReadFile(filePath)
		if err == nil { // If file exists, read current execution count
			fmt.Sscan(string(content), &currentExecution)
		}

		// Output for debugging in test logs
		fmt.Fprintf(os.Stdout, "Child GO_TEST_MODE_RETRY: execution %d, target success on execution #%d, pid %d\n", currentExecution, failUntilTarget, os.Getpid())

		exitCode := 1
		if currentExecution >= failUntilTarget { // Succeed if current execution is the target success attempt
			fmt.Fprintf(os.Stdout, "Child GO_TEST_MODE_RETRY: Succeeded on execution %d\n", currentExecution)
			exitCode = 0
		} else {
			fmt.Fprintf(os.Stderr, "Child GO_TEST_MODE_RETRY: Failed on execution %d, will retry\n", currentExecution)
		}

		// Write the *next* execution number to the file, regardless of success/fail this time
		// This ensures the counter increments for the *next time this child logic is run*.
		if err := os.WriteFile(filePath, []byte(fmt.Sprintf("%d", currentExecution+1)), 0644); err != nil {
			// If we can't write the counter, the test might get stuck. Log and potentially exit differently.
			fmt.Fprintf(os.Stderr, "Child GO_TEST_MODE_RETRY: Error writing attempt counter file %s: %v\n", filePath, err)
			// os.Exit(255) // Special exit code if counter fails
		}
		os.Exit(exitCode)
		return
	}

	// In parent test process
	t.Parallel()

	tmpDir := t.TempDir()
	stdoutPath := filepath.Join(tmpDir, "retry_stdout.log")

	maxAttempts := 3

	jobConfig := &config.Job{
		Name:        "retry_job",
		Command:     os.Args[0], // Re-run the test binary
		Args:        []string{},
		MaxAttempts: maxAttempts,
		Stdout:      stdoutPath,
		CanFail:     false,
	}

	// This logger will write to t.Logf, making it visible with `go test -v`
	logger := log.New(&testLogWriter{t: t}, fmt.Sprintf("[%s] ", jobConfig.Name), 0)
	jm := NewJobManager(jobConfig, logger)

	// The Start method will set these env vars for each attempt.
	// We need to ensure the test's child process logic correctly uses them.
	// The JobManager's Start loop itself handles attempts and does not rely on child changing env.
	// The child process needs to know which attempt it is. Let's pass it via an arg or specific env.
	// Simpler: the child always fails N-1 times then succeeds. Parent's MaxAttempts handles it.

	// For this test, the child process will be started multiple times.
	// The parent (this test) will control the environment for each start.
	// We need a way for the child to know "how many times it's supposed to fail before succeeding".
	// Let's modify the child logic: it will succeed on the 3rd call if parent sets MaxAttempts=3.
	// The parent doesn't set FAIL_COUNT. The child uses its own counter or relies on MaxAttempts.
	// This is getting complicated. Let's simplify the child:
	// Child: if env "SUCCEED_THIS_ATTEMPT" is "true", exit 0, else exit 1.
	// Parent: in jm.Start, before cmd.Start(), set this env var based on currentAttempt.
	// This requires modifying JobManager.Start which is not ideal for a test.

	// Alternative: The child process (os.Args[0] with GO_TEST_MODE_RETRY=1)
	// writes its attempt number to a file. The test checks this file.
	// Or, the child determines success based on an external signal/file.
	// Let's stick to the original simple child logic: it fails a fixed number of times, then succeeds.
	// The parent's `MaxAttempts` should align with this.
	// If child fails twice then succeeds (total 3 runs), MaxAttempts should be 3.

	// The child part:
	// os.Getenv("GO_TEST_MODE_RETRY") == "1"
	//   fail_counter_file := "fail_count.txt"
	//   count := read from fail_counter_file (default 0)
	//   if count < 2 { print "failing"; write count+1 to file; exit 1 }
	//   else { print "succeeding"; delete file; exit 0 }
	// This stateful file-based approach is robust for retries.

	attemptCounterFile := filepath.Join(tmpDir, "attempt.count")
	defer os.Remove(attemptCounterFile) // Clean up

	// Adjust jobConfig.Args or Env for the child to find this file
	jobConfig.Env = append(os.Environ(),
		"GO_TEST_MODE_RETRY=1",
		"ATTEMPT_COUNTER_FILE="+attemptCounterFile,
		"FAIL_UNTIL_ATTEMPT=3", // Child should succeed on its 3rd execution.
	)


	// Child logic for GO_TEST_MODE_RETRY=1 (conceptual, needs to be in output_generator or similar if not self-test)
	// For self-test:
	if os.Getenv("GO_TEST_MODE_RETRY") == "1" { // This block is actually unreachable here, it's for the child
		filePath := os.Getenv("ATTEMPT_COUNTER_FILE")
		failUntil := 0
		fmt.Sscan(os.Getenv("FAIL_UNTIL_ATTEMPT"), &failUntil)

		currentAttempt := 1
		content, err := os.ReadFile(filePath)
		if err == nil {
			fmt.Sscan(string(content), &currentAttempt)
			currentAttempt++
		}
		os.WriteFile(filePath, []byte(fmt.Sprintf("%d", currentAttempt)), 0644)

		fmt.Fprintf(os.Stdout, "Child: execution %d, fail_until %d, pid %d\n", currentAttempt, failUntil, os.Getpid())
		if currentAttempt >= failUntil {
			fmt.Fprintf(os.Stdout, "Child: Succeeded on attempt %d\n", currentAttempt)
			os.Exit(0)
		} else {
			fmt.Fprintf(os.Stderr, "Child: Failed on attempt %d, will retry\n", currentAttempt)
			os.Exit(1)
		}
		return
	}
	// End of conceptual child logic for GO_TEST_MODE_RETRY=1


	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second) // Increased timeout
	defer cancel()

	var overallWg sync.WaitGroup
	overallWg.Add(1)
	jm.Start(ctx, &overallWg)
	overallWg.Wait()
	jm.Wait()


	if jm.currentAttempts != maxAttempts { // Expects it to take all attempts to succeed
		t.Errorf("Expected %d attempts, got %d", maxAttempts, jm.currentAttempts)
	}

	contentBytes, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("Failed to read stdout: %v. Log was:\n%s", err, string(contentBytes))
	}
	stdoutContent := string(contentBytes)
	expectedSuccessMessage := fmt.Sprintf("Child GO_TEST_MODE_RETRY: Succeeded on execution %d", maxAttempts)
	if !strings.Contains(stdoutContent, expectedSuccessMessage) {
		t.Errorf("Expected successful attempt log message containing '%s', got: %s", expectedSuccessMessage, stdoutContent)
	}
	if jm.running {
		t.Errorf("Job should not be running after successful completion.")
	}
}


func TestJobManager_Start_MaxAttemptsExceeded(t *testing.T) {
	if os.Getenv("GO_TEST_MODE_FAIL_ALWAYS") == "1" {
		fmt.Fprintf(os.Stderr, "Failing intentionally, pid %d\n", os.Getpid())
		os.Exit(1)
		return
	}
	t.Parallel()

	jobConfig := &config.Job{
		Name:        "fail_always_job",
		Command:     os.Args[0],
		Args:        []string{},
		MaxAttempts: 2,
		CanFail:     true,
	}
	// logger := log.New(&testLogWriter{t: t}, fmt.Sprintf("[%s] ", jobConfig.Name), 0)
	logger := newTestLogger(t)
	jm := NewJobManager(jobConfig, logger)
	jm.JobConfig.Env = append(os.Environ(), "GO_TEST_MODE_FAIL_ALWAYS=1")


	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // Increased timeout
	defer cancel()

	var overallWg sync.WaitGroup
	overallWg.Add(1)
	jm.Start(ctx, &overallWg)
	overallWg.Wait()
	jm.Wait()

	if jm.currentAttempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", jm.currentAttempts)
	}
	if jm.running {
		t.Error("Job should not be running after max attempts exceeded")
	}
}


func TestJobManager_OutputRedirection(t *testing.T) {
	projectRoot := ".." // Assuming tests run from cmd/
	helperBin := filepath.Join(projectRoot, "test_helpers", "output_generator_bin")

	if _, err := os.Stat(helperBin); os.IsNotExist(err) {
		t.Fatalf("Helper binary not found at %s. Run TestMain or build manually.", helperBin)
	}

	tmpDir := t.TempDir()
	stdoutPath := filepath.Join(tmpDir, "output_stdout.log")
	stderrPath := filepath.Join(tmpDir, "output_stderr.log")
	stdouterrPath := filepath.Join(tmpDir, "output_stdouterr.log")

	tests := []struct {
		name          string
		jobConfig     *config.Job
		expectedStdout string
		expectedStderr string
		verifyPaths   []string
	}{
		{
			name: "Separate stdout and stderr files",
			jobConfig: &config.Job{
				Name:    "sep_files",
				Command: helperBin,
				Args:    []string{"-lines=1", "-length=5", "-stderr-lines=1", "-stderr-length=6"},
				Stdout:  stdoutPath,
				Stderr:  stderrPath,
				MaxAttempts: 1,
			},
			expectedStdout: "aaaaa",
			expectedStderr: "eeeeee",
			verifyPaths:   []string{stdoutPath, stderrPath},
		},
		{
			name: "Stdout and stderr to same file",
			jobConfig: &config.Job{
				Name:    "same_file",
				Command: helperBin,
				Args:    []string{"-lines=1", "-length=3", "-stderr-lines=1", "-stderr-length=4"},
				Stdout:  stdouterrPath,
				Stderr:  stdouterrPath,
				MaxAttempts: 1,
			},
			expectedStdout: "aaa",
			expectedStderr: "eeee", // Both should be in the file
			verifyPaths:   []string{stdouterrPath},
		},
		{
			name: "Stdout to os.Stdout, Stderr to os.Stderr",
			jobConfig: &config.Job{
				Name:    "os_default",
				Command: "echo",
				Args:    []string{"os_out_test"},
				MaxAttempts: 1,
			},
			expectedStdout: "",
			expectedStderr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager(t, tt.jobConfig)
			// logger := log.New(&testLogWriter{t:t}, "[OutputRedir] ", 0)
			// jm.logger = logger // So we can see job manager logs

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var wg sync.WaitGroup
			wg.Add(1)
			jm.Start(ctx, &wg)
			wg.Wait()
			jm.Wait()

			jobNamePrefix := fmt.Sprintf("[%s]", tt.jobConfig.Name)

			if tt.jobConfig.Stdout != "" { // If we expect stdout to a file
				content, err := os.ReadFile(tt.jobConfig.Stdout)
				if err != nil {
					t.Fatalf("Failed to read stdout log file %s: %v", tt.jobConfig.Stdout, err)
				}
				sContent := string(content)

				// Construct expected line with job name prefix
				expectedLine := fmt.Sprintf("%s %s", jobNamePrefix, tt.expectedStdout)
				if tt.expectedStdout != "" && !strings.Contains(sContent, expectedLine) {
					t.Errorf("Log file %s: expected to contain stdout '%s', got '%s'", tt.jobConfig.Stdout, expectedLine, sContent)
				}

				// If stdout and stderr go to the same file, also check for stderr content
				if tt.jobConfig.Stdout == tt.jobConfig.Stderr && tt.expectedStderr != "" {
					expectedErrLine := fmt.Sprintf("%s %s", jobNamePrefix, tt.expectedStderr)
					if !strings.Contains(sContent, expectedErrLine) {
						t.Errorf("Log file %s (shared): expected to also contain stderr '%s', got '%s'", tt.jobConfig.Stdout, expectedErrLine, sContent)
					}
				}
			}

			if tt.jobConfig.Stderr != "" && tt.jobConfig.Stdout != tt.jobConfig.Stderr { // If stderr is a separate file
				content, err := os.ReadFile(tt.jobConfig.Stderr)
				if err != nil {
					t.Fatalf("Failed to read stderr log file %s: %v", tt.jobConfig.Stderr, err)
				}
				sContent := string(content)
				expectedErrLine := fmt.Sprintf("%s %s", jobNamePrefix, tt.expectedStderr)
				if tt.expectedStderr != "" && !strings.Contains(sContent, expectedErrLine) {
					t.Errorf("Log file %s: expected to contain stderr '%s', got '%s'", tt.jobConfig.Stderr, expectedErrLine, sContent)
				}
			}
		})
	}
}

func TestJobManager_Timestamping(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "ts.log")

	jobConfig := &config.Job{
		Name:             "ts_job",
		Command:          "echo",
		Args:             []string{"timestamp test"},
		Stdout:           logPath,
		EnableTimestamps: true,
		TimestampFormat:  "RFC3339",
		MaxAttempts: 1,
	}
	jm := newTestJobManager(t, jobConfig)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	jm.Start(ctx, &wg)
	wg.Wait()
	jm.Wait()

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log: %v", err)
	}

	s := strings.TrimSpace(string(content)) // Trim trailing newline
	parts := strings.SplitN(s, " ", 3)
	if len(parts) < 3 {
		t.Fatalf("Log format incorrect, expected at least 3 parts (timestamp, [job_name], message), got %d: '%s'", len(parts), s)
	}
	// Try parsing the first part as RFC3339 timestamp
	// Example: 2024-01-11T15:04:05Z
	_, err = time.Parse(time.RFC3339, parts[0])
	if err != nil {
		// It might be RFC3339Nano due to default settings if job.TimestampFormat is not hit.
		// Let's check the config.LoadConfig defaults again. It sets RFC3339.
		// The actual output format also depends on the time.Now().Format() behavior.
		// Let's try RFC3339Nano as a fallback for robustness if the default changes.
		_, errNano := time.Parse(time.RFC3339Nano, parts[0])
		if errNano != nil {
			t.Errorf("Expected RFC3339 or RFC3339Nano timestamp, got parse error '%v' (RFC3339) and '%v' (RFC3339Nano) on '%s'", err, errNano, parts[0])
		}
	}

	expectedTag := fmt.Sprintf("[%s]", jobConfig.Name)
	if strings.TrimSpace(parts[1]) != expectedTag {
		t.Errorf("Expected job name tag '%s', got '%s'", expectedTag, parts[1])
	}
	if !strings.Contains(parts[2], "timestamp test") {
		t.Errorf("Expected log message 'timestamp test', got '%s'", parts[2])
	}
}


func TestJobManager_NopWriteCloser(t *testing.T) {
	nwc := NopWriteCloser{Writer: io.Discard}
	err := nwc.Close()
	if err != nil {
		t.Errorf("NopWriteCloser.Close() returned error: %v", err)
	}
	// Test writing - just ensure no panic
	_, err = nwc.Write([]byte("test"))
	if err != nil {
		t.Errorf("NopWriteCloser.Write() returned error: %v", err)
	}

}

func TestJobManager_CloseLogFiles(t *testing.T) {
	tmpDir := t.TempDir()
	stdoutPath := filepath.Join(tmpDir, "closetest_stdout.log")
	stderrPath := filepath.Join(tmpDir, "closetest_stderr.log")

	// Create dummy files
	fOut, _ := os.Create(stdoutPath)
	fErr, _ := os.Create(stderrPath)

	jm := newTestJobManager(t, &config.Job{
		Name:   "close_test",
		Stdout: stdoutPath,
		Stderr: stderrPath,
	})
	jm.stdout = fOut // Assign the opened files
	jm.stderr = fErr

	jm.closeLogFiles() // This should close fOut and fErr

	// Try to close again, should result in os.ErrClosed or similar
	errOut := fOut.Close()
	errErr := fErr.Close()

	if errOut == nil {
		t.Error("Expected error when closing already closed stdout, got nil")
	} else if !strings.Contains(errOut.Error(), os.ErrClosed.Error()) && !strings.Contains(errOut.Error(), "file already closed") { // Cross-platform check
		t.Errorf("Expected os.ErrClosed for stdout, got %v", errOut)
	}

	if errErr == nil {
		t.Error("Expected error when closing already closed stderr, got nil")
	} else if !strings.Contains(errErr.Error(), os.ErrClosed.Error()) && !strings.Contains(errErr.Error(), "file already closed") {
		t.Errorf("Expected os.ErrClosed for stderr, got %v", errErr)
	}

	// Test with stdout = stderr
	stdouterrPath := filepath.Join(tmpDir, "closetest_stdouterr.log")
	fBoth, _ := os.Create(stdouterrPath)
	jmBoth := newTestJobManager(t, &config.Job{
		Name:   "close_test_both",
		Stdout: stdouterrPath,
		Stderr: stdouterrPath,
	})
	jmBoth.stdout = fBoth // Assign the same file descriptor to both
	jmBoth.stderr = fBoth

	jmBoth.closeLogFiles() // Should close fBoth (once effectively)
	errBoth := fBoth.Close()
	if errBoth == nil {
		t.Error("Expected error when closing already closed stdouterr, got nil")
	} else if !strings.Contains(errBoth.Error(), os.ErrClosed.Error()) && !strings.Contains(errBoth.Error(), "file already closed") {
		t.Errorf("Expected os.ErrClosed for stdouterr, got %v", errBoth)
	}
}


func TestJobManager_SendSignal(t *testing.T) {
	jobConfig := &config.Job{
		Name:    "signal_job",
		Command: "sleep",
		Args:    []string{"5"}, // Sleep for 5 seconds
		MaxAttempts: 1,
		CanFail: true, // Allow the job to "fail" when terminated by a signal for this test
	}
	jm := newTestJobManager(t, jobConfig)
	logger := log.New(&testLogWriter{t:t}, "[SignalTest] ", 0)
	jm.logger = logger // Use test logger for job manager's own logs

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Logf("TestJobManager_SendSignal: Starting job %s", jm.JobConfig.Name)
	var overallWg sync.WaitGroup
	overallWg.Add(1)
	jm.Start(ctx, &overallWg)

	t.Logf("TestJobManager_SendSignal: Waiting for job to be running and PID...")
	var pid int
	success := false
	for i := 0; i < 100; i++ { // Try for up to 1 second
		jm.mutex.Lock()
		if jm.Cmd != nil && jm.Cmd.Process != nil && jm.running {
			pid = jm.Cmd.Process.Pid
			success = true
		}
		jm.mutex.Unlock()
		if success {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !success || pid == 0 {
		t.Fatalf("TestJobManager_SendSignal: Job did not start, PID not found (%d), or not marked as running in time. Success: %v", pid, success)
	}
	t.Logf("TestJobManager_SendSignal: Job started. PID: %d. Sending SIGTERM.", pid)

	err := jm.SendSignal(syscall.SIGTERM)
	if err != nil {
		t.Fatalf("TestJobManager_SendSignal: SendSignal returned error: %v", err)
	}
	t.Logf("TestJobManager_SendSignal: SIGTERM sent. Waiting on overallWg.")

	overallWg.Wait()
	t.Logf("TestJobManager_SendSignal: overallWg complete. Waiting on jm.Wait().")
	jm.Wait()
	t.Logf("TestJobManager_SendSignal: jm.Wait() complete.")

	jm.mutex.Lock()
	finalRunningStatus := jm.running
	jm.mutex.Unlock()

	if finalRunningStatus {
		t.Error("Job is still marked as running after SIGTERM and Wait")
	}
	// Check if the process actually terminated. Cmd.Wait() in the job's goroutine
	// would have returned an error if the process exited due to SIGTERM.
	// This is implicitly tested by overallWg.Wait() completing without timeout.
}


func TestJobManager_Restart(t *testing.T) {
	// Child process logic for the command being run
	if os.Getenv("GO_TEST_MODE_RESTART") == "1" {
		pid := os.Getpid()
		fmt.Printf("PID: %d, ARGS: %v, PWD: %s\n", pid, os.Args, os.Getenv("PWD")) // Log PID and args

		// Write PID to a file named by an env var to signal "I'm running"
		pidFile := os.Getenv("PID_FILE")
		if pidFile != "" {
			os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", pid)), 0644)
		}

		// Stay alive until a signal file appears or timeout
		signalFile := os.Getenv("STOP_SIGNAL_FILE")
		if signalFile == "" { signalFile = "stop.signal" } // Default, relative to CWD

		for i := 0; i < 40; i++ { // Max 4 seconds
			if _, err := os.Stat(signalFile); err == nil {
				fmt.Printf("PID: %d, Stop signal file '%s' found, exiting.\n", pid, signalFile)
				os.Remove(signalFile) // Consume the signal
				os.Exit(0)
			}
			time.Sleep(100 * time.Millisecond)
		}
		fmt.Printf("PID: %d, Timed out waiting for stop signal, exiting with error.\n", pid)
		os.Exit(1) // Exit with error if not signaled
		return
	}
	// Parent test process
	t.Parallel()

	tmpDir := t.TempDir()
	stopSignalFile := filepath.Join(tmpDir, "stop.signal")
	pidFile1 := filepath.Join(tmpDir, "pid1.txt")
	pidFile2 := filepath.Join(tmpDir, "pid2.txt")
	stdoutLog := filepath.Join(tmpDir, "restart_out.log")


	jobConfig := &config.Job{
		Name:    "restart_job",
		Command: os.Args[0], // Test binary itself
		Args:    []string{}, // Args for the child process if needed
		MaxAttempts: 1,      // Important: Restart logic resets currentAttempts
		WorkingDirectory: tmpDir, // So child can use relative paths for signal files if needed
		Stdout: stdoutLog,
		EnableTimestamps: true,
	}
	// logger := log.New(&testLogWriter{t: t}, fmt.Sprintf("[%s_Test] ", jobConfig.Name), 0)
	logger := newTestLogger(t)
	jm := NewJobManager(jobConfig, logger)
	// Common Env for all child runs
	jm.JobConfig.Env = append(os.Environ(), "GO_TEST_MODE_RESTART=1", "STOP_SIGNAL_FILE="+stopSignalFile)


	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second) // Generous timeout for the whole test
	defer cancel()

	// --- First run ---
	jm.JobConfig.Env = append(jm.JobConfig.Env, "PID_FILE="+pidFile1) // Specific for first run

	var firstRunWg sync.WaitGroup
	firstRunWg.Add(1)
	jm.Start(ctx, &firstRunWg)

	// Wait for PID file to appear
	var readPid1 int
	for i:=0; i< 50; i++ { // wait up to 5s
		time.Sleep(100 * time.Millisecond)
		jm.mutex.Lock()
		pOpen := jm.running
		jm.mutex.Unlock()
		if !pOpen && i > 10 { // if not running after 1s, probably failed to start
			break
		}
		pidBytes, err := os.ReadFile(pidFile1)
		if err == nil {
			fmt.Sscan(string(pidBytes), &readPid1)
			if readPid1 != 0 { break }
		}
	}
	if readPid1 == 0 {
		t.Fatal("Job 1 (first run) did not write PID file or failed to start.")
	}
	t.Logf("First run started, PID: %d", readPid1)


	// Signal the first instance to stop by creating the stopSignalFile
	if err := os.WriteFile(stopSignalFile, []byte("stop"), 0644); err != nil {
		t.Fatalf("Failed to write stop signal file: %v", err)
	}
	firstRunWg.Wait() // Wait for the first instance's JobManager goroutine to complete Start()
	jm.Wait()         // Wait for the JobManager's internal goroutine (jm.jobWg) for the first run.
	t.Logf("First run stopped.")
	os.Remove(pidFile1) // Clean up pid file

	// --- Restart ---
	// Update Env for the second run (e.g. different PID_FILE)
	// Remove old PID_FILE env var if it was added directly to jm.JobConfig.Env
	var newEnv []string
	for _, e := range jm.JobConfig.Env {
		if !strings.HasPrefix(e, "PID_FILE=") {
			newEnv = append(newEnv, e)
		}
	}
	jm.JobConfig.Env = append(newEnv, "PID_FILE="+pidFile2)


	// Restart does not use overallWg in its current implementation
	jm.Restart(ctx, nil)
	t.Logf("Restart initiated.")

	// Wait for PID file for the second instance
	var readPid2 int
	for i:=0; i< 50; i++ {
		time.Sleep(100 * time.Millisecond)
		jm.mutex.Lock()
		pOpen := jm.running
		jm.mutex.Unlock()
		if !pOpen && i > 10 { break }

		pidBytes, err := os.ReadFile(pidFile2)
		if err == nil {
			fmt.Sscan(string(pidBytes), &readPid2)
			if readPid2 != 0 { break }
		}
	}
	if readPid2 == 0 {
		t.Fatal("Job 2 (restarted) did not write PID file or failed to start.")
	}
	t.Logf("Second run (restarted) started, PID: %d", readPid2)

	if readPid1 == readPid2 {
		t.Errorf("PID should be different after restart. PID1: %d, PID2: %d", readPid1, readPid2)
	}

	// Stop the second instance
	if err := os.WriteFile(stopSignalFile, []byte("stop"), 0644); err != nil {
		t.Fatalf("Failed to write stop signal file for second run: %v", err)
	}
	// jm.Stop() // This would also work, Stop sends SIGTERM via cancelFunc
	jm.Wait() // Wait for the JobManager's internal goroutine for the second run.
	t.Logf("Second run stopped.")
	os.Remove(pidFile2)


	// Check logs for evidence of two runs (e.g., two different PIDs)
	contentBytes, err := os.ReadFile(stdoutLog)
	if err != nil {
		t.Fatalf("Could not read stdout log: %v", err)
	}
	content := string(contentBytes)

	scanner := bufio.NewScanner(strings.NewReader(content))
	seenPids := make(map[string]bool)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "PID:") {
			// Example line: "... PID: 1234 ..."
			parts := strings.Split(line, "PID:")
			if len(parts) > 1 {
				pidStr := strings.Fields(strings.TrimSpace(parts[1]))[0] // "1234," -> "1234"
				pidStr = strings.TrimRight(pidStr, ",")
				seenPids[pidStr] = true
			}
		}
	}

	if len(seenPids) < 2 {
		t.Errorf("Expected at least 2 different PIDs in log, found %d unique PIDs. Seen: %v. Log:\n%s", len(seenPids), seenPids, content)
	}
}


func TestExecuteBootJob_Successful(t *testing.T) {
	bootConfig := &config.Boot{
		Name:    "boot_echo",
		Command: "echo",
		Args:    []string{"boot job success"},
	}
	// logger := log.New(&testLogWriter{t: t}, "[BootSuccess] ", 0)
	logger := newTestLogger(t)
	err := ExecuteBootJob(bootConfig, logger, context.Background())
	if err != nil {
		t.Errorf("ExecuteBootJob failed for successful command: %v", err)
	}
}

func TestExecuteBootJob_Failing(t *testing.T) {
	bootConfig := &config.Boot{
		Name:    "boot_fail",
		Command: "sh",
		Args:    []string{"-c", "echo 'boot failing to stderr' >&2 && exit 1"},
	}
	// var logBuf strings.Builder
	// logger := log.New(&logBuf, "[BootFail] ", 0)
	logger := newTestLogger(t) // Use discard for now, enable logBuf for debugging if needed

	err := ExecuteBootJob(bootConfig, logger, context.Background())
	if err == nil {
		t.Error("ExecuteBootJob should have failed but returned nil")
	} else {
		if !strings.Contains(err.Error(), "boot_fail") || !strings.Contains(err.Error(), "failed") {
			t.Errorf("Error message does not clearly indicate failure of 'boot_fail': %v", err)
		}
		// t.Logf("ExecuteBootJob_Failing log output: %s", logBuf.String()) // If using logBuf
		// t.Logf("ExecuteBootJob_Failing error: %v", err)
	}
}

func TestExecuteBootJob_Timeout(t *testing.T) {
	bootConfig := &config.Boot{
		Name:    "boot_timeout_test", // Changed name to avoid conflict if tests run oddly
		Command: "sleep",
		Args:    []string{"2"}, // Sleeps for 2 seconds
		Timeout: "50ms",      // Very short timeout
	}
	// var logBuf strings.Builder
	// logger := log.New(&logBuf, "[BootTimeout] ", 0)
	logger := newTestLogger(t)

	startTime := time.Now()
	err := ExecuteBootJob(bootConfig, logger, context.Background())
	duration := time.Since(startTime)

	if err == nil {
		t.Fatal("ExecuteBootJob should have failed due to timeout but returned nil")
	}
	// t.Logf("ExecuteBootJob_Timeout log output: %s", logBuf.String())
	// t.Logf("ExecuteBootJob_Timeout error: %s", err)


	if !strings.Contains(err.Error(), "timed out") && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("Expected error message to contain 'timed out' or 'context deadline exceeded', got: %v", err)
	}
	// Check if it actually timed out quickly, not after the full sleep duration
	// Allow some slack for process startup, e.g. 500ms
	if duration >= 1*time.Second {
		t.Errorf("Boot job did not seem to time out quickly enough. Duration: %v, Timeout: %s", duration, bootConfig.Timeout)
	}
}

func TestExecuteBootJob_StreamingOutput(t *testing.T) {
	var logOutput strings.Builder
	logger := log.New(&logOutput, "", 0) // Capture logs directly

	stdoutMsg := "Boot job standard output line for streaming test."
	stderrMsg := "Boot job standard error line for streaming test."

	// Use a shell command to produce specific stdout and stderr
	// Ensure messages are unique enough not to clash with other log lines.
	command := fmt.Sprintf("echo '%s'; >&2 echo '%s'", stdoutMsg, stderrMsg)

	bootConfig := &config.Boot{
		Name:    "boot_stream_check", // Unique name
		Command: "sh",
		Args:    []string{"-c", command},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := ExecuteBootJob(bootConfig, logger, ctx)
	if err != nil {
		t.Fatalf("ExecuteBootJob failed: %v. Log output:\n%s", err, logOutput.String())
	}

	logs := logOutput.String()
	expectedStdoutLog := fmt.Sprintf("[%s stdout] %s", bootConfig.Name, stdoutMsg)
	if !strings.Contains(logs, expectedStdoutLog) {
		t.Errorf("Expected log to contain stdout message '%s', got:\n%s", expectedStdoutLog, logs)
	}

	expectedStderrLog := fmt.Sprintf("[%s stderr] %s", bootConfig.Name, stderrMsg)
	if !strings.Contains(logs, expectedStderrLog) {
		t.Errorf("Expected log to contain stderr message '%s', got:\n%s", expectedStderrLog, logs)
	}
}

// Note: Tests involving os.Args[0] (self-execution) like TestJobManager_Start_FailingCommand_WithRetries,
// TestJobManager_Start_MaxAttemptsExceeded, and TestJobManager_Restart require the test binary to be compiled.
// `go test` handles this automatically. The `GO_TEST_MODE_*` environment variables are crucial for the
// child process (the re-executed test binary) to know it should run in a special mode.
// `t.Parallel()` is important for these tests if they are long-running and can run concurrently with others.
// However, be cautious with `t.Parallel()` if tests modify shared global state or files in a conflicting way.
// The `TestMain` function is added to ensure the `output_generator_bin` helper is built before tests run.
// This makes the test suite more self-contained.
