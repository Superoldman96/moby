package container

import (
	"context"
	"sync"

	"github.com/containerd/log"
	"github.com/moby/moby/api/types/container"
)

// Health holds the current container health-check state
type Health struct {
	container.Health
	stop chan struct{} // Write struct{} to stop the monitor
	mu   sync.Mutex
}

// String returns a human-readable description of the health-check state
func (s *Health) String() string {
	status := s.Status()

	switch status {
	case container.Starting:
		return "health: starting"
	default: // Healthy and Unhealthy are clear on their own
		return status
	}
}

// Status returns the current health status.
//
// Note that this takes a lock and the value may change after being read.
func (s *Health) Status() container.HealthStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	// This happens when the monitor has yet to be setup.
	if s.Health.Status == "" {
		return container.Unhealthy
	}

	return s.Health.Status
}

// SetStatus writes the current status to the underlying health structure,
// obeying the locking semantics.
//
// Status may be set directly if another lock is used.
func (s *Health) SetStatus(healthStatus container.HealthStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Health.Status = healthStatus
}

// OpenMonitorChannel creates and returns a new monitor channel. If there
// already is one, it returns nil.
func (s *Health) OpenMonitorChannel() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stop == nil {
		log.G(context.TODO()).Debug("OpenMonitorChannel")
		s.stop = make(chan struct{})
		return s.stop
	}
	return nil
}

// CloseMonitorChannel closes any existing monitor channel.
func (s *Health) CloseMonitorChannel() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stop != nil {
		log.G(context.TODO()).Debug("CloseMonitorChannel: waiting for probe to stop")
		close(s.stop)
		s.stop = nil
		// unhealthy when the monitor has stopped for compatibility reasons
		s.Health.Status = container.Unhealthy
		log.G(context.TODO()).Debug("CloseMonitorChannel done")
	}
}
