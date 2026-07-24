// Package bus implements a typed, fan-out event bus for BlackEye.
// Services publish snapshots to named topics; TUI tabs subscribe to topics.
// All operations are goroutine-safe.
package bus

import (
	"sync"
)

const bufferSize = 16 // per-subscriber channel buffer

// Bus is a fan-out publish/subscribe event bus.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan interface{}
}

// New creates a new empty Bus.
func New() *Bus {
	return &Bus{
		subscribers: make(map[string][]chan interface{}),
	}
}

// Subscribe returns a channel that receives events published to topic.
// The caller must call Unsubscribe when done to avoid goroutine leaks.
func (b *Bus) Subscribe(topic string) <-chan interface{} {
	ch := make(chan interface{}, bufferSize)
	b.mu.Lock()
	b.subscribers[topic] = append(b.subscribers[topic], ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a previously subscribed channel from topic.
func (b *Bus) Unsubscribe(topic string, sub <-chan interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subscribers[topic]
	for i, ch := range subs {
		if ch == sub {
			newSubs := make([]chan interface{}, 0, len(subs)-1)
			newSubs = append(newSubs, subs[:i]...)
			newSubs = append(newSubs, subs[i+1:]...)
			if len(newSubs) == 0 {
				delete(b.subscribers, topic)
			} else {
				b.subscribers[topic] = newSubs
			}
			close(ch)
			return
		}
	}
}

// Publish sends data to all subscribers of topic.
// It is non-blocking: if a subscriber's buffer is full the event is dropped
// for that subscriber rather than blocking the publishing service.
func (b *Bus) Publish(topic string, data interface{}) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers[topic] {
		select {
		case ch <- data:
		default:
			// Subscriber is slow — drop this tick rather than block.
		}
	}
}

// Close drains and closes all subscriber channels.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for topic, subs := range b.subscribers {
		for _, ch := range subs {
			close(ch)
		}
		delete(b.subscribers, topic)
	}
}
