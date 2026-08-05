// Package syncer coalesces sync requests into a single background worker.
package syncer

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tokitoki-dev/tokitoki-windows/internal/agentcli"
)

// Client is the subset of the shared-CLI client used by Syncer.
type Client interface {
	Sync(ctx context.Context, providerDirs map[string][]string) error
}

// OptionsFunc resolves provider directories for each sync attempt.
type OptionsFunc func() map[string][]string

// Syncer runs sync requests one at a time and keeps at most one queued request.
type Syncer struct {
	client  Client
	options OptionsFunc
	logger  *slog.Logger

	requests chan struct{}
	done     chan struct{}
	once     sync.Once
}

// New creates a Syncer.
func New(client Client, options OptionsFunc, logger *slog.Logger) *Syncer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Syncer{
		client:   client,
		options:  options,
		logger:   logger,
		requests: make(chan struct{}, 1),
		done:     make(chan struct{}),
	}
}

// Start runs the worker until ctx is cancelled.
func (s *Syncer) Start(ctx context.Context) {
	s.once.Do(func() {
		go s.loop(ctx)
	})
}

// Trigger requests a sync. Multiple triggers while one sync is running are
// coalesced into one follow-up run.
func (s *Syncer) Trigger() {
	select {
	case s.requests <- struct{}{}:
	default:
	}
}

// Done is closed when the worker exits.
func (s *Syncer) Done() <-chan struct{} {
	return s.done
}

func (s *Syncer) loop(ctx context.Context) {
	defer close(s.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.requests:
			s.syncOnce(ctx)
		}
	}
}

func (s *Syncer) syncOnce(ctx context.Context) {
	providerDirs := s.options()
	if len(providerDirs) == 0 {
		s.logger.Debug("skip sync; no existing provider directories")
		return
	}

	syncCtx, cancel := context.WithTimeout(ctx, agentcli.SyncTimeout)
	defer cancel()
	if err := s.client.Sync(syncCtx, providerDirs); err != nil {
		s.logger.Warn("sync failed", "error", err)
		return
	}
	s.logger.Info("sync completed")
}

// Periodically triggers sync until ctx is cancelled.
func (s *Syncer) Periodically(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.Trigger()
			}
		}
	}()
}
