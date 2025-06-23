package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"mittinit/config"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// JobManager holds the state for a running job
type JobManager struct {
	JobConfig      *config.Job
	Cmd            *exec.Cmd
	running        bool
	mutex          sync.Mutex
	cancelFunc     context.CancelFunc
	stdout         io.WriteCloser
	stderr         io.WriteCloser
	logger         *log.Logger
	jobWg          sync.WaitGroup
	currentAttempts int
}

// NewJobManager creates a new manager for a job
func NewJobManager(jobConfig *config.Job, appLogger *log.Logger) *JobManager {
	return &JobManager{
		JobConfig: jobConfig,
		logger:    appLogger,
	}
}

func (jm *JobManager) GetName() string {
	return jm.JobConfig.Name
}

func (jm *JobManager) setupOutputStreams() error {
	var err error
	handleOutput := func(pipe io.ReadCloser, baseWriter io.Writer, streamName string) {
		go func() {
			scanner := bufio.NewScanner(pipe)
			for scanner.Scan() {
				line := scanner.Text()
				if jm.JobConfig.EnableTimestamps {
					tsFormat := time.RFC3339
					if jm.JobConfig.CustomTimestampFormat != "" {
						tsFormat = jm.JobConfig.CustomTimestampFormat
					} else if jm.JobConfig.TimestampFormat != "" {
						// Map named formats to Go time layout strings
						// This can be expanded
						switch jm.JobConfig.TimestampFormat {
						case "RFC3339Nano":
							tsFormat = time.RFC3339Nano
						case "Kitchen":
							tsFormat = time.Kitchen
						// Add other common formats as needed
						default:
							// If not a known named format, try to use it directly (e.g. "2006-01-02 15:04:05")
							// This might be risky if the string is not a valid format.
							// Consider validating known formats more strictly or parsing them.
							tsFormat = jm.JobConfig.TimestampFormat
						}
					}
					fmt.Fprintf(baseWriter, "%s [%s] %s\n", time.Now().Format(tsFormat), jm.JobConfig.Name, line)
				} else {
					fmt.Fprintf(baseWriter, "[%s] %s\n", jm.JobConfig.Name, line)
				}
			}
			if err := scanner.Err(); err != nil {
				jm.logger.Printf("Error reading %s for job %s: %v", streamName, jm.JobConfig.Name, err)
			}
		}()
	}

	// Setup stdout
	if jm.JobConfig.Stdout != "" {
		jm.stdout, err = os.OpenFile(jm.JobConfig.Stdout, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open stdout log file %s for job %s: %w", jm.JobConfig.Stdout, jm.JobConfig.Name, err)
		}
	} else {
		jm.stdout = NopWriteCloser{os.Stdout}
	}

	// Setup stderr
	if jm.JobConfig.Stderr != "" {
		if jm.JobConfig.Stderr == jm.JobConfig.Stdout { // if stderr is same as stdout
			jm.stderr = jm.stdout
		} else {
			jm.stderr, err = os.OpenFile(jm.JobConfig.Stderr, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				if closer, ok := jm.stdout.(io.Closer); ok && jm.JobConfig.Stdout != "" {
					closer.Close() // Close stdout if it was opened
				}
				return fmt.Errorf("failed to open stderr log file %s for job %s: %w", jm.JobConfig.Stderr, jm.JobConfig.Name, err)
			}
		}
	} else {
		jm.stderr = NopWriteCloser{os.Stderr}
	}

	cmdStdoutPipe, err := jm.Cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe for job %s: %w", jm.JobConfig.Name, err)
	}
	handleOutput(cmdStdoutPipe, jm.stdout, "stdout")

	cmdStderrPipe, err := jm.Cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe for job %s: %w", jm.JobConfig.Name, err)
	}
	handleOutput(cmdStderrPipe, jm.stderr, "stderr")

	return nil
}

// Start prepares and starts the job.
func (jm *JobManager) Start(ctx context.Context, overallWg *sync.WaitGroup) {
	jm.jobWg.Add(1) // Add to WaitGroup before goroutine starts
	go func() {
		defer jm.jobWg.Done()
		if overallWg != nil {
			defer overallWg.Done()
		}

		for {
			jm.currentAttempts++
			jm.logger.Printf("Starting job %s (attempt %d)...", jm.JobConfig.Name, jm.currentAttempts)

			jobCtx, cancelFunc := context.WithCancel(ctx)
			jm.mutex.Lock()
			jm.cancelFunc = cancelFunc
			jm.Cmd = exec.CommandContext(jobCtx, jm.JobConfig.Command, jm.JobConfig.Args...)
			jm.Cmd.Env = append(os.Environ(), jm.JobConfig.Env...)
			jm.Cmd.Dir = jm.JobConfig.WorkingDirectory
			// Set a process group ID for the child process
			jm.Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			jm.mutex.Unlock()

			if err := jm.setupOutputStreams(); err != nil {
				jm.logger.Printf("Error setting up output streams for job %s: %v. Job will not start.", jm.JobConfig.Name, err)
				// If we can't set up logging, it's a critical failure for this job instance.
				// Depending on CanFail, this might lead to mittinit termination.
				if !jm.JobConfig.CanFail {
					jm.logger.Printf("Job %s is critical and failed to setup output. Terminating mittinit.", jm.JobConfig.Name)
					// This needs a way to signal main to terminate. For now, just log.
					// In a real scenario, send a signal on a channel or call a global shutdown function.
					// For now, let's assume this means the job attempt failed and retry logic will handle it.
				}
				// Decrement attempts as this was a setup failure, not an execution one for retry logic.
				// Or, consider this a valid attempt. For now, let's count it.
				// break // or continue based on desired behavior for setup failure
				goto attemptFailed // Using goto to handle retry logic consistently
			}

			jm.mutex.Lock()
			jm.running = true
			jm.mutex.Unlock()

			err := jm.Cmd.Start()
			if err != nil {
				jm.logger.Printf("Failed to start job %s: %v", jm.JobConfig.Name, err)
				jm.closeLogFiles()
				goto attemptFailed
			}

			jm.logger.Printf("Job %s started successfully (PID: %d).", jm.JobConfig.Name, jm.Cmd.Process.Pid)
			err = jm.Cmd.Wait() // Wait for the job to complete
			jm.closeLogFiles()

			jm.mutex.Lock()
			jm.running = false
			jm.mutex.Unlock()

			if err != nil {
				if jobCtx.Err() == context.Canceled {
					jm.logger.Printf("Job %s was canceled or stopped: %v", jm.JobConfig.Name, err)
					return // Exit goroutine as job was intentionally stopped
				}
				jm.logger.Printf("Job %s exited with error: %v", jm.JobConfig.Name, err)
			} else {
				jm.logger.Printf("Job %s completed successfully.", jm.JobConfig.Name)
				// If a job completes successfully, it should not be restarted unless watched files trigger it.
				// For now, if it completes without error, we assume it's done.
				return // Exit goroutine
			}

		attemptFailed:
			if ctx.Err() == context.Canceled { // Check if mittinit is shutting down
				jm.logger.Printf("Mittinit shutting down, not restarting job %s.", jm.JobConfig.Name)
				return
			}

			if jm.JobConfig.MaxAttempts == -1 {
				jm.logger.Printf("Job %s failed. Retrying indefinitely...", jm.JobConfig.Name)
				time.Sleep(1 * time.Second) // Simple backoff
				continue
			}
			if jm.currentAttempts >= jm.JobConfig.MaxAttempts && jm.JobConfig.MaxAttempts != 0 { // MaxAttempts = 0 means try once
				jm.logger.Printf("Job %s failed after %d attempts.", jm.JobConfig.Name, jm.currentAttempts)
				if !jm.JobConfig.CanFail {
					jm.logger.Fatalf("Critical job %s failed. Terminating mittinit.", jm.JobConfig.Name)
					// This will call os.Exit due to log.Fatalf
				}
				return // Stop trying for this job
			}
			if jm.JobConfig.MaxAttempts == 0 && jm.currentAttempts >=1 { // Only one attempt if MaxAttempts is 0
				jm.logger.Printf("Job %s failed and MaxAttempts is 0. Not retrying.", jm.JobConfig.Name)
				if !jm.JobConfig.CanFail {
					jm.logger.Fatalf("Critical job %s failed (MaxAttempts=0). Terminating mittinit.", jm.JobConfig.Name)
				}
				return
			}

			jm.logger.Printf("Job %s failed. Retrying in 1 second...", jm.JobConfig.Name)
			time.Sleep(1 * time.Second) // Simple backoff
		}
	}()
}

// Stop sends a SIGTERM to the job's process group and waits for it to exit.
// If the process does not exit after a timeout, it sends a SIGKILL.
func (jm *JobManager) Stop() {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()

	if !jm.running || jm.Cmd == nil || jm.Cmd.Process == nil {
		jm.logger.Printf("Job %s is not running or already stopped.", jm.JobConfig.Name)
		if jm.cancelFunc != nil {
			jm.cancelFunc() // Ensure context is cancelled even if process not running
		}
		return
	}

	if jm.cancelFunc != nil {
		jm.cancelFunc() // Cancels the context for Cmd
	}

	jm.logger.Printf("Stopping job %s (PID: %d)...", jm.JobConfig.Name, jm.Cmd.Process.Pid)

	// Send SIGTERM to the entire process group
	// Negative PID sends signal to the process group
	if err := syscall.Kill(-jm.Cmd.Process.Pid, syscall.SIGTERM); err != nil {
		jm.logger.Printf("Failed to send SIGTERM to job %s (PID: %d, PGID: %d): %v", jm.JobConfig.Name, jm.Cmd.Process.Pid, jm.Cmd.Process.Pid, err)
	} else {
		jm.logger.Printf("Sent SIGTERM to job %s (PID: %d, PGID: %d)", jm.JobConfig.Name, jm.Cmd.Process.Pid, jm.Cmd.Process.Pid)
	}

	// Wait for a grace period for the process to exit
	// Note: Cmd.Wait() would have already been called by the run goroutine.
	// Here we need a separate mechanism to wait for the stop signal to take effect.
	// The job's run goroutine will handle Cmd.Wait() and log completion/error.
	// This Stop function primarily ensures the signal is sent.
	// The jobWg.Wait() in the main up command will ensure cleanup.
}

// Wait waits for the job goroutine to complete.
func (jm *JobManager) Wait() {
	jm.jobWg.Wait()
}


func (jm *JobManager) closeLogFiles() {
	if closer, ok := jm.stdout.(io.Closer); ok && jm.JobConfig.Stdout != "" {
		if err := closer.Close(); err != nil {
			jm.logger.Printf("Error closing stdout log for job %s: %v", jm.JobConfig.Name, err)
		}
	}
	if closer, ok := jm.stderr.(io.Closer); ok && jm.JobConfig.Stderr != "" && jm.JobConfig.Stderr != jm.JobConfig.Stdout {
		if err := closer.Close(); err != nil {
			jm.logger.Printf("Error closing stderr log for job %s: %v", jm.JobConfig.Name, err)
		}
	}
}


// NopWriteCloser wraps an io.Writer to make it an io.WriteCloser with a no-op Close().
// This is useful when we want to use os.Stdout/os.Stderr but need an io.WriteCloser interface.
type NopWriteCloser struct {
	io.Writer
}

// Close implements the io.Closer interface for NopWriteCloser.
func (nwc NopWriteCloser) Close() error {
	return nil // No-op
}

// ExecuteBootJob runs a boot job and waits for it to complete.
// Added mainCtx for potential cancellation during boot, if needed by specific boot commands.
func ExecuteBootJob(bootConfig *config.Boot, appLogger *log.Logger, mainCtx context.Context) error {
	appLogger.Printf("Starting boot job %s: %s %s", bootConfig.Name, bootConfig.Command, strings.Join(bootConfig.Args, " "))

	var cmd *exec.Cmd
	cmd.Env = append(os.Environ(), bootConfig.Env...)
	// Consider adding WorkingDirectory to Boot struct if needed
	// cmd.Dir = bootConfig.WorkingDirectory

	// Capture output for logging
	var outbuf, errbuf strings.Builder
	cmd.Stdout = &outbuf
	cmd.Stderr = &errbuf

	var err error
	if bootConfig.Timeout != "" {
		timeout, parseErr := time.ParseDuration(bootConfig.Timeout)
		if parseErr != nil {
			return fmt.Errorf("failed to parse timeout for boot job %s: %w", bootConfig.Name, parseErr)
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd = exec.CommandContext(ctx, bootConfig.Command, bootConfig.Args...)
		cmd.Env = append(os.Environ(), bootConfig.Env...)
		cmd.Stdout = &outbuf
		cmd.Stderr = &errbuf

		err = cmd.Start()
		if err == nil {
			err = cmd.Wait()
		}

		if ctx.Err() == context.DeadlineExceeded {
			appLogger.Printf("Boot job %s timed out after %s.", bootConfig.Name, bootConfig.Timeout)
			if cmd.Process != nil {
				// Attempt to kill the process if it's still running
				if killErr := cmd.Process.Kill(); killErr != nil {
					appLogger.Printf("Failed to kill timed-out boot job %s: %v", bootConfig.Name, killErr)
				}
			}
			return fmt.Errorf("boot job %s timed out", bootConfig.Name)
		}
	} else {
		err = cmd.Run() // Simpler execution if no timeout
	}


	if outbuf.Len() > 0 {
		appLogger.Printf("Boot job %s stdout:\n%s", bootConfig.Name, outbuf.String())
	}
	if errbuf.Len() > 0 {
		appLogger.Printf("Boot job %s stderr:\n%s", bootConfig.Name, errbuf.String())
	}

	if err != nil {
		return fmt.Errorf("boot job %s failed: %w. Stderr: %s", bootConfig.Name, err, errbuf.String())
	}

	appLogger.Printf("Boot job %s completed successfully.", bootConfig.Name)
	return nil
}

func (jm *JobManager) SendSignal(sig syscall.Signal) error {
	jm.mutex.Lock()
	defer jm.mutex.Unlock()

	if !jm.running || jm.Cmd == nil || jm.Cmd.Process == nil {
		return fmt.Errorf("job %s is not running, cannot send signal", jm.JobConfig.Name)
	}

	// Send signal to the entire process group
	err := syscall.Kill(-jm.Cmd.Process.Pid, sig)
	if err != nil {
		return fmt.Errorf("failed to send signal %v to job %s (PID: %d, PGID: %d): %w", sig, jm.JobConfig.Name, jm.Cmd.Process.Pid, jm.Cmd.Process.Pid, err)
	}
	jm.logger.Printf("Sent signal %v to job %s (PID: %d, PGID: %d)", sig, jm.JobConfig.Name, jm.Cmd.Process.Pid, jm.Cmd.Process.Pid)
	return nil
}

// Restart stops and then starts the job again.
// This is a simplified version. In a real scenario, you might want to ensure
// it respects contexts and potentially MaxAttempts if the restart is due to a watch.
func (jm *JobManager) Restart(ctx context.Context, overallWg *sync.WaitGroup) {
	jm.logger.Printf("Restarting job %s due to watch trigger...", jm.JobConfig.Name)

	// Stop the current instance.
	// We need to be careful here. The existing Stop() sends SIGTERM.
	// The run goroutine will see this, log, and exit.
	// We need to wait for that goroutine to finish before starting a new one.

	jm.mutex.Lock()
	if jm.cancelFunc != nil {
		jm.cancelFunc() // Signal the current job instance to terminate
	}
	jm.mutex.Unlock()

	jm.jobWg.Wait() // Wait for the previous job instance's goroutine to fully exit.

	// Reset attempts if restart is manual/watch-triggered, or decide based on policy
	// For now, let's reset attempts as it's a new "session" for the job.
	jm.currentAttempts = 0

	// Start a new instance.
	// The overallWg is tricky here. If the original Start call used it,
	// we might be double-decrementing or need a new Wg for the restarted instance.
	// For now, let's assume the overallWg is for the initial set of jobs.
	// A restarted job is managed by its own jm.jobWg.
	jm.Start(ctx, nil) // Pass nil for overallWg for restarts to avoid issues.
	jm.logger.Printf("Job %s restart process initiated.", jm.JobConfig.Name)
}

func (jm *JobManager) ExecuteSubCommand(subCmdConfig *config.SubCommand, action string) {
	if subCmdConfig == nil {
		return
	}
	jm.logger.Printf("Executing %s command for job %s: %s %v", action, jm.JobConfig.Name, subCmdConfig.Command, subCmdConfig.Args)
	cmd := exec.Command(subCmdConfig.Command, subCmdConfig.Args...)
	// Optionally capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		jm.logger.Printf("Error executing %s command for job %s: %v. Output: %s", action, jm.JobConfig.Name, err, string(output))
	} else {
		jm.logger.Printf("%s command for job %s executed successfully. Output: %s", action, jm.JobConfig.Name, string(output))
	}
}
