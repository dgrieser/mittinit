boot "network_setup" {
  command = "./test_helpers/output_generator_bin" # Assuming relative path from project root or adjusted in test
  args    = ["-lines=1", "-length=10"] # Removed unsupported -prefix and -env flags
  timeout = "10s" # Reduced timeout for faster tests
  env     = ["BOOT_MODE=rescue_test_value"] # Env is set for the process, but not checked in output anymore
}

boot "another_boot_task" {
  command = "/usr/bin/touch"
  args    = ["{{TEMP_BOOT_MARKER_FILE}}"] # Placeholder for temp file
}
