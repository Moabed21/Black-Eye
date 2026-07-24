package bus

import (
	"sync"
	"testing"
	"time"
)

func TestBus_SubscribeAndPublish(t *testing.T) {
	b := New()
	defer b.Close()

	ch1 := b.Subscribe("cpu")
	ch2 := b.Subscribe("cpu")

	val := "test-snapshot"
	b.Publish("cpu", val)

	select {
	case msg := <-ch1:
		if msg != val {
			t.Errorf("ch1 expected %v, got %v", val, msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ch1 timed out receiving published message")
	}

	select {
	case msg := <-ch2:
		if msg != val {
			t.Errorf("ch2 expected %v, got %v", val, msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ch2 timed out receiving published message")
	}
}

func TestBus_Unsubscribe(t *testing.T) {
	b := New()
	defer b.Close()

	ch := b.Subscribe("mem")
	b.Unsubscribe("mem", ch)

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after Unsubscribe")
	}

	// Topic subscriber map should be cleaned up
	b.Publish("mem", "data")
}

func TestBus_NonBlockingPublish(t *testing.T) {
	b := New()
	defer b.Close()

	ch := b.Subscribe("slow_topic")
	_ = ch

	// Fill buffer (bufferSize = 16)
	for i := 0; i < bufferSize; i++ {
		b.Publish("slow_topic", i)
	}

	// 17th item should be dropped without blocking
	done := make(chan struct{})
	go func() {
		b.Publish("slow_topic", 999)
		close(done)
	}()

	select {
	case <-done:
		// Success — non-blocking
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Publish blocked when channel buffer was full")
	}
}

func TestBus_ConcurrentAccess(t *testing.T) {
	b := New()
	defer b.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := b.Subscribe("concurrent")
			b.Publish("concurrent", id)
			time.Sleep(5 * time.Millisecond)
			b.Unsubscribe("concurrent", ch)
		}(i)
	}
	wg.Wait()
}
