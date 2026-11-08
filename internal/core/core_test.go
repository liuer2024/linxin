package core

import "testing"

func TestConversation(t *testing.T) {
	s := NewStore()
	m := Message{ID: "1", Peer: "a", Text: "你好", Incoming: true, State: Delivered}
	if ok, e := s.Add(m); !ok || e != nil {
		t.Fatal(ok, e)
	}
	if ok, _ := s.Add(m); ok {
		t.Fatal("duplicate accepted")
	}
	if s.Unread("a") != 1 || s.Unread("b") != 0 {
		t.Fatal("unread isolation")
	}
	ids := s.MarkRead("a")
	if len(ids) != 1 || s.Unread("a") != 0 {
		t.Fatal("read failed")
	}
	s.Add(Message{ID: "2", Peer: "a", Text: "hello", State: Delivered})
	s.Receipt("b", []string{"2"})
	if s.Messages("a")[1].State != Delivered {
		t.Fatal("cross peer receipt")
	}
	s.Receipt("a", []string{"2"})
	if s.Messages("a")[1].State != Read {
		t.Fatal("receipt failed")
	}
}

func TestConversationSummaryIsolation(t *testing.T) {
	s := NewStore()
	s.Add(Message{ID: "a1", Peer: "a", Text: "first", Incoming: true, State: Delivered})
	s.Add(Message{ID: "b1", Peer: "b", Text: "second", Incoming: true, State: Delivered})
	s.Add(Message{ID: "a2", Peer: "a", Text: "reply", State: Delivered})
	s.MarkReadIDs("a", []string{"a1"})
	summaries := s.Conversations()
	if len(summaries) != 2 {
		t.Fatal("missing conversation")
	}
	for _, c := range summaries {
		switch c.Peer {
		case "a":
			if c.Unread != 0 || c.Last.ID != "a2" {
				t.Fatal(c)
			}
		case "b":
			if c.Unread != 1 {
				t.Fatal("reading a cleared b", c)
			}
		}
	}
}
