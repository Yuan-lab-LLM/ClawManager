package services

import (
	"sync"
	"testing"
	"time"
)

func TestGetHubReturnsSameInstance(t *testing.T) {
	// Reset the package-level singleton so the test is self-contained.
	hubOnce = sync.Once{}
	hub = nil

	h1 := GetHub()
	h2 := GetHub()
	if h1 != h2 {
		t.Fatal("GetHub() returned different instances")
	}

	// Cleanup: stop the hub so the goroutine doesn't leak.
	h1.Stop()
}

func TestGetHubConcurrentAccess(t *testing.T) {
	hubOnce = sync.Once{}
	hub = nil

	const goroutines = 50
	results := make(chan *Hub, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			results <- GetHub()
		}()
	}
	wg.Wait()
	close(results)

	var first *Hub
	for h := range results {
		if first == nil {
			first = h
		} else if h != first {
			t.Fatal("GetHub() returned different instances under concurrent access")
		}
	}

	first.Stop()
}

func TestHubStopClosesClients(t *testing.T) {
	h := NewHub()
	go h.Run()

	// Register a fake client with a buffered Send channel.
	client := &Client{
		UserID: 1,
		Send:   make(chan []byte, 8),
		hub:    h,
	}
	h.register <- client

	// Give the hub a moment to process the registration.
	time.Sleep(20 * time.Millisecond)

	if h.GetClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", h.GetClientCount())
	}

	h.Stop()

	// Give the hub a moment to process the stop.
	time.Sleep(20 * time.Millisecond)

	if h.GetClientCount() != 0 {
		t.Fatalf("expected 0 clients after Stop, got %d", h.GetClientCount())
	}

	// The client's Send channel should be closed.
	_, ok := <-client.Send
	if ok {
		t.Fatal("expected client.Send to be closed after hub Stop")
	}
}
