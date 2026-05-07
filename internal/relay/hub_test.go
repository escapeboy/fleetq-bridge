package relay

import (
	"sync"
	"testing"
	"time"
)

func TestShouldForwardHeartbeat_FirstCallAlwaysForwards(t *testing.T) {
	c := &Conn{}
	if !c.ShouldForwardHeartbeat(60 * time.Second) {
		t.Fatal("expected first call to forward")
	}
}

func TestShouldForwardHeartbeat_ThrottledWithinInterval(t *testing.T) {
	c := &Conn{}
	if !c.ShouldForwardHeartbeat(60 * time.Second) {
		t.Fatal("first call should forward")
	}
	for i := 0; i < 10; i++ {
		if c.ShouldForwardHeartbeat(60 * time.Second) {
			t.Fatalf("call %d should be throttled", i+2)
		}
	}
}

func TestShouldForwardHeartbeat_AllowsAfterIntervalElapsed(t *testing.T) {
	c := &Conn{}
	c.ShouldForwardHeartbeat(50 * time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	if !c.ShouldForwardHeartbeat(50 * time.Millisecond) {
		t.Fatal("expected forward after interval elapsed")
	}
}

func TestShouldForwardHeartbeat_ConcurrentCallsAdmitOne(t *testing.T) {
	c := &Conn{}
	const workers = 50
	var wg sync.WaitGroup
	wg.Add(workers)
	var admitted int32
	var mu sync.Mutex
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if c.ShouldForwardHeartbeat(time.Hour) {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if admitted != 1 {
		t.Fatalf("expected exactly 1 admitted forward across %d goroutines, got %d", workers, admitted)
	}
}
