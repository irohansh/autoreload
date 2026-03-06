# hotreload

A CLI tool that watches a project folder for code changes and automatically rebuilds and restarts the server.

## Usage

```bash
hotreload --root <project-folder> --build "<build-command>" --exec "<run-command>"
```

### Parameters

- `--root <project-folder>` - Directory to watch for file changes (including all subfolders)
- `--build "<build-command>"` - Command used to build the project when a change is detected
- `--exec "<run-command>"` - Command used to run the built server after a successful build

### Example

```bash
hotreload --root ./myproject --build "go build -o ./bin/server ./cmd/server" --exec "./bin/server"
```

Edit code and save; the server restarts automatically within seconds.

## Building

```bash
make build
```

Or directly:

```bash
go build -o bin/hotreload ./cmd/hotreload
```

## Demo

A sample HTTP server is included in `testserver/`. Run the demo:

```bash
make demo
```

This starts hotreload watching the testserver. Edit `testserver/main.go` and save to see the server rebuild and restart. The test server listens on `http://localhost:8080`.

## Features

- **File watching**: Recursively watches all subdirectories, including newly created folders
- **Debouncing**: Multiple rapid file events (e.g., from editor saves) trigger a single rebuild
- **Build cancellation**: If a new change arrives during a build, the previous build is discarded
- **Process management**: Kills the entire process group (parent and children) when restarting
- **Crash loop prevention**: If the server exits within 3 seconds of starting, exponential backoff is applied before the next restart
- **File filtering**: Ignores `.git/`, `node_modules/`, `vendor/`, `bin/`, `dist/`, `build/`, and editor temp files

## inotify Limits (Linux)

On Linux, the number of watched files is limited by `fs.inotify.max_user_watches` (default often 8192). For large projects, you may need to increase it:

```bash
# Temporary (until reboot)
sudo sysctl -w fs.inotify.max_user_watches=65536

# Permanent: add to /etc/sysctl.d/99-inotify.conf
fs.inotify.max_user_watches=65536
```

The tool filters out common directories (`.git`, `node_modules`, etc.) to keep the watch count low.

## Requirements

- Go 1.21 or later
- Linux, macOS, or Windows
