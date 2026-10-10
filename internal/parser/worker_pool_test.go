package parser

import (
	"bufio"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriteCloser) Close() error                { return nil }

func newUnstartedWorker() *parserWorker {
	return &parserWorker{cmd: &exec.Cmd{}, stdin: discardWriteCloser{}}
}

// @spec: WORKER-002: Releasing an unhealthy worker does not wait for an in-flight operation.
func TestWorkerPoolUnhealthyReleaseDoesNotWaitForInFlightOperation(t *testing.T) {
	created := 0
	pool := newWorkerPool(1, func() (*parserWorker, error) {
		created++
		return newUnstartedWorker(), nil
	})

	worker, err := pool.borrow()
	if err != nil {
		t.Fatalf("borrow worker: %v", err)
	}

	worker.opMu.Lock()
	locked := true
	defer func() {
		if locked {
			worker.opMu.Unlock()
		}
	}()

	releaseDone := make(chan struct{})
	go func() {
		pool.release(worker, false)
		close(releaseDone)
	}()

	select {
	case <-releaseDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("unhealthy release blocked while operation lock was held")
	}

	worker.opMu.Unlock()
	locked = false

	replacement, err := pool.borrow()
	if err != nil {
		t.Fatalf("borrow replacement worker: %v", err)
	}
	if replacement == worker {
		t.Fatal("expected an unhealthy worker to be discarded")
	}
	if created != 2 {
		t.Fatalf("worker factory called %d times, want replacement worker count 2", created)
	}
	pool.release(replacement, false)
}

func newProcessWorker(t *testing.T) (*parserWorker, error) {
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
	return w, nil
}

func TestWorkerPoolCloseUnblocksWaiters(t *testing.T) {
	pool := newWorkerPool(1, func() (*parserWorker, error) {
		worker, err := newProcessWorker(t)
		if err != nil {
			return nil, err
		}
		return worker, nil
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

		worker, err := newProcessWorker(t)
		if err != nil {
			return nil, err
		}
		return worker, nil
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
