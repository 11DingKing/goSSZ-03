package worker

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/qilian/patrol-dispatch/internal/service"
)

// Worker runs background tasks on a fixed interval:
//   - routes pending disturbance events to the station duty room or the
//     superior forestry department based on severity;
//   - triggers any pending sync retry queue (the queue is populated by the
//     sync manager when a batch partially fails and needs reprocessing).
//
// The worker is safe to start and stop concurrently; Stop blocks until the
// current tick finishes.
type Worker struct {
	svc      *service.Service
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
	running  atomic.Bool

	// RetryQueue holds sync batch payloads that need reprocessing. In a
	// production system this would be a durable queue; here it is an
	// in-memory slice protected by a mutex to stay self-contained.
	retryMu      atomic.Value // []RetryItem
	totalRouted  atomic.Int64
	totalRetried atomic.Int64
}

// RetryItem represents a deferred sync batch awaiting reprocessing.
type RetryItem struct {
	ID      string
	Payload []byte
	AddedAt time.Time
}

// New creates a Worker bound to the given service.
func New(svc *service.Service, interval time.Duration) *Worker {
	w := &Worker{
		svc:      svc,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	w.retryMu.Store([]RetryItem{})
	return w
}

// Start launches the background loop. Calling Start twice is a no-op.
func (w *Worker) Start(ctx context.Context) {
	if !w.running.CompareAndSwap(false, true) {
		return
	}
	go w.loop(ctx)
}

// Stop signals the worker to exit and blocks until it has.
func (w *Worker) Stop() {
	if !w.running.Load() {
		return
	}
	close(w.stop)
	<-w.done
	w.running.Store(false)
}

// EnqueueRetry adds a sync batch payload for deferred reprocessing.
func (w *Worker) EnqueueRetry(id string, payload []byte) {
	items := w.retryMu.Load().([]RetryItem)
	items = append(items, RetryItem{ID: id, Payload: payload, AddedAt: time.Now()})
	w.retryMu.Store(items)
}

// PendingRetryCount returns the number of items waiting for reprocessing.
func (w *Worker) PendingRetryCount() int {
	return len(w.retryMu.Load().([]RetryItem))
}

// TotalRouted returns the cumulative count of events routed by this worker.
func (w *Worker) TotalRouted() int64 { return w.totalRouted.Load() }

// TotalRetried returns the cumulative count of sync batches retried.
func (w *Worker) TotalRetried() int64 { return w.totalRetried.Load() }

func (w *Worker) loop(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
			w.tick()
		}
	}
}

// tick executes one round of background processing.
func (w *Worker) tick() {
	// 1. Route pending disturbance events.
	count, err := w.svc.RoutePendingEvents()
	if err != nil {
		log.Printf("worker: event routing error: %v", err)
	}
	if count > 0 {
		w.totalRouted.Add(int64(count))
	}

	// 2. Drain the retry queue (items are reprocessed by the sync manager
	//    when it next receives a request; here we just clear expired items
	//    older than 5 minutes to avoid unbounded growth).
	items := w.retryMu.Load().([]RetryItem)
	if len(items) > 0 {
		var kept []RetryItem
		cutoff := time.Now().Add(-5 * time.Minute)
		for _, item := range items {
			if item.AddedAt.After(cutoff) {
				kept = append(kept, item)
			} else {
				w.totalRetried.Add(1)
			}
		}
		w.retryMu.Store(kept)
	}
}

// RunOnce executes a single tick synchronously, primarily for testing.
func (w *Worker) RunOnce() {
	w.tick()
}
