job "memory_hog" {
  command = "./test_helpers/output_generator_bin" // Assuming mittinit is run from the repo root
  args = [
    "-lines=1000",
    "-length=1024",       // 1KB lines
    "-stderr-lines=100",
    "-stderr-length=256",
    "-delay-ms=10"        // Slow down output a bit to simulate a more realistic job
  ]
  max_attempts = 1
  can_fail = false
  enable_timestamps = true
  stdout = "{{TEMP_MEMHOG_STDOUT}}"
  stderr = "{{TEMP_MEMHOG_STDERR}}"
}

job "do_nothing_long" {
  command = "/bin/sleep"
  args = ["1"] // Shortened for tests
  max_attempts = 1
  enable_timestamps = true
  stdout = "{{TEMP_DONOTHING_STDOUT}}"
  stderr = "{{TEMP_DONOTHING_STDERR}}"
}
