job "watcher_job" {
  command = "/bin/sh"
  // Corrected PID variable to $$
  args    = ["-c", "echo watcher_job started. PID: $$; trap 'echo SIGHUP received by watcher_job' SIGHUP; while true; do echo watcher_job_ping; sleep 1; done"]
  enable_timestamps = true
  stdout = "/tmp/mittinit_watcher.log"

  watch "config_touch" {
    path = "/tmp/watch_me.txt" // Run 'touch /tmp/watch_me.txt' to trigger
    signal = 1 // SIGHUP
    restart = false // Just send signal
    pre_command {
      command = "/bin/echo"
      args = ["Pre-watch command: File /tmp/watch_me.txt changed!"]
    }
    post_command {
      command = "/bin/echo"
      args = ["Post-watch command: Signal sent."]
    }
  }
}

job "restarter_job" {
  command = "/bin/sh"
  args    = ["-c", "echo restarter_job started at $(date). PID: $$; sleep 10; echo restarter_job exiting."]
  enable_timestamps = true
  stdout = "/tmp/mittinit_restarter.log"
  max_attempts = 1 // So it doesn't restart on its own exit

  watch "config_restart" {
    path = "/tmp/restart_trigger.txt" // Run 'touch /tmp/restart_trigger.txt'
    signal = 15 // SIGTERM (to allow graceful shutdown if process traps it)
    restart = true
  }
}
