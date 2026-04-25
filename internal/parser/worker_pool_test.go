package parser

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type trackedWorker struct {
	worker   *parserWorker
	opMuHeld atomic.Bool
}

func TestWorkerPoolUnhealthyReleaseDoesNotWaitForInFlightOperation(t *testing.T) {
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
		tw.worker.opMu.Lock()
		tw.opMuHeld.Store(true)

		releaseDone := make(chan struct{})
		go func() {
			pool.release(worker, false)
			close(releaseDone)
		}()

		select {
		case <-releaseDone:
		case <-time.After(250 * time.Millisecond):
			t.Fatalf("unhealthy release blocked while operation lock was held")
		}

		if live := liveWorkerProcessCount(&trackedMu, tracked); live > poolSize {
			t.Fatalf("live worker process count exceeded pool cap: got %d, cap %d", live, poolSize)
		}

		unlockWorkerOpMu(tw)

		replacement, err := pool.borrow()
		if err != nil {
			t.Fatalf("borrow replacement worker: %v", err)
		}

		pool.release(replacement, true)
	}

	t.Cleanup(func() {
		trackedMu.Lock()
		cleanup := append([]*trackedWorker(nil), tracked...)
		trackedMu.Unlock()
		for _, tw := range cleanup {
			unlockWorkerOpMu(tw)
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

func unlockWorkerOpMu(tw *trackedWorker) {
	if tw == nil || !tw.opMuHeld.CompareAndSwap(true, false) {
		return
	}
	tw.worker.opMu.Unlock()
}

func TestWorkerPoolCloseUnblocksWaiters(t *testing.T) {
	pool := newWorkerPool(1, func() (*parserWorker, error) {
		tw, err := newTrackedProcessWorker(t)
		if err != nil {
			return nil, err
		}
		return tw.worker, nil
	})

	worker, err := pool.borrow()
	if err != nil {
		t.Fatalf("borrow worker: %v", err)
	}

	borrowErrCh := make(chan error, 1)
	go func() {
		_, err := pool.borrow()
		borrowErrCh <- err
	}()

	if err := pool.close(); err != nil {
		t.Fatalf("close pool: %v", err)
	}

	select {
	case err := <-borrowErrCh:
		if err == nil {
			t.Fatal("expected borrow error after pool close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocked borrow to unblock")
	}

	pool.release(worker, true)
}

func TestWorkerPoolBorrowReturnsErrorWhenClosedDuringWorkerCreation(t *testing.T) {
	newFnStarted := make(chan struct{})
	allowNewFnReturn := make(chan struct{})

	pool := newWorkerPool(1, func() (*parserWorker, error) {
		close(newFnStarted)
		<-allowNewFnReturn

		tw, err := newTrackedProcessWorker(t)
		if err != nil {
			return nil, err
		}
		return tw.worker, nil
	})

	borrowResultCh := make(chan struct {
		worker *parserWorker
		err    error
	}, 1)
	go func() {
		worker, err := pool.borrow()
		borrowResultCh <- struct {
			worker *parserWorker
			err    error
		}{worker: worker, err: err}
	}()

	select {
	case <-newFnStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for newFn to start")
	}

	if err := pool.close(); err != nil {
		t.Fatalf("close pool: %v", err)
	}

	close(allowNewFnReturn)

	select {
	case result := <-borrowResultCh:
		if result.worker != nil {
			result.worker.close()
			t.Fatal("expected no worker to be returned after pool close")
		}
		if result.err == nil {
			t.Fatal("expected borrow error after pool close")
		}
		if !strings.Contains(result.err.Error(), "worker pool is closing") {
			t.Fatalf("expected pool closing error, got: %v", result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for borrow result")
	}

	func() {
		pool.mu.Lock()
		defer pool.mu.Unlock()

		if pool.total != 0 {
			t.Fatalf("expected pool total to return to zero, got %d", pool.total)
		}
		if gotIdle := len(pool.idle); gotIdle != 0 {
			t.Fatalf("expected idle workers to be empty, got %d", gotIdle)
		}
	}()

	if _, err := pool.borrow(); err == nil {
		t.Fatal("expected subsequent borrow to fail for closed pool")
	}

}
