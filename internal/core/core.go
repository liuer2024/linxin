// Package core contains UI-independent conversation state.
package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

type State string

const (
	Pending   State = "pending"
	Failed    State = "failed"
	Delivered State = "delivered"
	Read      State = "read"
)

type Message struct {
	ID       string    `json:"id"`
	Peer     string    `json:"peer"`
	Text     string    `json:"text"`
	FileID   string    `json:"file_id,omitempty"`
	Incoming bool      `json:"incoming"`
	State    State     `json:"state"`
	Created  time.Time `json:"created"`
}
type Store struct {
	mu       sync.RWMutex
	messages []Message
	seen     map[string]bool
}

func NewStore() *Store { return &Store{seen: make(map[string]bool)} }
func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (s *Store) Add(m Message) (bool, error) {
	if m.ID == "" || m.Peer == "" || len(m.Text) == 0 || len(m.Text) > 65536 {
		return false, errors.New("invalid message")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := m.Peer + "/" + m.ID
	if s.seen[key] {
		return false, nil
	}
	s.seen[key] = true
	s.messages = append(s.messages, m)
	return true, nil
}
func (s *Store) Messages(peer string) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Message{}
	for _, m := range s.messages {
		if m.Peer == peer {
			out = append(out, m)
		}
	}
	return out
}
func (s *Store) Unread(peer string) int {
	n := 0
	for _, m := range s.Messages(peer) {
		if m.Incoming && m.State != Read {
			n++
		}
	}
	return n
}

// MarkRead must only be invoked after the UI has established visibility.
func (s *Store) MarkRead(peer string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := []string{}
	for i := range s.messages {
		m := &s.messages[i]
		if m.Peer == peer && m.Incoming && m.State != Read {
			m.State = Read
			ids = append(ids, m.ID)
		}
	}
	return ids
}
func (s *Store) Receipt(peer string, ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	for i := range s.messages {
		m := &s.messages[i]
		if m.Peer == peer && !m.Incoming && set[m.ID] {
			m.State = Read
		}
	}
}

// MarkReadIDs updates only messages the UI actually displayed.
func (s *Store) MarkReadIDs(peer string, ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	for i := range s.messages {
		m := &s.messages[i]
		if m.Peer == peer && m.Incoming && set[m.ID] {
			m.State = Read
		}
	}
}

func (s *Store) Delivery(peer, id string, state State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages {
		m := &s.messages[i]
		if m.Peer == peer && m.ID == id && !m.Incoming && m.State != Read {
			m.State = state
		}
	}
}

// Conversation is a snapshot for a recent-conversations list. Discovered
// devices with no messages are deliberately absent.
type Conversation struct {
	Peer   string
	Last   Message
	Unread int
}

func (s *Store) Conversations() []Conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	byPeer := map[string]*Conversation{}
	for _, m := range s.messages {
		c := byPeer[m.Peer]
		if c == nil {
			c = &Conversation{Peer: m.Peer}
			byPeer[m.Peer] = c
		}
		if c.Last.Created.IsZero() || !m.Created.Before(c.Last.Created) {
			c.Last = m
		}
		if m.Incoming && m.State != Read {
			c.Unread++
		}
	}
	out := make([]Conversation, 0, len(byPeer))
	for _, c := range byPeer {
		out = append(out, *c)
	}
	return out
}
