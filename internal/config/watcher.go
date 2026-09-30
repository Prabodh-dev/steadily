package config

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	path     string
	onChange func(*Config)
}

func NewWatcher(path string, onChange func(*Config)) *Watcher {
	return &Watcher{
		path:     path,
		onChange: onChange,
	}
}

func (w *Watcher) Start(ctx context.Context) error {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	if err := fsWatcher.Add(w.path); err != nil {
		_ = fsWatcher.Close()
		return err
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)

	go func() {
		defer fsWatcher.Close()
		defer signal.Stop(sigChan)

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-fsWatcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					w.reload()
				}
			case err, ok := <-fsWatcher.Errors:
				if !ok {
					return
				}
				slog.Error("config watcher error", "error", err)
			case <-sigChan:
				slog.Info("received SIGHUP, reloading config")
				w.reload()
			}
		}
	}()

	return nil
}

func (w *Watcher) reload() {
	newCfg, err := Load(w.path)
	if err != nil {
		slog.Error("failed to reload config", "path", w.path, "error", err)
		return
	}
	slog.Info("config reloaded successfully", "path", w.path)
	if w.onChange != nil {
		w.onChange(newCfg)
	}
}
