package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
)

type State struct {
	mu          sync.RWMutex
	UserCorrect map[int]int  `json:"user_correct"`
	NewbieMap   map[int]bool `json:"is_newbie"`
	file        string       `json:"-"`
}

func NewState() *State {
	// Create data dir
	os.MkdirAll("data", 0755)

	file := "state.json"
	// Ensure file is in data directory
	if !strings.HasPrefix(file, "data/") {
		file = "data/" + file
	}

	s := &State{
		UserCorrect: make(map[int]int),
		NewbieMap:   make(map[int]bool),
		file:        file,
	}
	s.load()
	return s
}

func (s *State) InitUser(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UserCorrect[id] = 0
	s.save()
}

func (s *State) IncCorrect(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UserCorrect[id]++
	s.save()
}

func (s *State) TotalCorrect(id int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.UserCorrect[id]
}

func (s *State) Reset(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.UserCorrect, id)
	s.save()
}

func (s *State) SetNewbie(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.NewbieMap[id] = true
	s.save()
}

func (s *State) ClearNewbie(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.NewbieMap, id)
	s.save()
}

func (s *State) IsNewbie(id int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.NewbieMap[id]
}

func (s *State) save() {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		log.Printf("Error with serialization state: %v", err)
		return
	}
	if err := os.WriteFile(s.file, data, 0644); err != nil {
		log.Printf("Error with writing state to %s: %v", s.file, err)
	}
}

func (s *State) load() {
	data, err := os.ReadFile(s.file)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("State file %s does not exist, creating new one", s.file)
			return
		}
		log.Printf("Error with reading state from %s: %v", s.file, err)
		return
	}

	// Preserve the file path before unmarshaling
	file := s.file

	if err := json.Unmarshal(data, s); err != nil {
		log.Printf("Error with unmarshalling state from %s: %v", s.file, err)
		return
	}

	// Restore the file path after unmarshaling
	s.file = file

	// Ensure maps are initialized after loading
	if s.UserCorrect == nil {
		s.UserCorrect = make(map[int]int)
	}
	if s.NewbieMap == nil {
		s.NewbieMap = make(map[int]bool)
	}
}
