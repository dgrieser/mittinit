job "example_server" {
  command = "/usr/local/bin/server"
  args    = ["--port", "8080", "--host", "0.0.0.0"]
  max_attempts = 3
  can_fail = false
  working_directory = "/srv/app"
  env = [
    "FOO=bar",
    "GODEBUG=http2debug=1",
  ]
  stdout = "/var/log/server.out"
  stderr = "/var/log/server.err"
  enable_timestamps = true
  timestamp_format = "RFC3339Nano" // Example of a non-default format

  watch "config_file" {
    path = "/etc/server/config.json"
    signal = 1 // SIGHUP
    restart = true
    pre_command {
      command = "/usr/local/bin/validate_config"
      args = ["/etc/server/config.json"]
    }
  }

  lazy {
    spin_up_timeout = "15s"
    cool_down_timeout = "10m"
  }

  listen "http" {
    address = "tcp://:8000"
    forward = "tcp://127.0.0.1:8080"
  }
}

job "another_job" {
  command = "echo"
  args    = ["Hello from another job"]
  max_attempts = 0 // Default or no retries
  can_fail = true
  enable_timestamps = false
}

job "custom_timestamp_job" {
  command = "echo"
  args    = ["Logging with custom timestamp"]
  enable_timestamps = true
  custom_timestamp_format = "2006-01-02 15:04:05"
}
