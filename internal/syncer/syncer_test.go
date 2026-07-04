package syncer

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/labx/tokitoki-agent/pkg/agentlib"
)

func TestSyncerCoalescesRequests(t *testing.T) {
	client := &recordingClient{started: make(chan struct{}), release: make(chan struct{})}
	options := func() agentlib.SyncOptions {
		return agentlib.SyncOptions{
			ProviderDirs: map[agentlib.Provider][]string{agentlib.ProviderClaude: []string{t.TempDir()}},
		}
	}
	syncer := New(client, options, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer.Start(ctx)

	syncer.Trigger()
	<-client.started
	syncer.Trigger()
	syncer.Trigger()
	close(client.release)

	waitForCount(t, client, 2)
	cancel()
	<-syncer.Done()
}

func TestSyncerSkipsEmptyOptions(t *testing.T) {
	client := &recordingClient{started: make(chan struct{}), release: make(chan struct{})}
	syncer := New(client, func() agentlib.SyncOptions { return agentlib.SyncOptions{} }, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer.Start(ctx)

	syncer.Trigger()
	time.Sleep(50 * time.Millisecond)
	if client.Count() != 0 {
		t.Fatalf("sync count = %d, want 0", client.Count())
	}
	cancel()
	<-syncer.Done()
}

type recordingClient struct {
	mu      sync.Mutex
	count   int
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *recordingClient) Sync(context.Context, agentlib.SyncOptions) error {
	c.mu.Lock()
	c.count++
	c.once.Do(func() { close(c.started) })
	c.mu.Unlock()
	<-c.release
	return nil
}

func (c *recordingClient) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

func waitForCount(t *testing.T, client *recordingClient, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		if client.Count() == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("sync count = %d, want %d", client.Count(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
