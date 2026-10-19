package retry

import (
	"context"
	"sync"
	"time"
)

var Delays = [...]time.Duration{5 * time.Second, 25 * time.Second, 125 * time.Second, 625 * time.Second, 3125 * time.Second}

type State struct {
	Count int       `json:"count"`
	Next  time.Time `json:"next"`
}
type Policy struct {
	mu    sync.Mutex
	state State
	Now   func() time.Time
	Sleep func(context.Context, time.Duration) error
	Save  func(State)
}

func New(s State) *Policy { return &Policy{state: s, Now: time.Now, Sleep: Wait} }
func Wait(ctx context.Context, d time.Duration) error {
	if d < 0 {
		d = 0
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (p *Policy) Snapshot() State { p.mu.Lock(); defer p.mu.Unlock(); return p.state }
func (p *Policy) Reset() {
	p.mu.Lock()
	p.state = State{}
	s := p.state
	p.mu.Unlock()
	if p.Save != nil {
		p.Save(s)
	}
}
func (p *Policy) Failed() State {
	p.mu.Lock()
	i := p.state.Count
	if i < 0 {
		i = 0
	}
	if i >= len(Delays) {
		i = len(Delays) - 1
	}
	p.state = State{Count: i + 1, Next: p.Now().Add(Delays[i])}
	s := p.state
	p.mu.Unlock()
	if p.Save != nil {
		p.Save(s)
	}
	return s
}
func (p *Policy) Await(ctx context.Context) error {
	s := p.Snapshot()
	d := s.Next.Sub(p.Now())
	// A persisted wall clock deadline must not become an unbounded wait after
	// the system clock moves backwards. Normal retries retain their exact delay.
	if d > Delays[len(Delays)-1] {
		d = Delays[len(Delays)-1]
	}
	return p.Sleep(ctx, d)
}
func (p *Policy) Retry(ctx context.Context, _ error) error { p.Failed(); return p.Await(ctx) }
