package workbench

import (
	"errors"
	"strings"
	"time"
)

// queued is a follow-up typed while a turn was running. The workbench owns the
// queue so both engines behave the same and other browsers see it.
type queued struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

const queueLimit = 20

func (m *Manager) emitQueue(r *runtime) {
	items := append([]queued{}, r.queue...)
	m.emit(r.session.ID, "queue", map[string]any{"items": items, "paused": r.queuePaused})
}

// Enqueue sends immediately when the session can take a turn; otherwise the
// message waits and is sent when the current turn completes. It reports
// whether the message was queued.
func (m *Manager) Enqueue(id, text string) (bool, error) {
	if strings.TrimSpace(text) == "" || len(text) > 1024*1024 {
		return false, errors.New("message must contain 1 to 1048576 bytes")
	}
	m.mu.Lock()
	r := m.find(id)
	busy := r != nil && r.proc != nil && (r.turn || r.configuring || len(r.pending) > 0 || (len(r.queue) > 0 && !r.queuePaused))
	if !busy {
		m.mu.Unlock()
		return false, m.Send(id, text)
	}
	defer m.mu.Unlock()
	if len(r.queue) >= queueLimit {
		return false, errors.New("queue is full")
	}
	r.queue = append(r.queue, queued{ID: token()[:12], Text: text})
	m.emitQueue(r)
	return true, nil
}

// turnEnded runs with m.mu held after a turn settles. Interrupted or failed
// turns pause the queue instead of sending the next message.
func (m *Manager) turnEnded(r *runtime, failed bool) {
	// What the turn left running, once its own processes have exited.
	id := r.session.ID
	time.AfterFunc(time.Second, func() { m.refreshAllOthers(id) })
	interrupted := r.interrupted
	r.interrupted = false
	if len(r.queue) == 0 {
		return
	}
	if failed || interrupted {
		if !r.queuePaused {
			r.queuePaused = true
			m.emitQueue(r)
		}
		return
	}
	if !r.queuePaused {
		m.dispatchQueued(r)
	}
}

func (m *Manager) dispatchQueued(r *runtime) {
	next := r.queue[0]
	r.queue = r.queue[1:]
	m.emitQueue(r)
	id := r.session.ID
	go func() {
		if err := m.Send(id, next.Text); err != nil {
			m.mu.Lock()
			defer m.mu.Unlock()
			if rt := m.find(id); rt != nil {
				rt.queue = append([]queued{next}, rt.queue...)
				rt.queuePaused = true
				m.emit(rt.session.ID, "error", map[string]any{"message": "Queued message not sent: " + err.Error()})
				m.emitQueue(rt)
			}
		}
	}()
}

func (m *Manager) RemoveQueued(id, qid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(id)
	if r == nil {
		return errors.New("session not found")
	}
	for i, q := range r.queue {
		if q.ID == qid {
			r.queue = append(r.queue[:i], r.queue[i+1:]...)
			if len(r.queue) == 0 {
				r.queuePaused = false
			}
			m.emitQueue(r)
			return nil
		}
	}
	return errors.New("queued message not found")
}

// ResumeQueue unpauses the queue and sends the next message if the session is free.
func (m *Manager) ResumeQueue(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(id)
	if r == nil {
		return errors.New("session not found")
	}
	r.queuePaused = false
	if len(r.queue) == 0 {
		m.emitQueue(r)
		return nil
	}
	if !r.turn && !r.configuring && len(r.pending) == 0 {
		m.dispatchQueued(r)
		return nil
	}
	m.emitQueue(r)
	return nil
}
