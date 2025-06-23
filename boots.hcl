boot "network_setup" {
  command = "/bin/bash"
  args    = ["-c", "echo 'Setting up network...' && ip link set eth0 up"]
  timeout = "60s"
  env     = ["BOOT_MODE=rescue"]
}

boot "another_boot_task" {
  command = "/usr/bin/touch"
  args    = ["/tmp/boot_marker"]
}
