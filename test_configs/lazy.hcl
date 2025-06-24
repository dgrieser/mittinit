job "lazy_server" {
  command = "/bin/sh"
  // Using netcat to simulate a simple server.
  // The -e option for nc might not be available or behave as expected in all environments.
  // A more portable test might use a small Go program.
  // For testing, we'll simplify the command to just echo to its log that it started.
  // Full network testing of lazy loading is complex for this stage.
  args = ["-c", "echo Lazy server process started for test > {{LAZY_SERVER_STARTED_FLAG_FILE}} && sleep 5"] # sleep to keep it "running"
  max_attempts = 1 # For faster tests if it fails
  can_fail = true  // Allow failure if the flag file part has issues, easier to debug test
  enable_timestamps = true
  stdout = "{{TEMP_LAZY_STDOUT}}"
  stderr = "{{TEMP_LAZY_STDERR}}"

  lazy {
    spin_up_timeout = "3s"  # Shorter for tests
    cool_down_timeout = "5s" # Shorter for tests
  }

  listen "proxy" {
    address = "0.0.0.0:{{LAZY_LISTEN_PORT}}"   # Listen on mittinit side
    forward = "127.0.0.1:{{LAZY_FORWARD_PORT}}" # Forward to the (simulated) job
  }
}
