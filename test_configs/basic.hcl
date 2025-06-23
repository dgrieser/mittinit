boot "simple_boot" {
  command = "/bin/echo"
  args    = ["Boot job completed!"]
  timeout = "5s"
}

job "echo_once" {
  command = "/bin/echo"
  args    = ["Hello from echo_once job!"]
  max_attempts = 1
  can_fail = false
  enable_timestamps = true
  timestamp_format = "Kitchen"
}

job "echo_timestamp" {
  command = "/bin/sleep"
  args = ["2"] // Keep it short for testing
  enable_timestamps = true
  custom_timestamp_format = "2006-01-02_15:04:05.000"
  stdout = "/tmp/mittinit_echo_custom.log"
  stderr = "/tmp/mittinit_echo_custom.err"
}
