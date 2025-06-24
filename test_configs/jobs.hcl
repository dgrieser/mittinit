job "example_server" {
  command = "./test_helpers/output_generator_bin" # Testable command
  args    = ["-lines=1", "-length=5"] # Removed unsupported flags: -prefix, -check-pwd, -env
  max_attempts = 2 # Keep low for tests
  can_fail = false
  working_directory = "{{TEST_WORKING_DIR}}" # Placeholder for test-specific temp dir
  env = [
    "FOO=bar_test",
    "GODEBUG=http2debug_test=1",
  ]
  stdout = "{{TEMP_SERVER_OUT}}" # Placeholder
  stderr = "{{TEMP_SERVER_ERR}}" # Placeholder
  enable_timestamps = true
  timestamp_format = "RFC3339Nano"

  watch "config_file" {
    path = "{{WATCH_TARGET_FILE}}" # Placeholder
    signal = 1 // SIGHUP
    restart = true
    pre_command {
      command = "/bin/echo" # Testable pre-command
      args = ["Pre-command for watch executed for {{WATCH_TARGET_FILE}}"]
    }
  }

  lazy {
    spin_up_timeout = "5s" # Shorter for tests
    cool_down_timeout = "10s" # Shorter for tests
  }

  listen "http" {
    address = "tcp://:{{TEST_LISTEN_PORT}}" # Placeholder for dynamic port
    forward = "tcp://127.0.0.1:{{TEST_FORWARD_PORT}}" # Placeholder
  }
}

job "another_job" {
  command = "/bin/echo"
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
