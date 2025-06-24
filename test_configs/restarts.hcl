job "failing_job_critical" {
  command = "/bin/false" // This command always fails
  max_attempts = 2
  can_fail = false // Mittinit should exit after 2 attempts
  enable_timestamps = true
}

// To test can_fail=true, one would run this alone or comment out critical one
job "failing_job_canfail" {
  command = "/bin/nonexistentcommand"
  max_attempts = 2
  can_fail = true // Mittinit should NOT exit
  enable_timestamps = true
  stdout = "{{TEMP_CANFAIL_STDOUT}}"
}
