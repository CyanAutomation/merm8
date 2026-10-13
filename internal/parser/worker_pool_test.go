package parser

import (
	"os/exec"
	"strings"
	"sync"
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

type notifyingPoolCondition struct {
	*sync.Cond
	waitStarted     chan struct{}
	waitStartedOnce sync.Once
	waitAgain       chan struct{}
	waitCalls       int
}

func (c *notifyingPoolCondition) Wait() {
	c.waitCalls++
	if c.waitCalls == 2 {
		close(c.waitAgain)
	}
	c.waitStartedOnce.Do(func() { close(c.waitStarted) })
	c.Cond.Wait()
}

func TestWorkerPoolCloseUnblocksWaiters(t *testing.T) {
	pool := newWorkerPool(1, func() (*parserWorker, error) {
		return newUnstartedWorker(), nil
	})

	worker, err := pool.borrow()
	if err != nil {
		t.Fatalf("borrow worker: %v", err)
	}
	t.Cleanup(func() {
		_ = pool.close()
		pool.release(worker, false)
	})

	waitStarted := make(chan struct{})
	waitAgain := make(chan struct{})
	pool.mu.Lock()
	pool.cond = &notifyingPoolCondition{
		Cond:        pool.cond.(*sync.Cond),
		waitStarted: waitStarted,
		waitAgain:   waitAgain,
	}
	pool.mu.Unlock()

	borrowErrCh := make(chan error, 1)
	go func() {
		_, err := pool.borrow()
		borrowErrCh <- err
	}()
	select {
	case <-waitStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for borrower to block on pool capacity")
	}

	pool.mu.Lock()
	pool.cond.Signal()
	pool.mu.Unlock()
	select {
	case <-waitAgain:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for borrower to recheck pool capacity")
	}

	if err := pool.close(); err != nil {
		t.Fatalf("close pool: %v", err)
	}

	select {
	case err := <-borrowErrCh:
		if err == nil || !strings.Contains(err.Error(), "worker pool is closing") {
			t.Fatalf("expected pool-closing error from blocked borrower, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocked borrow to unblock")
	}

}

func TestWorkerPoolBorrowReturnsErrorWhenClosedDuringWorkerCreation(t *testing.T) {
	newFnStarted := make(chan struct{})
	allowNewFnReturn := make(chan struct{})

	pool := newWorkerPool(1, func() (*parserWorker, error) {
		close(newFnStarted)
		<-allowNewFnReturn
		return newUnstartedWorker(), nil
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
