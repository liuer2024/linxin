package lan

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// VerifyPeer records a human-confirmed fingerprint. Discovery never sets it.
func (s *Service) VerifyPeer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.peers[id]
	if !ok {
		return errors.New("unknown device")
	}
	p.Verified = true
	saved := map[string]bool{}
	for key, value := range s.trusted {
		saved[key] = value
	}
	saved[id] = true
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".trusted-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), filepath.Join(s.dir, "trusted.json")); err != nil {
		return err
	}
	s.trusted = saved
	s.peers[id] = p
	return nil
}
func loadTrust(dir string) (map[string]bool, error) {
	result := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(dir, "trusted.json"))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = map[string]bool{}
	}
	return result, nil
}
