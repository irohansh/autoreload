package main

import (
	"bufio"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rohan/hotreload/internal/config"
	"github.com/rohan/hotreload/internal/runner"
	"github.com/rohan/hotreload/internal/watcher"
)

func main() {
	configPath := flag.String("config", "", "Path to hotreload.yaml (optional)")
	root := flag.String("root", "", "Directory to watch for file changes")
	buildCmd := flag.String("build", "", "Command used to build the project")
	execCmd := flag.String("exec", "", "Command used to run the built server")
	flag.Parse()

	var cfg *config.Config
	if *configPath != "" {
		var err error
		cfg, err = config.Load(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[hotreload] load config: %v\n", err)
			os.Exit(1)
		}
	} else {
		for _, p := range config.DefaultPaths() {
			cfg, _ = config.Load(p)
			if cfg != nil {
				break
			}
		}
	}

	if cfg != nil {
		if *root == "" {
			*root = cfg.Root
		}
		if *buildCmd == "" {
			*buildCmd = cfg.Build
		}
		if *execCmd == "" {
			*execCmd = cfg.Exec
		}
	}

	if *root == "" || *buildCmd == "" || *execCmd == "" {
		fmt.Fprintln(os.Stderr, "Usage: hotreload [--config <path>] --root <dir> --build \"<cmd>\" --exec \"<cmd>\"")
		fmt.Fprintln(os.Stderr, "  Or create hotreload.yaml with root, build, exec, ignore.")
		os.Exit(1)
	}

	var extraIgnore []string
	if cfg != nil && len(cfg.Ignore) > 0 {
		extraIgnore = cfg.Ignore
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	w, err := watcher.New(*root, logger, extraIgnore)
	if err != nil {
		logger.Error("[hotreload] failed to create watcher", "error", err)
		os.Exit(1)
	}
	defer w.Close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	manualRestart := make(chan struct{}, 1)
	logger.Info("[hotreload] Press 'r' + Enter to rebuild manually")
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == "r" {
				select {
				case manualRestart <- struct{}{}:
				default:
				}
			}
		}
	}()

	done := make(chan error, 1)
	r := runner.New(*buildCmd, *execCmd, *root, logger)
	go func() {
		done <- r.Run(w.Changes(), manualRestart)
	}()

	select {
	case err := <-done:
		if err != nil {
			logger.Error("[hotreload] runner failed", "error", err)
			os.Exit(1)
		}
	case <-sigCh:
		logger.Info("[hotreload] shutting down...")
		w.Close()
		if err := <-done; err != nil {
			logger.Error("[hotreload] runner failed", "error", err)
		}
	}
}
