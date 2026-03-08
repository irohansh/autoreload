# hotreload

hotreload is a CLI tool that watches a project directory for source changes and automatically rebuilds and restarts your server. Edit code, save, and see the server restart within seconds—without manually stopping, rebuilding, or starting processes.

Built with Go using [fsnotify](https://github.com/fsnotify/fsnotify) for cross-platform file watching and the standard library `log/slog` for structured logging.

> **Note:** hotreload does not use third-party hot-reload frameworks (air, realize, reflex). The implementation is self-contained; fsnotify is the only external dependency for file watching.

## Table of contents

- [Get Started](#get-started)
- [Usage](#usage)
- [Features](#features)
  - [Core behavior](#core-behavior)
  - [File watching](#file-watching)
  - [Process and server management](#process-and-server-management)
  - [Build and failure handling](#build-and-failure-handling)
  - [Configuration and UX](#configuration-and-ux)
  - [Validation and robustness](#validation-and-robustness)
- [Requirements](#requirements)
- [Linux inotify limits](#linux-inotify-limits)
- [Testing](#testing)
- [Project structure](#project-structure)

## Get Started

The quickest way to try hotreload is with the included demo. From the repository root, build the binary and run it against the sample HTTP server:

```bash
make build
make demo
```

This starts hotreload watching `testserver/`. The test server listens on `http://localhost:8080`. Edit `testserver/main.go` and save—you should see a single rebuild and restart. Stop with Ctrl+C.

To run your own project:

```bash
./bin/hotreload --root ./myproject --build "go build -o ./bin/server ./cmd/server" --exec "./bin/server"
```

## Usage

| Flag | Description |
|------|-------------|
| `--root <dir>` | Directory to watch (including subdirectories). |
| `--build "<cmd>"` | Shell command to build the project. Run with working directory `--root`. |
| `--exec "<cmd>"` | Shell command to run the built server (e.g. `./bin/server`). |
| `--config <path>` | Optional path to a YAML config file. If omitted, hotreload looks for `hotreload.yaml`, `.hotreload.yaml`, or `~/.config/hotreload.yaml`. |

You can also use a config file and run with no arguments. Create `hotreload.yaml` in your project:

```yaml
root: .
build: go build -o ./bin/server .
exec: ./bin/server
ignore:
  - .git
  - node_modules
```

Then run:

```bash
hotreload
```

CLI flags override config values. Press `r` + Enter anytime to trigger a manual rebuild and restart.

## Features

When you open the README on GitHub and click **Features** in the table of contents (or scroll to this section), here is the full list of what hotreload supports.

### Core behavior

- **First build on start** — Runs a full build and starts the server as soon as you launch hotreload; no need to touch a file first.
- **Automatic rebuild and restart** — On relevant file changes, cancels any in-flight build, runs a fresh build, and restarts the server. Build and server output stream to your terminal in real time (unbuffered).
- **Build scheduler** — A single build worker consumes a request channel so only one build runs at a time; the latest change wins and previous requests are coalesced.
- **Build cancellation** — If a new change arrives while a build is running, the current build is cancelled via context and the latest tree is built instead.

### File watching

- **Recursive watching** — Watches the root directory and all subdirectories. Uses fsnotify; directories are added explicitly so the full tree is covered.
- **Dynamic directories** — Newly created directories under the watch root are added to the watcher; removed directories are removed from the watcher.
- **Relevant-file filter** — Rebuilds are triggered only when changes affect `.go`, `go.mod`, `go.sum`, or `.env`. Changes to README, images, JSON, and other non-Go files do not trigger a rebuild.
- **Debouncing** — Multiple file events in quick succession (e.g. editor save bursts) are coalesced into a single rebuild after a short delay (400 ms).
- **Git / burst handling** — When many events fire in a short window (e.g. `git checkout`), the watcher enters a burst mode and emits a single change after the burst, avoiding a storm of rebuilds.
- **Ignore list** — Skips watching and ignores events under: `.git`, `node_modules`, `vendor`, `bin`, `dist`, `build`, `.vscode`, `.idea`, and editor artifacts (e.g. `.#*`, `*.tmp`, `*.swp`, `*.~`, `*.bak`). Chmod-only events are ignored.
- **Watcher health metrics** — On startup, logs how many directories are being watched and how many ignored for easier debugging.
- **inotify limit detection (Linux)** — On Linux, reads `fs.inotify.max_user_watches` and warns if the number of watched directories is near the limit, with a hint to increase it.

### Process and server management

- **Process group termination** — On Unix, the server is started in its own process group. On restart, the entire group is signaled so child processes are terminated, not just the parent.
- **Graceful shutdown** — On restart, the server receives SIGTERM first and is given a short window (e.g. 5 s) to exit cleanly; if it does not exit, the process group is sent SIGKILL. On Windows, the process tree is terminated via `taskkill /t /f`.
- **Crash loop protection** — If the server exits within a few seconds of starting, exponential backoff is applied (1 s → 2 s → 4 s → 8 s → 16 s cap) before the next restart. After a successful longer run, backoff is reset.
- **Single restart loop** — Only one restart is scheduled per server exit; duplicate exit events do not schedule multiple builds, avoiding log spam and redundant work.
- **Smart restart (binary hash)** — After a successful build, the server binary is hashed (SHA256). If the hash is unchanged from the previous run, the server is not restarted and a one-line log explains the skip.

### Build and failure handling

- **Structured build logs** — Logs `[build] starting...`, `[build] ok` or `[build] failed` with duration and error. Build stdout/stderr are passed through so compiler output is visible. On build failure, the server is not restarted.
- **Restart summary** — Each cycle logs change path, build duration, and “server restarted” for a clear one-line summary of what happened.

### Configuration and UX

- **Config file** — Optional `hotreload.yaml` (or `.hotreload.yaml`, or `~/.config/hotreload.yaml`) with `root`, `build`, `exec`, and `ignore` list. CLI flags override config. Run `hotreload` with no args when a config file is present.
- **Manual restart hotkey** — Press `r` followed by Enter to trigger a rebuild and restart without saving a file (e.g. after changing env or config).
- **Structured logging** — Logs use prefixes such as `[watcher]`, `[build]`, `[server]`, and `[hotreload]` for easy scanning.
- **Graceful shutdown** — On SIGINT/SIGTERM, hotreload closes the watcher, waits for the runner to exit, and exits cleanly.

### Validation and robustness

- **CLI validation** — Requires `--root`, `--build`, and `--exec` (or equivalent from config); prints usage and exits with a non-zero code if missing.
- **Invalid root handling** — If the root path does not exist or is not a directory, hotreload logs an error and exits instead of proceeding.

## Requirements

- Go 1.21 or later to build.
- Linux, macOS, or Windows. File watching uses fsnotify; process termination uses process groups on Unix and `taskkill` on Windows.

## Linux inotify limits

On Linux, the number of inotify watches is limited by `fs.inotify.max_user_watches` (often 8192 by default). Large trees can hit this limit. hotreload filters out common directories to keep the count low and, when running on Linux, warns if the watch count is near the limit.

To raise the limit temporarily:

```bash
sudo sysctl -w fs.inotify.max_user_watches=65536
```

To make it persistent, add to a file under `/etc/sysctl.d/` (e.g. `99-inotify.conf`):

```
fs.inotify.max_user_watches=65536
```

## Testing

Unit and integration tests live in `internal/watcher/watcher_test.go` and `internal/process/process_test.go`.

Run all tests:

```bash
go test ./internal/watcher/ ./internal/process/ -v
```

Watcher tests cover `isRelevantFile`, `shouldIgnore`, `shouldIgnoreEvent`, and debounce behavior (multiple writes → one change). Process tests cover `Kill()` terminating a subprocess (Unix only; skipped on Windows when using `sleep`).

## Project structure

```
hotreload/
├── cmd/hotreload/
│   └── main.go           # CLI, flags, config loading, watcher/runner wiring
├── internal/
│   ├── config/
│   │   └── config.go     # YAML config load and default paths
│   ├── watcher/
│   │   ├── watcher.go   # fsnotify wrapper, recursive watch, filter, debounce, burst
│   │   └── watcher_test.go
│   ├── runner/
│   │   └── runner.go    # Build/exec orchestration, scheduler, backoff, binary hash
│   └── process/
│       ├── process.go   # Process group start/kill, graceful shutdown
│       └── process_test.go
├── testserver/
│   ├── main.go          # Demo HTTP server
│   └── hotreload.yaml   # Example config
├── go.mod
├── go.sum
├── Makefile
└── README.md
```
