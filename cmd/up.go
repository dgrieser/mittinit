package cmd

import (
	"context"
	"log"

	"io"
	"mittinit/config"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

// appLogger is declared here, to be initialized in init() or main
var appLogger *log.Logger

// Ensure this init function is correctly placed and rootCmd is accessible.
// If up.go is part of the cmd package, rootCmd (defined in root.go) should be accessible.
func init() {
	// Assuming rootCmd is a global or package-level variable in the cmd package (e.g. in root.go)
	rootCmd.AddCommand(upCmd)
	appLogger = log.New(os.Stdout, "mittinit: ", log.LstdFlags|log.Lmicroseconds)
}

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Run jobs defined in HCL configuration files",
	Long:  `The up command is the core of mittinit. It reads HCL configuration files and runs the defined jobs.`,
	Run: func(cobraCmd *cobra.Command, args []string) { // Renamed cmd to cobraCmd to avoid conflict
		appLogger.Printf("mittinit starting up...")
		// Ensure configDir is accessible. It's typically a global flag variable from root.go
		appLogger.Printf("Loading configuration from directory: %s", configDir)
		cfg, err := config.LoadConfig(configDir)
		if err != nil {
			appLogger.Fatalf("Error loading config: %v", err)
		}

		if cfg == nil {
			appLogger.Fatalf("Config is nil after loading.")
		}

		appLogger.Println("Configuration loaded successfully.")

		mainCtx, cancelMainCtx := context.WithCancel(context.Background())
		defer cancelMainCtx()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		var jobManagers []*JobManager
		var jobWg sync.WaitGroup

		if len(cfg.Boots) > 0 {
			appLogger.Printf("Executing %d boot job(s)...", len(cfg.Boots))
			for _, bootConfig := range cfg.Boots {
				// Pass mainCtx to boot jobs if they need to be cancellable with the app
				// For now, ExecuteBootJob is synchronous and uses its own timeout if specified.
				if err := ExecuteBootJob(bootConfig, appLogger, mainCtx); err != nil {
					appLogger.Fatalf("Boot job %s failed: %v. Terminating.", bootConfig.Name, err)
				}
			}
			appLogger.Println("All boot jobs completed successfully.")
		} else {
			appLogger.Println("No boot jobs defined.")
		}

		jobRunners := make(map[string]*JobManager)

		if len(cfg.Jobs) > 0 {
			appLogger.Printf("Initializing %d regular job(s)...", len(cfg.Jobs))
			for _, jobConfig := range cfg.Jobs {
				jm := NewJobManager(jobConfig, appLogger)
				jobManagers = append(jobManagers, jm)
				jobRunners[jobConfig.Name] = jm

				if jobConfig.Lazy != nil && len(jobConfig.Listen) > 0 {
					appLogger.Printf("Job %s is configured for lazy loading. Setting up listeners.", jobConfig.Name)
					// Lazy loading setup will happen here
					// For now, it means the job is not started immediately by the main loop
					// but will be managed by its listener logic.
					// We still add it to jobManagers for potential signal handling / graceful shutdown.
					go setupLazyJob(mainCtx, jm, &jobWg)
				} else {
					jobWg.Add(1)
					jm.Start(mainCtx, &jobWg)
					if len(jobConfig.Watch) > 0 {
						go watchJobFiles(mainCtx, jm, &jobWg) // Pass mainCtx for cancellation
					}
				}
			}
		} else {
			appLogger.Println("No regular jobs defined to run.")
		}

		go func() {
			sig := <-sigChan
			appLogger.Printf("Received signal: %s. Initiating shutdown...", sig)
			cancelMainCtx()
		}()

		// Wait for all jobs to complete or for shutdown signal
		// This needs to be robust whether there are jobs or not.

		shutdownComplete := make(chan struct{})
		go func() {
			jobWg.Wait() // Wait for all initially started jobs and lazy jobs that might have started
			close(shutdownComplete)
		}()

		select {
		case <-shutdownComplete:
			appLogger.Println("All jobs have completed.")
		case <-mainCtx.Done(): // Triggered by signal
			appLogger.Println("Shutdown signal received. Waiting for jobs to terminate...")
			// After context is cancelled, give jobs a chance to stop.
			// Then wait for jobWg again, but with a timeout.
			select {
			case <-shutdownComplete: // Jobs finished quickly after cancel
				appLogger.Println("All jobs terminated gracefully after shutdown signal.")
			case <-time.After(10 * time.Second): // Timeout for graceful shutdown
				appLogger.Println("Timeout waiting for jobs to terminate. Some jobs may not have exited cleanly.")
			}
		}

		// Final cleanup of job managers (e.g. ensuring all associated resources like listeners are closed)
		// Most of this should be handled by the job's own context cancellation.
		// Explicitly stopping here can be a safeguard.
		appLogger.Println("Executing final stop for all job managers...")
		for _, jm := range jobManagers {
			jm.Stop() // Ensure stop is called, it's idempotent
		}
		// A small delay to allow final stop signals to be processed if any were missed.
		time.Sleep(100 * time.Millisecond)

		appLogger.Println("mittinit shut down.")
	},
}

func watchJobFiles(ctx context.Context, jm *JobManager, jobWg *sync.WaitGroup) {
	if len(jm.JobConfig.Watch) == 0 {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		appLogger.Printf("[%s] Error creating fsnotify watcher: %v", jm.GetName(), err)
		return
	}
	defer watcher.Close()

	// Add paths to watcher
	// Store a map of path to its Watch config for easy lookup
	watchMap := make(map[string]config.Watch)

	for _, w := range jm.JobConfig.Watch {
		// fsnotify needs an existing path. If it's a glob, this is more complex.
		// For now, assume direct paths or directories.
		// TODO: Handle glob patterns by expanding them or using a library that supports glob watching.
		// For simplicity, this example assumes w.Path is a concrete file or directory.
		appLogger.Printf("[%s] Watching path '%s' for changes.", jm.GetName(), w.Path)
		err = watcher.Add(w.Path)
		if err != nil {
			appLogger.Printf("[%s] Error adding path '%s' to watcher: %v", jm.GetName(), w.Path, err)
			continue
		}
		watchMap[w.Path] = w // Simple mapping; might need refinement if paths overlap or are non-canonical
	}

	if len(watchMap) == 0 {
		appLogger.Printf("[%s] No valid paths found to watch.", jm.GetName())
		return
	}

	for {
		select {
		case <-ctx.Done():
			appLogger.Printf("[%s] Stopping file watcher due to context cancellation.", jm.GetName())
			return
		case event, ok := <-watcher.Events:
			if !ok {
				appLogger.Printf("[%s] File watcher events channel closed.", jm.GetName())
				return
			}
			// fsnotify can send multiple events for a single save (e.g., RENAME, CHMOD, WRITE)
			// We typically care about WRITE or CREATE.
			// Also, some editors write to a temp file and rename, triggering RENAME or CREATE.
			// Consider debouncing or filtering events if needed.
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Rename == fsnotify.Rename {
				appLogger.Printf("[%s] File change detected: %s on %s", jm.GetName(), event.Op.String(), event.Name)

				// Find the specific watch config that matches the event path.
				// This needs to be robust. event.Name is usually the absolute path.
				var matchedWatchConfig *config.Watch
				for path, wc := range watchMap {
					// If wc.Path is a directory, event.Name could be a file inside it.
					// If wc.Path is a file, event.Name should match it.
					if strings.HasPrefix(event.Name, path) { // This is a basic check.
						mw := wc // Copy wc to avoid issues with loop variable address
						matchedWatchConfig = &mw
						break
					}
				}

				if matchedWatchConfig != nil {
					appLogger.Printf("[%s] Change on '%s' matched watch config for path '%s'", jm.GetName(), event.Name, matchedWatchConfig.Path)

					jm.ExecuteSubCommand(matchedWatchConfig.PreCommand, "pre-watch")

					if matchedWatchConfig.Restart {
						appLogger.Printf("[%s] Restarting due to file change on '%s'", jm.GetName(), event.Name)
						if matchedWatchConfig.Signal != 0 {
							// Send signal to the old process before restarting
							// The jm.Restart method should handle stopping the old process first.
							// Sending an explicit signal here might be redundant if Restart logic is good.
							// However, if the signal is for graceful shutdown before the hard restart, it's useful.
							appLogger.Printf("[%s] Sending signal %d before restart.", jm.GetName(), matchedWatchConfig.Signal)
							jm.SendSignal(syscall.Signal(matchedWatchConfig.Signal))
							time.Sleep(time.Duration(500) * time.Millisecond) // Give a moment for signal processing
						}
						jm.Restart(ctx, jobWg) // Pass context and original jobWg
					} else if matchedWatchConfig.Signal != 0 {
						appLogger.Printf("[%s] Sending signal %d due to file change on '%s'", jm.GetName(), matchedWatchConfig.Signal, event.Name)
						err := jm.SendSignal(syscall.Signal(matchedWatchConfig.Signal))
						if err != nil {
							appLogger.Printf("[%s] Error sending signal for watch: %v", jm.GetName(), err)
						}
					}

					jm.ExecuteSubCommand(matchedWatchConfig.PostCommand, "post-watch")
				} else {
					appLogger.Printf("[%s] File change on '%s' did not match any specific watch configurations.", jm.GetName(), event.Name)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				appLogger.Printf("[%s] File watcher errors channel closed.", jm.GetName())
				return
			}
			appLogger.Printf("[%s] Error from file watcher: %v", jm.GetName(), err)
		}
	}
}

// setupLazyJob will be implemented to handle lazy loading logic
func setupLazyJob(ctx context.Context, jm *JobManager, appWg *sync.WaitGroup) {
	// This function will set up listeners and manage the job's lifecycle based on network activity.
	// It needs to interact with jm.Start() and jm.Stop().
	// It also needs to decrement appWg when the lazy job and its listeners are truly finished,
	// especially on graceful shutdown.
	appLogger.Printf("[%s] Setting up lazy job with %d listeners.", jm.GetName(), len(jm.JobConfig.Listen))

	if len(jm.JobConfig.Listen) == 0 {
		appLogger.Printf("[%s] Lazy job configured but no listeners defined. Job will not start.", jm.GetName())
		return // Or handle as an error
	}

	// Add to the app's WaitGroup when the lazy job's "supervisor" goroutine starts.
	// This ensures mittinit waits for lazy jobs to clean up during shutdown.
	appWg.Add(1)
	defer appWg.Done() // Decrement when this supervisor goroutine exits.

	var listeners []net.Listener
	var listenerWg sync.WaitGroup // To manage individual listener goroutines

	for _, lc := range jm.JobConfig.Listen {
		listenConfig := lc // Capture range variable
		appLogger.Printf("[%s] Attempting to listen on %s, forwarding to %s", jm.GetName(), listenConfig.Address, listenConfig.Forward)

		// Parse listenConfig.Address (e.g. "0.0.0.0:8080" or "tcp://0.0.0.0:8080")
		// For simplicity, assume "host:port" for now.
		// TODO: More robust address parsing (protocol, etc.)
		addr := listenConfig.Address
		if strings.HasPrefix(addr, "tcp://") {
			addr = strings.TrimPrefix(addr, "tcp://")
		}

		ln, err := net.Listen("tcp", addr)
		if err != nil {
			appLogger.Printf("[%s] Error listening on %s: %v. This listener will be skipped.", jm.GetName(), listenConfig.Address, err)
			continue
		}
		listeners = append(listeners, ln)
		appLogger.Printf("[%s] Listening on %s, will forward to %s", jm.GetName(), ln.Addr().String(), listenConfig.Forward)

		listenerWg.Add(1)
		go func(l net.Listener, lconf config.Listen) {
			defer listenerWg.Done()
			defer l.Close() // Ensure listener is closed when this goroutine exits

			for {
				select {
				case <-ctx.Done(): // Main context cancelled
					appLogger.Printf("[%s] Listener on %s stopping due to context cancellation.", jm.GetName(), l.Addr().String())
					return
				default:
					// Set a deadline for accept to make it non-blocking and check ctx.Done() periodically
					if tcpLn, ok := l.(*net.TCPListener); ok {
						tcpLn.SetDeadline(time.Now().Add(500 * time.Millisecond))
					}
					conn, err := l.Accept()
					if err != nil {
						if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
							continue // Timeout, loop again to check context
						}
						// Check if context was cancelled during a blocking Accept
						select {
						case <-ctx.Done():
							// Already handled by the select case above, but good to double check
							return
						default:
							// Real error
							appLogger.Printf("[%s] Error accepting connection on %s: %v", jm.GetName(), l.Addr().String(), err)
							// Depending on error, might want to break or continue
							if !strings.Contains(err.Error(), "use of closed network connection") {
								// If it's not a "closed connection" error (which happens on shutdown), log it.
							}
							return // Stop this listener goroutine on significant errors
						}
					}
					// Successfully accepted a connection
					appLogger.Printf("[%s] Accepted connection on %s from %s", jm.GetName(), l.Addr().String(), conn.RemoteAddr().String())
					go handleLazyConnection(ctx, conn, jm, lconf.Forward, appWg)
				}
			}
		}(ln, listenConfig)
	}

	if len(listeners) == 0 {
		appLogger.Printf("[%s] Lazy job: No listeners could be successfully started. Job will not activate.", jm.GetName())
		// Since we added to appWg, we must ensure it's decremented if we exit early.
		// However, the defer appWg.Done() handles this.
		return
	}

	// Goroutine to manage job cooldown
	go manageLazyJobCooldown(ctx, jm)

	// Wait for all listener goroutines to complete (e.g., on shutdown)
	listenerWg.Wait()
	appLogger.Printf("[%s] All listeners for lazy job have shut down.", jm.GetName())

	// Ensure the job itself is stopped if it was running
	jm.Stop()
	jm.Wait() // Wait for the job process to fully exit
	appLogger.Printf("[%s] Lazy job supervisor finished.", jm.GetName())
}

var (
	activeConnections      = make(map[string]int) // Job name to connection count
	activeConnectionsMutex sync.Mutex
	lastActivityTime       = make(map[string]time.Time) // Job name to last activity time
	lastActivityTimeMutex  sync.Mutex
)

func handleLazyConnection(ctx context.Context, clientConn net.Conn, jm *JobManager, forwardAddress string, appWg *sync.WaitGroup) {
	defer clientConn.Close()

	activeConnectionsMutex.Lock()
	activeConnections[jm.GetName()]++
	activeConnectionsMutex.Unlock()

	lastActivityTimeMutex.Lock()
	lastActivityTime[jm.GetName()] = time.Now()
	lastActivityTimeMutex.Unlock()

	defer func() {
		activeConnectionsMutex.Lock()
		activeConnections[jm.GetName()]--
		count := activeConnections[jm.GetName()]
		activeConnectionsMutex.Unlock()

		if count == 0 {
			lastActivityTimeMutex.Lock()
			lastActivityTime[jm.GetName()] = time.Now() // Mark time when last conn closed
			lastActivityTimeMutex.Unlock()
			appLogger.Printf("[%s] Last active connection closed. Current count: %d", jm.GetName(), count)
		}
	}()

	// Ensure job is running
	jm.mutex.Lock()
	wasRunning := jm.running
	jm.mutex.Unlock()

	if !wasRunning {
		appLogger.Printf("[%s] First connection. Starting job for forwarding...", jm.GetName())
		// This needs to be synchronous enough to ensure the job is ready or starting
		// The Start method is async, so we need to manage its readiness.
		// For simplicity, let's assume Start will make it ready soon.
		// A more robust solution would involve a condition variable or channel.

		// We pass nil for the WaitGroup to Start because the appWg is managed by setupLazyJob for the supervisor.
		// The job's own lifecycle (restarts, etc.) is managed by its internal jobWg.
		jm.Start(ctx, nil) // Start the job if not already running. Start should be idempotent or check 'running' status.

		// Wait for job to be ready or timeout (SpinUpTimeout)
		// This is a simplified wait. A real implementation would check if the forward port is connectable.
		spinUpTimeoutDuration := 5 * time.Second // Default, should come from jm.JobConfig.Lazy.SpinUpTimeout
		if jm.JobConfig.Lazy != nil && jm.JobConfig.Lazy.SpinUpTimeout != "" {
			d, err := time.ParseDuration(jm.JobConfig.Lazy.SpinUpTimeout)
			if err == nil {
				spinUpTimeoutDuration = d
			} else {
				appLogger.Printf("[%s] Invalid SpinUpTimeout format '%s': %v. Using default %s", jm.GetName(), jm.JobConfig.Lazy.SpinUpTimeout, err, spinUpTimeoutDuration)
			}
		}

		// Crude way to wait for spin-up: try to connect to the forward address.
		ready := false
		startTime := time.Now()
		for time.Since(startTime) < spinUpTimeoutDuration {
			dialCtx, dialCancel := context.WithTimeout(ctx, 100*time.Millisecond)
			targetConn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", forwardAddress)
			dialCancel()
			if err == nil {
				targetConn.Close()
				ready = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if !ready {
			appLogger.Printf("[%s] Job did not become ready on %s within SpinUpTimeout (%s). Closing client connection.", jm.GetName(), forwardAddress, spinUpTimeoutDuration)
			return
		}
		appLogger.Printf("[%s] Job assumed ready for forwarding to %s.", jm.GetName(), forwardAddress)
	} else {
		appLogger.Printf("[%s] Job already running. Proceeding with forwarding.", jm.GetName())
	}

	// Connect to the target service
	targetConn, err := net.DialTimeout("tcp", forwardAddress, 5*time.Second) // Timeout for connecting to backend
	if err != nil {
		appLogger.Printf("[%s] Failed to connect to target %s: %v", jm.GetName(), forwardAddress, err)
		return
	}
	defer targetConn.Close()

	appLogger.Printf("[%s] Connection established to target %s. Proxying data.", jm.GetName(), forwardAddress)

	// Proxy data
	proxyWg := sync.WaitGroup{}
	proxyWg.Add(2)

	go func() {
		defer proxyWg.Done()
		defer targetConn.Close() // Close target when client reads are done or error
		defer clientConn.Close() // Also close client if target write fails
		_, err := io.Copy(targetConn, clientConn)
		if err != nil && err != io.EOF {
			if !strings.Contains(err.Error(), "use of closed network connection") {
				appLogger.Printf("[%s] Error copying data from client to target: %v", jm.GetName(), err)
			}
		}
	}()
	go func() {
		defer proxyWg.Done()
		defer clientConn.Close() // Close client when target reads are done or error
		defer targetConn.Close() // Also close target if client write fails
		_, err := io.Copy(clientConn, targetConn)
		if err != nil && err != io.EOF {
			if !strings.Contains(err.Error(), "use of closed network connection") {
				appLogger.Printf("[%s] Error copying data from target to client: %v", jm.GetName(), err)
			}
		}
	}()

	proxyWg.Wait()
	appLogger.Printf("[%s] Finished proxying for connection from %s.", jm.GetName(), clientConn.RemoteAddr().String())
}

func manageLazyJobCooldown(ctx context.Context, jm *JobManager) {
	if jm.JobConfig.Lazy == nil {
		return // Not a lazy job
	}

	coolDownTimeoutDuration := 15 * time.Minute // Default
	if jm.JobConfig.Lazy.CoolDownTimeout != "" {
		d, err := time.ParseDuration(jm.JobConfig.Lazy.CoolDownTimeout)
		if err == nil {
			coolDownTimeoutDuration = d
		} else {
			appLogger.Printf("[%s] Invalid CoolDownTimeout format '%s': %v. Using default %s", jm.GetName(), jm.JobConfig.Lazy.CoolDownTimeout, err, coolDownTimeoutDuration)
		}
	}

	if coolDownTimeoutDuration <= 0 {
		appLogger.Printf("[%s] CoolDownTimeout is zero or negative, cooldown management disabled.", jm.GetName())
		return // Cooldown disabled
	}

	appLogger.Printf("[%s] Cooldown manager started. Timeout: %s", jm.GetName(), coolDownTimeoutDuration)
	ticker := time.NewTicker(coolDownTimeoutDuration / 2) // Check periodically
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			appLogger.Printf("[%s] Cooldown manager stopping due to context cancellation.", jm.GetName())
			return
		case <-ticker.C:
			jm.mutex.Lock()
			isRunning := jm.running
			jm.mutex.Unlock()

			if !isRunning {
				continue // Job is not running, no need to check for cooldown.
			}

			activeConnectionsMutex.Lock()
			currentActiveConns := activeConnections[jm.GetName()]
			activeConnectionsMutex.Unlock()

			if currentActiveConns > 0 {
				continue // Still active connections
			}

			// No active connections, check last activity time
			lastActivityTimeMutex.Lock()
			lastActivity, hasActivity := lastActivityTime[jm.GetName()]
			lastActivityTimeMutex.Unlock()

			if hasActivity && time.Since(lastActivity) >= coolDownTimeoutDuration {
				appLogger.Printf("[%s] CoolDownTimeout (%s) reached with no active connections since %s. Stopping job.", jm.GetName(), coolDownTimeoutDuration, lastActivity.Format(time.RFC3339))
				jm.Stop() // Stop the job
				// The job's own goroutine will handle Cmd.Wait() and exit.
				// jm.Wait() here would block if the job doesn't stop quickly.
				// The main shutdown logic should handle final jm.Wait().
			}
		}
	}
}
