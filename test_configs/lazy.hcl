job "lazy_server" {
  command = "/bin/sh"
  // Using netcat to simulate a simple server.
  // The -e option for nc might not be available or behave as expected in all environments.
  // A more portable test might use a small Go program.
  // For now, hoping `nc -l -p PORT` is sufficient for testing connection establishment.
  // If `-e` is problematic, `nc -l -p 8081` will just accept connection and then close, which can still test spinup.
  // Let's make it echo something to stdout to confirm it ran.
  args = ["-c", "echo Lazy server process started, listening on 127.0.0.1:8081; nc -v -l -p 8081"]
  max_attempts = 3
  can_fail = false // So mittinit doesn't die if nc has issues initially
  enable_timestamps = true
  stdout = "/tmp/mittinit_lazy_server.log"
  stderr = "/tmp/mittinit_lazy_server.err"

  lazy {
    spin_up_timeout = "5s"
    cool_down_timeout = "10s" // Shortened for quicker testing
  }

  listen "proxy" {
    address = "0.0.0.0:8080" // Listen on mittinit side
    forward = "127.0.0.1:8081" // Forward to the nc job
  }
}
