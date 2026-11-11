package workbench

import (
	"context"
	"sync"
)

// killOn kills the group when ctx ends, until the returned stop is called.
// stop waits for the watcher, so nothing is signalled after the leader has
// been waited for (its PID could be reused by then).
func (g *procGroup) killOn(ctx context.Context) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			g.Kill(g.PIDs())
		case <-done:
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done); wg.Wait() }) }
}
