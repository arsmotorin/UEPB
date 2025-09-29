package main

import (
	"strings"
	"sync"
)

type Blacklist struct {
	mu      sync.RWMutex
	phrases [][]string
}

func NewBlacklist() *Blacklist {
	return &Blacklist{
		phrases: [][]string{},
	}
}

func (b *Blacklist) AddPhrase(words []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.phrases = append(b.phrases, words)
}

func (b *Blacklist) CheckMessage(msg string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	msgLower := strings.ToLower(msg)
	for _, phrase := range b.phrases {
		found := true
		for _, word := range phrase {
			if !strings.Contains(msgLower, strings.ToLower(word)) {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}
