package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rohan/hotreload/internal/runner"
	"github.com/rohan/hotreload/internal/watcher"
)

func main() {
	root := flag.String("root", "", "Directory to watch for file changes")
	buildCmd := flag.String("build", "", "Command used to build the project")
	execCmd := flag.String("exec", "", "Command used to run the built server")
	flag.Parse()

	if *root == "" || *buildCmd == "" || *execCmd == "" {
		fmt.Fprintln(os.Stderr, "Usage: hotreload --root <project-folder> --build \"<build-command>\" --exec \"<run-command>\"")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	w, err := watcher.New(*root, logger)
	if err != nil {
		logger.Error("failed to create watcher", "error", err)
		os.Exit(1)
	}
	defer w.Close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan error, 1)
	r := runner.New(*buildCmd, *execCmd, *root, logger)
	go func() {
		done <- r.Run(w.Changes())
	}()

	select {
	case err := <-done:
		if err != nil {
			logger.Error("runner failed", "error", err)
			os.Exit(1)
		}
	case <-sigCh:
		logger.Info("shutting down...")
		w.Close()
		if err := <-done; err != nil {
			logger.Error("runner failed", "error", err)
		}
	}
}
