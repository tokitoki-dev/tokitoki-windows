// Package watcher recursively watches provider data directories.
package watcher

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher manages one recursive fsnotify watcher.
type Watcher struct {
	debounce time.Duration
	onChange func()
	logger   *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// New creates a Watcher.
func New(debounce time.Duration, onChange func(), logger *slog.Logger) *Watcher {
	if debounce <= 0 {
		debounce = 2 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Watcher{
		debounce: debounce,
		onChange: onChange,
		logger:   logger,
	}
}

// Start replaces the current watch set with paths.
func (w *Watcher) Start(parent context.Context, paths []string) error {
	w.Stop()
	if len(paths) == 0 {
		return nil
	}

	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := addRecursive(fsWatcher, path); err != nil {
			_ = fsWatcher.Close()
			return err
		}
	}

	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	w.mu.Lock()
	w.cancel = cancel
	w.done = done
	w.mu.Unlock()

	go w.loop(ctx, fsWatcher, done)
	return nil
}

// Stop stops any active watcher.
func (w *Watcher) Stop() {
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.cancel = nil
	w.done = nil
	w.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (w *Watcher) loop(ctx context.Context, fsWatcher *fsnotify.Watcher, done chan<- struct{}) {
	defer close(done)
	defer fsWatcher.Close()

	var timer *time.Timer
	var timerC <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-fsWatcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := addRecursive(fsWatcher, event.Name); err != nil {
						w.logger.Debug("watch new directory", "path", event.Name, "error", err)
					}
				}
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0 {
				if timer == nil {
					timer = time.NewTimer(w.debounce)
					timerC = timer.C
				} else {
					timer.Reset(w.debounce)
				}
			}
		case err, ok := <-fsWatcher.Errors:
			if !ok {
				return
			}
			w.logger.Debug("watch error", "error", err)
		case <-timerC:
			timerC = nil
			if timer != nil {
				timer.Stop()
				timer = nil
			}
			w.onChange()
		}
	}
}

func addRecursive(fsWatcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		return fsWatcher.Add(path)
	})
}
