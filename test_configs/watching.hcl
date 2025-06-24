job "watcher_job" {
  command = "/bin/sh"
  // Corrected PID variable to $$
  args    = ["-c", "echo watcher_job started. PID: $$; trap 'echo SIGHUP received by watcher_job PID: $$' SIGHUP; while true; do echo watcher_job_ping PID: $$; sleep 1; done"]
  enable_timestamps = true
  stdout = "{{TEMP_WATCHER_LOG}}"

  watch "config_touch" {
    path = "{{WATCH_TARGET_FILE_SIGNAL}}" // Placeholder
    signal = 1 // SIGHUP
    restart = false // Just send signal
    pre_command {
      command = "/bin/echo"
      args = ["Pre-watch command for signal: File {{WATCH_TARGET_FILE_SIGNAL}} changed!"]
    }
    post_command {
      command = "/bin/echo"
      args = ["Post-watch command for signal: Signal sent to job for {{WATCH_TARGET_FILE_SIGNAL}}."]
    }
  }
}

job "restarter_job" {
  command = "/bin/sh"
  # Changed sleep to 1s for faster basic execution test, full watch test would need different handling.
  args    = ["-c", "echo restarter_job started at $(date). PID: $$; sleep 1; echo restarter_job exiting PID: $$."]
  enable_timestamps = true
  stdout = "{{TEMP_RESTARTER_LOG}}"
  max_attempts = 1 // So it doesn't restart on its own exit

  watch "config_restart" {
    path = "{{WATCH_TARGET_FILE_RESTART}}" // Placeholder
    signal = 15 // SIGTERM (to allow graceful shutdown if process traps it)
    restart = true
    // No pre/post for this one in HCL, can add if needed for more detailed parsing tests
  }
}
