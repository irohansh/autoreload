package watcher

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	debounceDelay   = 400 * time.Millisecond
	burstThreshold  = 20
	burstWindow     = 1 * time.Second
)

var (
	ignoreDirs = []string{".git", "node_modules", "vendor", "bin", "dist", "build", ".vscode", ".idea"}
	ignoreSuffixes = []string{".tmp", ".swp", ".~", ".bak"}
)

type Watcher struct {
	root             string
	logger           *slog.Logger
	watcher          *fsnotify.Watcher
	changes          chan string
	lastPath         string
	extraIgnoreDirs  []string
	mu               sync.Mutex
	debounce         *time.Timer
	burstCount       int
	burstPending     bool
	burstWindowTimer *time.Timer
	done             chan struct{}
	wg               sync.WaitGroup
	closed           bool
}

func New(root string, logger *slog.Logger, extraIgnoreDirs []string) (*Watcher, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &os.PathError{Op: "watch", Path: absRoot, Err: os.ErrInvalid}
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		root:            absRoot,
		logger:          logger,
		watcher:         fsw,
		changes:         make(chan string, 1),
		done:            make(chan struct{}),
		extraIgnoreDirs: extraIgnoreDirs,
	}

	watched, ignored, err := w.addRecursive(absRoot)
	if err != nil {
		fsw.Close()
		return nil, err
	}
	w.logger.Info("[watcher] watching directories", "count", watched)
	w.logger.Info("[watcher] ignoring directories", "count", ignored)
	warnInotifyLimit(watched, w.logger)

	w.wg.Add(1)
	go w.run()
	return w, nil
}

func (w *Watcher) addRecursive(dir string) (watched, ignored int, err error) {
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsPermission(walkErr) {
				return filepath.SkipDir
			}
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		if w.shouldIgnore(path) {
			ignored++
			return filepath.SkipDir
		}
		if addErr := w.watcher.Add(path); addErr != nil {
			return addErr
		}
		watched++
		return nil
	})
	return watched, ignored, err
}

func (w *Watcher) shouldIgnore(path string) bool {
	rel, err := filepath.Rel(w.root, path)
	if err != nil {
		return true
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts {
		for _, ignore := range ignoreDirs {
			if part == ignore {
				return true
			}
		}
		for _, ignore := range w.extraIgnoreDirs {
			if part == ignore {
				return true
			}
		}
	}
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".#") || strings.HasPrefix(base, ".tmp") {
		return true
	}
	for _, suffix := range ignoreSuffixes {
		if strings.HasSuffix(base, suffix) || strings.HasPrefix(base, ".") && strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

func isRelevantFile(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".go", ".mod", ".sum":
		return true
	}
	if filepath.Base(path) == ".env" {
		return true
	}
	return false
}

func (w *Watcher) shouldIgnoreEvent(name string) bool {
	rel, err := filepath.Rel(w.root, name)
	if err != nil {
		return true
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts {
		for _, ignore := range ignoreDirs {
			if part == ignore {
				return true
			}
		}
		for _, ignore := range w.extraIgnoreDirs {
			if part == ignore {
				return true
			}
		}
	}
	base := filepath.Base(name)
	if strings.HasPrefix(base, ".#") || strings.HasPrefix(base, ".tmp") {
		return true
	}
	for _, suffix := range ignoreSuffixes {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

func (w *Watcher) run() {
	defer w.wg.Done()
	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.logger.Error("[watcher] error", "error", err)
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	if w.shouldIgnoreEvent(event.Name) {
		return
	}
	if event.Has(fsnotify.Chmod) && !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Remove) && !event.Has(fsnotify.Rename) {
		return
	}
	if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		info, err := os.Stat(event.Name)
		if err == nil && info.IsDir() {
			if event.Has(fsnotify.Create) {
				if !w.shouldIgnore(event.Name) {
					if err := w.watcher.Add(event.Name); err != nil {
						w.logger.Debug("[watcher] failed to add directory", "path", event.Name, "error", err)
					}
				}
			}
			if event.Has(fsnotify.Remove) {
				w.watcher.Remove(event.Name)
			}
			return
		}
		if isRelevantFile(event.Name) {
			w.debouncedNotify(filepath.Base(event.Name))
		}
	}
}

func (w *Watcher) debouncedNotify(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastPath = path
	w.burstCount++
	if w.burstWindowTimer != nil {
		w.burstWindowTimer.Stop()
	}
	w.burstWindowTimer = time.AfterFunc(burstWindow, w.onBurstWindow)
	if w.debounce != nil {
		w.debounce.Stop()
	}
	w.debounce = time.AfterFunc(debounceDelay, func() {
		w.mu.Lock()
		w.debounce = nil
		if w.burstCount > burstThreshold {
			w.burstPending = true
			w.mu.Unlock()
			return
		}
		closed := w.closed
		pathToSend := w.lastPath
		w.burstCount = 0
		w.mu.Unlock()
		if !closed && pathToSend != "" {
			select {
			case w.changes <- pathToSend:
			default:
			}
		}
	})
}

func (w *Watcher) onBurstWindow() {
	w.mu.Lock()
	w.burstWindowTimer = nil
	if !w.burstPending {
		w.mu.Unlock()
		return
	}
	w.burstPending = false
	closed := w.closed
	pathToSend := w.lastPath
	w.burstCount = 0
	w.mu.Unlock()
	if !closed && pathToSend != "" {
		select {
		case w.changes <- pathToSend:
		default:
		}
	}
}

func (w *Watcher) Changes() <-chan string {
	return w.changes
}

func warnInotifyLimit(watched int, logger *slog.Logger) {
	if runtime.GOOS != "linux" {
		return
	}
	data, err := os.ReadFile("/proc/sys/fs/inotify/max_user_watches")
	if err != nil {
		return
	}
	limit, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || limit <= 0 {
		return
	}
	if watched >= int(float64(limit)*0.9) {
		logger.Warn("[watcher] inotify limit may be exceeded",
			"watched", watched,
			"limit", limit,
			"msg", "consider increasing fs.inotify.max_user_watches")
	}
}

func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	if w.debounce != nil {
		w.debounce.Stop()
		w.debounce = nil
	}
	if w.burstWindowTimer != nil {
		w.burstWindowTimer.Stop()
		w.burstWindowTimer = nil
	}
	w.mu.Unlock()

	close(w.done)
	w.watcher.Close()
	w.wg.Wait()
	close(w.changes)
	return nil
}
