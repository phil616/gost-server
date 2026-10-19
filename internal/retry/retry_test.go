package retry

import (
	"context"
	"testing"
	"time"
)

func TestExactSequencePersistenceResetAndCancellation(t *testing.T) {
	now := time.Unix(1000, 0)
	p := New(State{})
	p.Now = func() time.Time { return now }
	var saved State
	p.Save = func(s State) { saved = s }
	for _, want := range []time.Duration{5, 25, 125, 625, 3125, 3125, 3125} {
		s := p.Failed()
		if got := s.Next.Sub(now); got != want*time.Second {
			t.Fatalf("delay=%v want=%v", got, want*time.Second)
		}
		now = s.Next
	}
	if saved.Count != 5 {
		t.Fatal("retry counter must saturate")
	}
	p = New(saved)
	p.Now = func() time.Time { return now }
	var waited time.Duration
	p.Sleep = func(_ context.Context, d time.Duration) error { waited = d; return nil }
	p.Await(context.Background())
	if waited != 0 {
		t.Fatal(waited)
	}
	now = now.Add(-48 * time.Hour)
	p.Await(context.Background())
	if waited != 3125*time.Second {
		t.Fatalf("clock rollback wait=%v", waited)
	}
	p.Reset()
	if p.Failed().Next.Sub(now) != 5*time.Second {
		t.Fatal("success did not reset")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Wait(ctx, time.Hour) != context.Canceled {
		t.Fatal("wait was not cancellable")
	}
}
