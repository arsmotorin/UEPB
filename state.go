package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
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
	// Create data dir with logging
	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Printf("[ERROR] Failed to create data directory %s: %v", dataDir, err)
	} else {
		absPath, _ := filepath.Abs(dataDir)
		log.Printf("[INFO] State data directory ensured: %s", absPath)
	}

	file := "state.json"
	// Ensure file is in data directory
	if !strings.HasPrefix(file, "data/") {
		file = "data/" + file
	}

	absFile, _ := filepath.Abs(file)
	log.Printf("[INFO] State file path: %s", absFile)

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
		log.Printf("[ERROR] Failed to marshal state: %v", err)
		return
	}

	absPath, _ := filepath.Abs(s.file)
	log.Printf("[DEBUG] Saving state to: %s", absPath)

	if err := os.WriteFile(s.file, data, 0644); err != nil {
		log.Printf("[ERROR] Failed to write state to %s: %v", absPath, err)
	} else {
		log.Printf("[DEBUG] Successfully saved state to: %s", absPath)
	}
}

func (s *State) load() {
	absPath, _ := filepath.Abs(s.file)
	log.Printf("[INFO] Loading state from: %s", absPath)

	data, err := os.ReadFile(s.file)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[INFO] State file %s does not exist, will create when needed", absPath)
			return
		}
		log.Printf("[ERROR] Failed to read state from %s: %v", absPath, err)
		return
	}

	log.Printf("[DEBUG] Read %d bytes from state file: %s", len(data), absPath)

	// Preserve the file path before unmarshaling
	file := s.file

	if err := json.Unmarshal(data, s); err != nil {
		log.Printf("[ERROR] Failed to unmarshal state from %s: %v", absPath, err)
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

	log.Printf("[SUCCESS] Loaded state from: %s (users: %d, newbies: %d)",
		absPath, len(s.UserCorrect), len(s.NewbieMap))
}
