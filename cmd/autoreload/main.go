package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/irohansh/autoreload/pkg/autoreload"
)

// version is set at build time via -ldflags "-X main.version=...".
// It defaults to "dev" for local/unversioned builds.
var version = "dev"

func main() {
	configPath := flag.String("config", "", "Path to autoreload.yaml (optional)")
	root := flag.String("root", "", "Directory to watch for file changes")
	buildCmd := flag.String("build", "", "Command used to build the project")
	execCmd := flag.String("exec", "", "Command used to run the built server")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("autoreload %s\n", version)
		return
	}

	var cfg *autoreload.Config
	if *configPath != "" {
		var err error
		cfg, err = autoreload.Load(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[autoreload] load config: %v\n", err)
			os.Exit(1)
		}
	} else {
		for _, p := range autoreload.DefaultPaths() {
			cfg, _ = autoreload.Load(p)
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
		fmt.Fprintln(os.Stderr, "Usage: autoreload [--config <path>] --root <dir> --build \"<cmd>\" --exec \"<cmd>\"")
		fmt.Fprintln(os.Stderr, "  Or create autoreload.yaml with root, build, exec, ignore.")
		os.Exit(1)
	}

	opts := autoreload.Options{
		Root:  *root,
		Build: *buildCmd,
		Exec:  *execCmd,
	}
	if cfg != nil {
		opts.Ignore = cfg.Ignore
	}

	engine, err := autoreload.New(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[autoreload] %v\n", err)
		os.Exit(1)
	}
	defer engine.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Fprintln(os.Stderr, "[autoreload] Press 'r' + Enter to rebuild manually")
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == "r" {
				engine.Restart()
			}
		}
	}()

	if err := engine.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[autoreload] runner failed: %v\n", err)
		os.Exit(1)
	}
}
