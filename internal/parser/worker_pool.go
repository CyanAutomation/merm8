package parser

import "sync"

type workerPool struct {
	maxSize int
	newFn   func() (*parserWorker, error)

	mu      sync.Mutex
	cond    *sync.Cond
	idle    []*parserWorker
	total   int
	closing bool
}

func newWorkerPool(maxSize int, newFn func() (*parserWorker, error)) *workerPool {
	if maxSize < 1 {
		maxSize = 1
	}
	p := &workerPool{maxSize: maxSize, newFn: newFn}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *workerPool) borrow() (*parserWorker, error) {
	p.mu.Lock()
	for {
		if p.closing {
			p.mu.Unlock()
			return nil, ErrSubprocess
		}
		if n := len(p.idle); n > 0 {
			w := p.idle[n-1]
			p.idle = p.idle[:n-1]
			p.mu.Unlock()
			return w, nil
		}
		if p.total < p.maxSize {
			p.total++
			p.mu.Unlock()
			w, err := p.newFn()
			if err != nil {
				p.mu.Lock()
				p.total--
				p.cond.Signal()
				p.mu.Unlock()
				return nil, err
			}
			return w, nil
		}
		p.cond.Wait()
	}
}

func (p *workerPool) release(w *parserWorker, healthy bool) {
	if w == nil {
		return
	}
	p.mu.Lock()
	if p.closing || !healthy {
		p.mu.Unlock()
		w.close()

		p.mu.Lock()
		p.total--
		p.cond.Signal()
		p.mu.Unlock()
		return
	}
	p.idle = append(p.idle, w)
	p.cond.Signal()
	p.mu.Unlock()
}
