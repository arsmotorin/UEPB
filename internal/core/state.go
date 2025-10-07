package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// State holds user quiz results and newbie flags
type State struct {
	mu          sync.RWMutex
	UserCorrect map[int]int  `json:"user_correct"`
	NewbieMap   map[int]bool `json:"is_newbie"`
	File        string       `json:"-"`
}

// NewState allocates a new State and loads persisted data
func NewState() UserState {
	const dataDir = "data"
	_ = os.MkdirAll(dataDir, 0755)
	file := "state.json"
	if !strings.HasPrefix(file, "data/") {
		file = "data/" + file
	}
	s := &State{UserCorrect: make(map[int]int), NewbieMap: make(map[int]bool), File: file}
	s.load()
	return s
}

func (s *State) InitUser(id int)   { s.mu.Lock(); s.UserCorrect[id] = 0; s.mu.Unlock(); s.save() }
func (s *State) IncCorrect(id int) { s.mu.Lock(); s.UserCorrect[id]++; s.mu.Unlock(); s.save() }
func (s *State) TotalCorrect(id int) int {
	s.mu.RLock()
	v := s.UserCorrect[id]
	s.mu.RUnlock()
	return v
}
func (s *State) Reset(id int)         { s.mu.Lock(); delete(s.UserCorrect, id); s.mu.Unlock(); s.save() }
func (s *State) SetNewbie(id int)     { s.mu.Lock(); s.NewbieMap[id] = true; s.mu.Unlock(); s.save() }
func (s *State) ClearNewbie(id int)   { s.mu.Lock(); delete(s.NewbieMap, id); s.mu.Unlock(); s.save() }
func (s *State) IsNewbie(id int) bool { s.mu.RLock(); v := s.NewbieMap[id]; s.mu.RUnlock(); return v }

func (s *State) save() {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		logrus.WithError(err).Error("state marshal")
		return
	}
	abs, _ := filepath.Abs(s.File)
	_ = os.WriteFile(s.File, data, 0644)
	logrus.WithField("path", abs).Debug("state saved")
}

func (s *State) load() {
	data, err := os.ReadFile(s.File)
	if err != nil {
		return
	}
	file := s.File
	_ = json.Unmarshal(data, s)
	s.File = file
	if s.UserCorrect == nil {
		s.UserCorrect = make(map[int]int)
	}
	if s.NewbieMap == nil {
		s.NewbieMap = make(map[int]bool)
	}
}
