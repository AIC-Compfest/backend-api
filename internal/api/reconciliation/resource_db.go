package reconciliation

import (
	"fmt"
	"sync"

	"backend-api/internal/model"
)

// Repository abstracts the storage layer for reconciliation events and audit logs.
type Repository interface {
	Save(event *model.ShipmentEvent) error
	GetByID(id string) (*model.ShipmentEvent, error)
	ListAll() ([]*model.ShipmentEvent, error)
	Update(event *model.ShipmentEvent) error
}

// MemoryRepository implements in-memory concurrency-safe persistence for the competition MVP.
type MemoryRepository struct {
	mu     sync.RWMutex
	events map[string]*model.ShipmentEvent
}

// NewMemoryRepository creates a new in-memory repository instance.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		events: make(map[string]*model.ShipmentEvent),
	}
}

// Save stores a new ShipmentEvent into the repository.
func (r *MemoryRepository) Save(event *model.ShipmentEvent) error {
	if event == nil || event.EventID == "" {
		return fmt.Errorf("invalid shipment event payload")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[event.EventID] = event
	return nil
}

// GetByID finds a ShipmentEvent by EventID, ShipmentID, or InvoiceNumber.
func (r *MemoryRepository) GetByID(id string) (*model.ShipmentEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if ev, exists := r.events[id]; exists {
		return ev, nil
	}

	for _, ev := range r.events {
		if ev.ShipmentID == id || ev.Invoice.InvoiceNumber == id {
			return ev, nil
		}
	}

	return nil, fmt.Errorf("reconciliation event '%s' not found", id)
}

// ListAll returns all stored ShipmentEvents.
func (r *MemoryRepository) ListAll() ([]*model.ShipmentEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]*model.ShipmentEvent, 0, len(r.events))
	for _, ev := range r.events {
		list = append(list, ev)
	}
	return list, nil
}

// Update updates an existing ShipmentEvent.
func (r *MemoryRepository) Update(event *model.ShipmentEvent) error {
	if event == nil || event.EventID == "" {
		return fmt.Errorf("invalid shipment event payload")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.events[event.EventID]; !exists {
		return fmt.Errorf("cannot update non-existent event '%s'", event.EventID)
	}

	r.events[event.EventID] = event
	return nil
}
