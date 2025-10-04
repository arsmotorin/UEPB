package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"UEPB/utils/interfaces"
	"UEPB/utils/logger"

	"github.com/sirupsen/logrus"
)

type State struct {
	mu          sync.RWMutex
	UserCorrect map[int]int  `json:"user_correct"`
	NewbieMap   map[int]bool `json:"is_newbie"`
	File        string       `json:"-"`
}

// NewState creates a new State instance
func NewState() interfaces.UserState {
	// Create data dir
	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logger.Error("Failed to create data directory", err, logrus.Fields{
			"directory": dataDir,
		})
	} else {
		absPath, _ := filepath.Abs(dataDir)
		logger.Info("State data directory ensured", logrus.Fields{
			"path": absPath,
		})
	}

	file := "state.json"
	// Ensure file is in data directory
	if !strings.HasPrefix(file, "data/") {
		file = "data/" + file
	}

	absFile, _ := filepath.Abs(file)
	logger.Info("State file path", logrus.Fields{
		"path": absFile,
	})

	s := &State{
		UserCorrect: make(map[int]int),
		NewbieMap:   make(map[int]bool),
		File:        file,
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
		logger.Error("Failed to marshal state", err)
		return
	}

	absPath, _ := filepath.Abs(s.File)
	logger.Debug("Saving state", logrus.Fields{
		"path": absPath,
	})

	if err := os.WriteFile(s.File, data, 0644); err != nil {
		logger.Error("Failed to write state", err, logrus.Fields{
			"path": absPath,
		})
	} else {
		logger.Debug("Successfully saved state", logrus.Fields{
			"path": absPath,
		})
	}
}

func (s *State) load() {
	absPath, _ := filepath.Abs(s.File)
	logger.Info("Loading state", logrus.Fields{
		"path": absPath,
	})

	data, err := os.ReadFile(s.File)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("State file does not exist, will create when needed", logrus.Fields{
				"path": absPath,
			})
			return
		}
		logger.Error("Failed to read state", err, logrus.Fields{
			"path": absPath,
		})
		return
	}

	logger.Debug("Read state file", logrus.Fields{
		"path": absPath,
		"size": len(data),
	})

	// Preserve the file path before unmarshalling
	file := s.File

	if err := json.Unmarshal(data, s); err != nil {
		logger.Error("Failed to unmarshal state", err, logrus.Fields{
			"path": absPath,
		})
		// Reset to default values on error
		s.UserCorrect = make(map[int]int)
		s.NewbieMap = make(map[int]bool)
	}

	// Restore the file path after unmarshalling
	s.File = file

	// Initialize maps if nil
	if s.UserCorrect == nil {
		s.UserCorrect = make(map[int]int)
	}
	if s.NewbieMap == nil {
		s.NewbieMap = make(map[int]bool)
	}

	logger.Info("Loaded state", logrus.Fields{
		"path":    absPath,
		"users":   len(s.UserCorrect),
		"newbies": len(s.NewbieMap),
	})
}
