package coordinator

import (
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"sync"
)

type sessionRegistry struct {
	mu     sync.RWMutex
	values map[identity.SessionID]*sessionmodel.Session
}

func (r *sessionRegistry) get(id identity.SessionID) *sessionmodel.Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.values[id]
}
func (r *sessionRegistry) add(id identity.SessionID, s *sessionmodel.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.values == nil {
		r.values = make(map[identity.SessionID]*sessionmodel.Session)
	}
	r.values[id] = s
}
func (r *sessionRegistry) remove(id identity.SessionID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, id)
}

func (r *sessionRegistry) clear() { r.mu.Lock(); defer r.mu.Unlock(); clear(r.values) }
