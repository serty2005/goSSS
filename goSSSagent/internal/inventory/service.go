package inventory

import (
	"context"
	"sync"
	"time"
)

type Collector interface {
	Collect(context.Context) (Snapshot, error)
}

type Service struct {
	collector Collector
	interval  time.Duration

	mu       sync.RWMutex
	snapshot Snapshot
	hasValue bool
}

func NewService(interval time.Duration) *Service {
	return &Service{
		collector: newCollector(),
		interval:  interval,
	}
}

func (s *Service) Interval() time.Duration {
	return s.interval
}

func (s *Service) CollectNow(ctx context.Context) (Snapshot, error) {
	snapshot, err := s.collector.Collect(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	s.mu.Lock()
	s.snapshot = snapshot
	s.hasValue = true
	s.mu.Unlock()

	return snapshot, nil
}

func (s *Service) Snapshot() (Snapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.hasValue {
		return Snapshot{}, false
	}
	return s.snapshot, true
}
