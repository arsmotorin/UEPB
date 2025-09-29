package main

import "sync"

type State struct {
	mu          sync.RWMutex
	userCorrect map[int]int
	isNewbie    map[int]bool
}

func NewState() *State {
	return &State{
		userCorrect: make(map[int]int),
		isNewbie:    make(map[int]bool),
	}
}

func (s *State) InitUser(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userCorrect[id] = 0
}

func (s *State) IncCorrect(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userCorrect[id]++
}

func (s *State) TotalCorrect(id int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.userCorrect[id]
}

func (s *State) Reset(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.userCorrect, id)
}

func (s *State) SetNewbie(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isNewbie[id] = true
}

func (s *State) ClearNewbie(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.isNewbie, id)
}

func (s *State) IsNewbie(id int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isNewbie[id]
}
