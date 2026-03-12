package parser

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type trackedWorker struct {
	worker      *parserWorker
	closeMuHeld atomic.Bool
}

func TestWorkerPoolUnhealthyReleaseWaitsForCloseBeforeReturningCapacity(t *testing.T) {
	const (
		poolSize   = 1
		iterations = 8
	)

	var (
		trackedMu sync.Mutex
		tracked   []*trackedWorker
	)

	newFn := func() (*parserWorker, error) {
		tw, err := newTrackedProcessWorker(t)
		if err != nil {
			return nil, err
		}
		trackedMu.Lock()
		tracked = append(tracked, tw)
		trackedMu.Unlock()
		return tw.worker, nil
	}

	pool := newWorkerPool(poolSize, newFn)

	for i := 0; i < iterations; i++ {
		worker, err := pool.borrow()
		if err != nil {
			t.Fatalf("borrow worker: %v", err)
		}
		tw := findTrackedWorker(t, &trackedMu, tracked, worker)

		releaseDone := make(chan struct{})
		go func() {
			pool.release(worker, false)
			close(releaseDone)
		}()

		borrowDone := make(chan *parserWorker, 1)
		go func() {
			next, borrowErr := pool.borrow()
			if borrowErr != nil {
				t.Errorf("borrow replacement worker: %v", borrowErr)
				return
			}
			borrowDone <- next
		}()

		select {
		case <-borrowDone:
			t.Fatalf("borrow unexpectedly succeeded before unhealthy worker close finished")
		case <-time.After(150 * time.Millisecond):
		}

		if live := liveWorkerProcessCount(&trackedMu, tracked); live > poolSize {
			t.Fatalf("live worker process count exceeded pool cap: got %d, cap %d", live, poolSize)
		}

		unlockWorkerCloseMu(tw)

		select {
		case <-releaseDone:
		case <-time.After(2 * time.Second):
			t.Fatalf("unhealthy release did not complete after allowing close")
		}

		var replacement *parserWorker
		select {
		case replacement = <-borrowDone:
		case <-time.After(2 * time.Second):
			t.Fatalf("borrow did not complete after unhealthy close released pool capacity")
		}

		pool.release(replacement, true)
	}

	t.Cleanup(func() {
		trackedMu.Lock()
		cleanup := append([]*trackedWorker(nil), tracked...)
		trackedMu.Unlock()
		for _, tw := range cleanup {
			unlockWorkerCloseMu(tw)
			tw.worker.close()
		}
	})
}

func newTrackedProcessWorker(t *testing.T) (*trackedWorker, error) {
	t.Helper()

	cmd := exec.Command("bash", "-c", "sleep 30") //nolint:gosec
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	w := &parserWorker{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}
	tw := &trackedWorker{worker: w}
	w.closeMu.Lock()
	tw.closeMuHeld.Store(true)
	return tw, nil
}

func findTrackedWorker(t *testing.T, mu *sync.Mutex, all []*trackedWorker, worker *parserWorker) *trackedWorker {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	for _, tw := range all {
		if tw.worker == worker {
			return tw
		}
	}
	t.Fatalf("tracked worker not found")
	return nil
}

func liveWorkerProcessCount(mu *sync.Mutex, all []*trackedWorker) int {
	mu.Lock()
	snapshot := append([]*trackedWorker(nil), all...)
	mu.Unlock()

	count := 0
	for _, tw := range snapshot {
		if processAlive(tw.worker.cmd.Process) {
			count++
		}
	}
	return count
}

func processAlive(proc *os.Process) bool {
	if proc == nil {
		return false
	}
	err := proc.Signal(syscall.Signal(0))
	return err == nil || !errors.Is(err, os.ErrProcessDone)
}

func unlockWorkerCloseMu(tw *trackedWorker) {
	if tw == nil || !tw.closeMuHeld.CompareAndSwap(true, false) {
		return
	}
	tw.worker.closeMu.Unlock()
}
