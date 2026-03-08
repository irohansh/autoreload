package process

import (
	"context"
	"log/slog"
	"runtime"
	"testing"
)

func TestKillTerminatesProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep not available on Windows")
	}
	workDir := t.TempDir()
	logger := slog.Default()
	ctx := context.Background()

	p, err := Start(ctx, "sleep", []string{"10"}, workDir, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	err = p.Wait()
	if err == nil {
		t.Fatal("expected Wait() to return error after Kill()")
	}
}
