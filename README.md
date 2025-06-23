# mittinit

mittinit is a simple, modern init system and job runner for Linux, written in Go. It reads HCL configuration files to define jobs, boots, and probes, and manages their lifecycle, restarts, and file watching.

## Features
- **Declarative job configuration** using HCL files
- **Automatic job restarts** and signal handling
- **File and directory watching** (with glob support)
- **Support for pre/post commands** on file changes
- **Lazy jobs** and network listeners
- **Boot jobs** for one-time startup tasks
- **Health probes** (optional)
- **Graceful shutdown** on SIGINT/SIGTERM

## Quick Start

1. **Install Go** (1.20+ recommended)
2. **Clone and build mittinit:**
   ```sh
   git clone <your-repo-url>
   cd mittinit
   go build -o mittinit
   ```
3. **Create a config directory** (default: `/etc/mittnite.d`) and add `.hcl` config files (see below).
4. **Run:**
   ```sh
   sudo ./mittinit up --config-dir /etc/mittnite.d
   ```

## Example HCL Job Config
```hcl
job "myjob" {
  command = "/usr/bin/myservice"
  args    = ["--flag1", "value"]
  max_attempts = 3
  watch {
    name   = "config"
    path   = "/etc/myservice/*.conf"
    restart = true
    pre_command {
      command = "/usr/bin/echo"
      args    = ["Config changed!"]
    }
  }
}
```

## Configuration Structure
- **Jobs**: Main processes to run and supervise
- **Boots**: One-time startup jobs
- **Probes**: Health checks (optional)

## File Watching & Globs
- Supports watching files, directories, and glob patterns (e.g., `/etc/myapp/*.conf`)
- Dynamically adds new files matching globs during runtime
- Can trigger restarts or send signals on changes

## Development & Testing
- Run all tests:
  ```sh
  go test ./cmd/...
  ```
- Test helpers are in `test_helpers/`
