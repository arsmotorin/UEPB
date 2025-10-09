package bot

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Blacklist stores blocked phrases
type Blacklist struct {
	mu      sync.RWMutex
	Phrases [][]string `json:"phrases"`
	file    string
}

// NewBlacklist creates a blocklist backed by a JSON file in data/
func NewBlacklist(file string) BlacklistInterface {
	dataDir := "data"
	_ = os.MkdirAll(dataDir, 0755)
	if !strings.HasPrefix(file, "data/") {
		file = "data/" + file
	}
	bl := &Blacklist{file: file}
	bl.load()
	return bl
}

// AddPhrase adds a phrase to the blacklist
func (b *Blacklist) AddPhrase(words []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	lower := make([]string, len(words))
	for i, w := range words {
		lower[i] = strings.ToLower(w)
	}
	b.Phrases = append(b.Phrases, lower)
	_ = b.save()
}

// RemovePhrase removes a phrase from the blacklist
func (b *Blacklist) RemovePhrase(words []string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	lower := make([]string, len(words))
	for i, w := range words {
		lower[i] = strings.ToLower(w)
	}
	target := strings.Join(lower, " ")
	for i, p := range b.Phrases {
		if strings.Join(p, " ") == target {
			b.Phrases = append(b.Phrases[:i], b.Phrases[i+1:]...)
			_ = b.save()
			return true
		}
	}
	return false
}

// CheckMessage checks if a message contains any blacklisted phrases
func (b *Blacklist) CheckMessage(msg string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	text := strings.ToLower(msg)
	words := strings.Fields(text)
	for _, phrase := range b.Phrases {
		if len(phrase) == 1 {
			for _, w := range words {
				if w == phrase[0] {
					return true
				}
			}
			continue
		}
		found := true
		for _, pw := range phrase {
			if !strings.Contains(text, pw) {
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

// List returns a copy of the blacklisted phrases
func (b *Blacklist) List() [][]string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([][]string(nil), b.Phrases...)
}

// save persists the blacklist to disk
func (b *Blacklist) save() error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(b.file, data, 0644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// load reads the blacklist from disk
func (b *Blacklist) load() {
	data, err := os.ReadFile(b.file)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, b)
}
