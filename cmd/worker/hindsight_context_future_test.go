package main

import (
	"sync"
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/agents"
)

// Regression for the hindsight pre-injection fix (2026-09-28): the recall now
// runs concurrently with worker startup and is awaited lazily via
// hindsightContextFuture. These tests pin the future's contract.
func TestHindsightContextFuture_ResolvesWithCompletedValue(t *testing.T) {
	f := newHindsightContextFuture()
	want := &agents.ContextEnrichment{Memories: nil}
	go func() {
		time.Sleep(10 * time.Millisecond)
		f.complete(want)
	}()
	if got := f.Get(); got != want {
		t.Fatalf("Get() = %v, want the completed enrichment", got)
	}
}

func TestHindsightContextFuture_GetBlocksUntilComplete(t *testing.T) {
	f := newHindsightContextFuture()
	var wg sync.WaitGroup
	resolved := make(chan *agents.ContextEnrichment, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		resolved <- f.Get()
	}()
	select {
	case v := <-resolved:
		t.Fatalf("Get() returned %v before complete() was called", v)
	case <-time.After(20 * time.Millisecond):
	}
	f.complete(nil)
	wg.Wait()
	if v := <-resolved; v != nil {
		t.Fatalf("Get() = %v, want nil", v)
	}
}

func TestHindsightContextFuture_NilReceiverReturnsNil(t *testing.T) {
	var f *hindsightContextFuture
	if got := f.Get(); got != nil {
		t.Fatalf("nil future Get() = %v, want nil", got)
	}
}
