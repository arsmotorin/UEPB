package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
)

type Blacklist struct {
	mu      sync.RWMutex
	Phrases [][]string `json:"phrases"`
	file    string
}

func NewBlacklist(file string) *Blacklist {
	// Create data dir
	os.MkdirAll("data", 0755)

	bl := &Blacklist{file: file}
	bl.load()
	return bl
}

func (b *Blacklist) AddPhrase(words []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Phrases = append(b.Phrases, words)
	b.save()
}

func (b *Blacklist) RemovePhrase(words []string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	target := strings.Join(words, " ")

	for i, p := range b.Phrases {
		if strings.Join(p, " ") == target {
			b.Phrases = append(b.Phrases[:i], b.Phrases[i+1:]...)
			b.save()
			return true
		}
	}
	return false
}

func (b *Blacklist) CheckMessage(msg string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	text := strings.ToLower(msg)
	words := strings.Fields(text)

	for _, phrase := range b.Phrases {
		if len(phrase) == 1 {
			// One word
			for _, w := range words {
				if w == phrase[0] {
					return true
				}
			}
		} else {
			// Several words
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
	}
	return false
}

func (b *Blacklist) List() [][]string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([][]string(nil), b.Phrases...)
}

func (b *Blacklist) save() {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		log.Println("Error with serialization blacklist:", err)
		return
	}
	if err := os.WriteFile(b.file, data, 0644); err != nil {
		log.Println("Error with writing in blacklist:", err)
	}
}

func (b *Blacklist) load() {
	data, err := os.ReadFile(b.file)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Println("Eror with reading blacklist:", err)
		return
	}
	if err := json.Unmarshal(data, b); err != nil {
		log.Println("Error with unmarshalling blacklist:", err)
	}
}
