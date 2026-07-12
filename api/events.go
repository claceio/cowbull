package api

import (
	"sync"
)

// Event is a notification sent to players connected to a challenge.
type Event struct {
	Name   string // SSE event name, e.g. "activity"
	Data   string // message text
	Origin string // player id that caused the event, "" for system events
}

// Broker is a simple in-memory pub/sub used for SSE notifications.
// Topics are challenge ids.
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: map[string]map[chan Event]struct{}{}}
}

// Subscribe returns a channel of events for the topic and an unsubscribe function.
func (b *Broker) Subscribe(topic string) (chan Event, func()) {
	ch := make(chan Event, 16)
	b.mu.Lock()
	if b.subs[topic] == nil {
		b.subs[topic] = map[chan Event]struct{}{}
	}
	b.subs[topic][ch] = struct{}{}
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		delete(b.subs[topic], ch)
		if len(b.subs[topic]) == 0 {
			delete(b.subs, topic)
		}
		b.mu.Unlock()
	}
}

func (b *Broker) Publish(topic string, ev Event) {
	b.mu.Lock()
	for ch := range b.subs[topic] {
		select {
		case ch <- ev:
		default: // slow subscriber, drop
		}
	}
	b.mu.Unlock()
}
