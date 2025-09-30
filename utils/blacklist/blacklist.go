package blacklist

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"UEPB/utils/interfaces"
)

type Blacklist struct {
	mu      sync.RWMutex
	Phrases [][]string `json:"phrases"`
	file    string
}

// NewBlacklist creates a new blacklist
func NewBlacklist(file string) interfaces.BlacklistInterface {
	// Create data dir with explicit logging
	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Printf("[ERROR] Failed to create data directory %s: %v", dataDir, err)
	} else {
		// Get absolute path for logging
		absPath, _ := filepath.Abs(dataDir)
		log.Printf("[INFO] Data directory ensured: %s", absPath)
	}

	// Ensure file is in data directory
	if !strings.HasPrefix(file, "data/") {
		file = "data/" + file
	}

	// Get absolute path for logging
	absFile, _ := filepath.Abs(file)
	log.Printf("[INFO] Blacklist file path: %s", absFile)

	bl := &Blacklist{file: file}
	bl.load()
	return bl
}

func (b *Blacklist) AddPhrase(words []string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Convert to lowercase for consistency
	lowerWords := make([]string, len(words))
	for i, word := range words {
		lowerWords[i] = strings.ToLower(word)
	}

	b.Phrases = append(b.Phrases, lowerWords)
	log.Printf("[INFO] Adding blacklist phrase: %v to file: %s", lowerWords, b.file)

	if err := b.save(); err != nil {
		log.Printf("[ERROR] Failed to save blacklist after adding phrase %v: %v", lowerWords, err)
	} else {
		// Verify the phrase was actually saved
		absPath, _ := filepath.Abs(b.file)
		log.Printf("[SUCCESS] Blacklist phrase %v successfully saved to: %s", lowerWords, absPath)

		// Log current total phrases count
		log.Printf("[INFO] Total blacklisted phrases: %d", len(b.Phrases))
	}
}

func (b *Blacklist) RemovePhrase(words []string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Convert to lowercase for consistency
	lowerWords := make([]string, len(words))
	for i, word := range words {
		lowerWords[i] = strings.ToLower(word)
	}

	target := strings.Join(lowerWords, " ")
	log.Printf("[INFO] Attempting to remove blacklist phrase: %v from file: %s", lowerWords, b.file)

	for i, p := range b.Phrases {
		if strings.Join(p, " ") == target {
			b.Phrases = append(b.Phrases[:i], b.Phrases[i+1:]...)

			if err := b.save(); err != nil {
				log.Printf("[ERROR] Failed to save blacklist after removing phrase %v: %v", lowerWords, err)
				return false
			}

			absPath, _ := filepath.Abs(b.file)
			log.Printf("[SUCCESS] Blacklist phrase %v successfully removed from: %s", lowerWords, absPath)
			log.Printf("[INFO] Total blacklisted phrases: %d", len(b.Phrases))
			return true
		}
	}

	log.Printf("[WARNING] Blacklist phrase %v not found in file: %s", lowerWords, b.file)
	return false
}

func (b *Blacklist) CheckMessage(msg string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	text := strings.ToLower(msg)
	words := strings.Fields(text)

	for _, phrase := range b.Phrases {
		if len(phrase) == 1 {
			// One word anywhere in the message
			for _, w := range words {
				if w == phrase[0] {
					log.Printf("[DETECTION] Blacklisted word detected: '%s' in message: '%s'", phrase[0], msg)
					return true
				}
			}
		} else {
			// Several words, but neighboring words are allowed to be different
			found := true
			for _, pw := range phrase {
				if !strings.Contains(text, pw) {
					found = false
					break
				}
			}
			if found {
				log.Printf("[DETECTION] Blacklisted phrase detected: '%s' in message: '%s'", strings.Join(phrase, " "), msg)
				return true
			}
		}
	}
	return false
}

func (b *Blacklist) List() [][]string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	log.Printf("[INFO] Listing blacklist phrases from file: %s (total: %d)", b.file, len(b.Phrases))
	return append([][]string(nil), b.Phrases...)
}

func (b *Blacklist) save() error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal blacklist: %w", err)
	}

	// Get file info before writing
	absPath, _ := filepath.Abs(b.file)
	log.Printf("[DEBUG] Attempting to write blacklist to: %s", absPath)

	// Check if directory exists
	dir := filepath.Dir(b.file)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		log.Printf("[WARNING] Directory %s does not exist, creating it", dir)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(b.file, data, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", b.file, err)
	}

	// Verify file was written successfully
	if stat, err := os.Stat(b.file); err != nil {
		log.Printf("[WARNING] Could not stat file after writing: %s", b.file)
	} else {
		log.Printf("[DEBUG] File successfully written: %s (size: %d bytes)", absPath, stat.Size())
	}

	return nil
}

func (b *Blacklist) load() {
	absPath, _ := filepath.Abs(b.file)
	log.Printf("[INFO] Loading blacklist from: %s", absPath)

	data, err := os.ReadFile(b.file)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[INFO] Blacklist file %s does not exist, will create new one when needed", absPath)
			return
		}
		log.Printf("[ERROR] Failed to read blacklist from %s: %v", absPath, err)
		return
	}

	log.Printf("[DEBUG] Read %d bytes from blacklist file: %s", len(data), absPath)

	if err := json.Unmarshal(data, b); err != nil {
		log.Printf("[ERROR] Failed to unmarshal blacklist from %s: %v", absPath, err)
		return
	}

	// Initialize empty slice if nil
	if b.Phrases == nil {
		b.Phrases = make([][]string, 0)
	}

	log.Printf("[SUCCESS] Loaded %d blacklisted phrases from: %s", len(b.Phrases), absPath)
}
