package watcher

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceDelay = 250 * time.Millisecond

var (
	ignoreDirs = []string{".git", "node_modules", "vendor", "bin", "dist", "build"}
	ignoreSuffixes = []string{".tmp", ".swp", ".~", ".bak"}
)

type Watcher struct {
	root     string
	logger   *slog.Logger
	watcher  *fsnotify.Watcher
	changes  chan struct{}
	mu       sync.Mutex
	debounce *time.Timer
	done     chan struct{}
	wg       sync.WaitGroup
	closed   bool
}

func New(root string, logger *slog.Logger) (*Watcher, error) {
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
		root:    absRoot,
		logger:  logger,
		watcher: fsw,
		changes: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}

	if err := w.addRecursive(absRoot); err != nil {
		fsw.Close()
		return nil, err
	}

	w.wg.Add(1)
	go w.run()
	return w, nil
}

func (w *Watcher) addRecursive(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsPermission(err) {
				return filepath.SkipDir
			}
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if w.shouldIgnore(path) {
			return filepath.SkipDir
		}
		return w.watcher.Add(path)
	})
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
	}
	base := filepath.Base(path)
	for _, suffix := range ignoreSuffixes {
		if strings.HasSuffix(base, suffix) || strings.HasPrefix(base, ".") && strings.HasSuffix(base, suffix) {
			return true
		}
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
	}
	base := filepath.Base(name)
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
			w.logger.Error("watcher error", "error", err)
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
						w.logger.Debug("failed to add new directory", "path", event.Name, "error", err)
					}
				}
			}
			if event.Has(fsnotify.Remove) {
				w.watcher.Remove(event.Name)
			}
		}
		w.debouncedNotify()
	}
}

func (w *Watcher) debouncedNotify() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.debounce != nil {
		w.debounce.Stop()
	}
	w.debounce = time.AfterFunc(debounceDelay, func() {
		w.mu.Lock()
		w.debounce = nil
		closed := w.closed
		w.mu.Unlock()
		if !closed {
			select {
			case w.changes <- struct{}{}:
			default:
			}
		}
	})
}

func (w *Watcher) Changes() <-chan struct{} {
	return w.changes
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
	w.mu.Unlock()

	close(w.done)
	w.watcher.Close()
	w.wg.Wait()
	close(w.changes)
	return nil
}
