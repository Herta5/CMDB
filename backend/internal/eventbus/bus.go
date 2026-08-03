package eventbus

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

type EventType string

const (
	EventCICreated     EventType = "ci.created"
	EventCIUpdated     EventType = "ci.updated"
	EventCIDeleted     EventType = "ci.deleted"
	EventChangeCreated EventType = "change.created"
	EventChangeApproved EventType = "change.approved"
	EventChangeExecuted EventType = "change.executed"
	EventChangeCompleted EventType = "change.completed"
	EventChangeRollback  EventType = "change.rollback"
)

type Event struct {
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Source    string                 `json:"source"`
	Payload   map[string]interface{} `json:"payload"`
}

type Handler func(event Event)

type Bus struct {
	mu       sync.RWMutex
	handlers map[EventType][]Handler
}

var Default = New()

func New() *Bus {
	return &Bus{handlers: make(map[EventType][]Handler)}
}

func (b *Bus) Subscribe(eventType EventType, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	handlers := b.handlers[event.Type]
	b.mu.RUnlock()

	for _, h := range handlers {
		go func(handler Handler) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("event handler panic for %s: %v", event.Type, r)
				}
			}()
			handler(event)
		}(h)
	}
}

func PublishJSON(eventType EventType, source string, payload map[string]interface{}) {
	Default.Publish(Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Source:    source,
		Payload:   payload,
	})
}

func (e Event) Marshal() []byte {
	b, _ := json.Marshal(e)
	return b
}