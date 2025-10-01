package blacklist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"UEPB/utils/interfaces"
	"UEPB/utils/logger"

	"github.com/sirupsen/logrus"
)

type Blacklist struct {
	mu      sync.RWMutex
	Phrases [][]string `json:"phrases"`
	file    string
}

// NewBlacklist creates a new blacklist
func NewBlacklist(file string) interfaces.BlacklistInterface {
	// Create data dir
	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logger.Error("Failed to create data directory", err, logrus.Fields{
			"directory": dataDir,
		})
	} else {
		// Get absolute path for logging
		absPath, _ := filepath.Abs(dataDir)
		logger.Info("Data directory ensured", logrus.Fields{
			"path": absPath,
		})
	}

	// Ensure file is in data directory
	if !strings.HasPrefix(file, "data/") {
		file = "data/" + file
	}

	// Get absolute path for logging
	absFile, _ := filepath.Abs(file)
	logger.Info("Blacklist file path", logrus.Fields{
		"path": absFile,
	})

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
	logger.Info("Adding blacklist phrase", logrus.Fields{
		"phrase": lowerWords,
		"file":   b.file,
	})

	if err := b.save(); err != nil {
		logger.Error("Failed to save blacklist after adding phrase", err, logrus.Fields{
			"phrase": lowerWords,
		})
	} else {
		// Verify the phrase was actually saved
		absPath, _ := filepath.Abs(b.file)
		logger.Info("Blacklist phrase successfully saved", logrus.Fields{
			"phrase": lowerWords,
			"path":   absPath,
			"total":  len(b.Phrases),
		})
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
	logger.Info("Attempting to remove blacklist phrase", logrus.Fields{
		"phrase": lowerWords,
		"file":   b.file,
	})

	for i, p := range b.Phrases {
		if strings.Join(p, " ") == target {
			b.Phrases = append(b.Phrases[:i], b.Phrases[i+1:]...)

			if err := b.save(); err != nil {
				logger.Error("Failed to save blacklist after removing phrase", err, logrus.Fields{
					"phrase": lowerWords,
				})
				return false
			}

			absPath, _ := filepath.Abs(b.file)
			logger.Info("Blacklist phrase successfully removed", logrus.Fields{
				"phrase": lowerWords,
				"path":   absPath,
				"total":  len(b.Phrases),
			})
			return true
		}
	}

	logger.Warn("Blacklist phrase not found", logrus.Fields{
		"phrase": lowerWords,
		"file":   b.file,
	})
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
					logger.Info("Blacklisted word detected", logrus.Fields{
						"word":    phrase[0],
						"message": msg,
					})
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
				logger.Info("Blacklisted phrase detected", logrus.Fields{
					"phrase":  strings.Join(phrase, " "),
					"message": msg,
				})
				return true
			}
		}
	}
	return false
}

func (b *Blacklist) List() [][]string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	logger.Info("Listing blacklist phrases", logrus.Fields{
		"file":  b.file,
		"total": len(b.Phrases),
	})
	return append([][]string(nil), b.Phrases...)
}

func (b *Blacklist) save() error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal blacklist: %w", err)
	}

	// Get file info before writing
	absPath, _ := filepath.Abs(b.file)
	logger.Debug("Attempting to write blacklist", logrus.Fields{
		"path": absPath,
	})

	// Check if directory exists
	dir := filepath.Dir(b.file)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		logger.Warn("Directory does not exist, creating it", logrus.Fields{
			"directory": dir,
		})
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(b.file, data, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", b.file, err)
	}

	// Verify file was written successfully
	if stat, err := os.Stat(b.file); err != nil {
		logger.Warn("Could not stat file after writing", logrus.Fields{
			"file": b.file,
		})
	} else {
		logger.Debug("File successfully written", logrus.Fields{
			"path": absPath,
			"size": stat.Size(),
		})
	}

	return nil
}

func (b *Blacklist) load() {
	absPath, _ := filepath.Abs(b.file)
	logger.Info("Loading blacklist", logrus.Fields{
		"path": absPath,
	})

	data, err := os.ReadFile(b.file)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("Blacklist file does not exist, will create new one when needed", logrus.Fields{
				"path": absPath,
			})
			return
		}
		logger.Error("Failed to read blacklist", err, logrus.Fields{
			"path": absPath,
		})
		return
	}

	logger.Debug("Read blacklist file", logrus.Fields{
		"path": absPath,
		"size": len(data),
	})

	if err := json.Unmarshal(data, b); err != nil {
		logger.Error("Failed to unmarshal blacklist", err, logrus.Fields{
			"path": absPath,
		})
		return
	}

	// Initialize empty slice if nil
	if b.Phrases == nil {
		b.Phrases = make([][]string, 0)
	}

	logger.Info("Loaded blacklisted phrases", logrus.Fields{
		"path":  absPath,
		"total": len(b.Phrases),
	})
}
