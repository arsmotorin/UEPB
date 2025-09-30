package admin

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"UEPB/interfaces"

	tb "gopkg.in/telebot.v4"
)

// AdminHandler handles all admin-related functionality
type AdminHandler struct {
	bot            *tb.Bot
	blacklist      interfaces.BlacklistInterface
	adminChatID    int64
	violations     map[int64]int
	violationsMu   sync.RWMutex
	violationsFile string
	groupIDs       map[int64]struct{}
	groupMu        sync.RWMutex
}

// NewAdminHandler creates a new admin handler
func NewAdminHandler(bot *tb.Bot, blacklist interfaces.BlacklistInterface, adminChatID int64, violations map[int64]int) *AdminHandler {
	// Create data dir with logging
	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Printf("[ERROR] Failed to create data directory %s: %v", dataDir, err)
	} else {
		absPath, _ := filepath.Abs(dataDir)
		log.Printf("[INFO] Admin data directory ensured: %s", absPath)
	}

	file := "data/violations.json"
	absFile, _ := filepath.Abs(file)
	log.Printf("[INFO] Admin violations file path: %s", absFile)

	ah := &AdminHandler{
		bot:            bot,
		blacklist:      blacklist,
		adminChatID:    adminChatID,
		violations:     violations,
		violationsFile: file,
		groupIDs:       make(map[int64]struct{}),
	}

	ah.loadViolations()
	return ah
}

// LogToAdmin sends a message to the admin chat
func (ah *AdminHandler) LogToAdmin(message string) {
	adminChat := &tb.Chat{ID: ah.adminChatID}
	if _, err := ah.bot.Send(adminChat, message); err != nil {
		log.Printf("[ERROR] Failed to send admin log: %v", err)
	}
}

// IsAdmin checks if the user is admin in the chat
func (ah *AdminHandler) IsAdmin(chat *tb.Chat, user *tb.User) bool {
	member, err := ah.bot.ChatMemberOf(chat, user)
	if err != nil {
		log.Printf("[ERROR] Failed to check member rights: %v", err)
		return false
	}
	return member.Role == tb.Administrator || member.Role == tb.Creator
}

// GetUserDisplayName returns user display name
func (ah *AdminHandler) GetUserDisplayName(user *tb.User) string {
	if user.Username != "" {
		return "@" + user.Username
	}
	name := user.FirstName
	if user.LastName != "" {
		name += " " + user.LastName
	}
	return name + fmt.Sprintf(" (ID: %d)", user.ID)
}

// DeleteAfter deletes a message after specified duration
func (ah *AdminHandler) DeleteAfter(m *tb.Message, d time.Duration) {
	if m == nil {
		return
	}
	go func() {
		time.Sleep(d)
		if err := ah.bot.Delete(m); err != nil {
			log.Printf("[ERROR] Failed to delete message %d: %v", m.ID, err)
		}
	}()
}

// BanUser bans a user from the chat
func (ah *AdminHandler) BanUser(chat *tb.Chat, user *tb.User) error {
	member := &tb.ChatMember{
		User:   user,
		Rights: tb.Rights{},
	}
	return ah.bot.Ban(chat, member)
}

// HandleBan handles /banword command
func (ah *AdminHandler) HandleBan(c tb.Context) error {
	log.Printf("[DEBUG] /banword command received from user %d", c.Sender().ID)

	if c.Message() == nil || c.Sender() == nil {
		return nil
	}

	if !ah.IsAdmin(c.Chat(), c.Sender()) {
		msg, _ := ah.bot.Send(c.Chat(), "⛔ Команда /banword доступна только администрации.")
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}

	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		msg, _ := ah.bot.Send(c.Chat(), "💡 Используй: /banword слово1 [слово2 ...]")
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}

	// Log before attempting to add
	log.Printf("[INFO] Admin %s attempting to add blacklist phrase: %v in chat %s (ID: %d)",
		ah.GetUserDisplayName(c.Sender()), args[1:], c.Chat().Title, c.Chat().ID)

	ah.blacklist.AddPhrase(args[1:])

	// Log after adding
	log.Printf("[SUCCESS] Admin successfully executed AddPhrase for: %v", args[1:])

	msg, _ := ah.bot.Send(c.Chat(), "✅ Добавлено запрещённое словосочетание: "+strings.Join(args[1:], " "))
	ah.DeleteAfter(msg, 10*time.Second)

	// Log to admin chat
	logMsg := fmt.Sprintf("🚫 Добавлено запрещённое слово\n\n"+
		"Админ: %s\n"+
		"Запрещённые слова: `%s`\n"+
		"Чат: %s (ID: %d)",
		ah.GetUserDisplayName(c.Sender()),
		strings.Join(args[1:], " "),
		c.Chat().Title,
		c.Chat().ID)
	ah.LogToAdmin(logMsg)

	return nil
}

// HandleUnban handles /unbanword command
func (ah *AdminHandler) HandleUnban(c tb.Context) error {
	log.Printf("[DEBUG] /unbanword command received from user %d", c.Sender().ID)

	if c.Message() == nil || c.Sender() == nil {
		return nil
	}

	if !ah.IsAdmin(c.Chat(), c.Sender()) {
		msg, _ := ah.bot.Send(c.Chat(), "⛔ Команда /unbanword доступна только администрации.")
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}

	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		msg, _ := ah.bot.Send(c.Chat(), "💡 Используй: /unbanword слово1 [слово2 ...]")
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}

	// Log before attempting to remove
	log.Printf("[INFO] Admin %s attempting to remove blacklist phrase: %v in chat %s (ID: %d)",
		ah.GetUserDisplayName(c.Sender()), args[1:], c.Chat().Title, c.Chat().ID)

	ok := ah.blacklist.RemovePhrase(args[1:])
	var text string
	if ok {
		text = "✅ Удалено запрещённое словосочетание: " + strings.Join(args[1:], " ")
		log.Printf("[SUCCESS] Admin successfully removed blacklist phrase: %v", args[1:])

		// Log to admin chat
		logMsg := fmt.Sprintf("✅ Удалено запрещённое слово\n\n"+
			"Админ: %s\n"+
			"Удалённые слова: `%s`\n"+
			"Чат: %s (ID: %d)",
			ah.GetUserDisplayName(c.Sender()),
			strings.Join(args[1:], " "),
			c.Chat().Title,
			c.Chat().ID)
		ah.LogToAdmin(logMsg)
	} else {
		text = "❌ Такого словосочетания нет в списке."
		log.Printf("[WARNING] Admin tried to remove non-existent blacklist phrase: %v", args[1:])
	}
	msg, _ := ah.bot.Send(c.Chat(), text)
	ah.DeleteAfter(msg, 10*time.Second)
	return nil
}

// HandleListBan handles /listbanword command
func (ah *AdminHandler) HandleListBan(c tb.Context) error {
	// Allow command only from admins or in admin chat
	if c.Chat().Type == tb.ChatPrivate {
		// In private chat, only allow admin chat
		if c.Chat().ID != ah.adminChatID {
			return nil
		}
	} else {
		// In group chat, only allow admins
		if !ah.IsAdmin(c.Chat(), c.Sender()) {
			msg, _ := ah.bot.Send(c.Chat(), "⛔ Команда /listbanword доступна только администрации.")
			ah.DeleteAfter(msg, 10*time.Second)
			return nil
		}
	}

	log.Printf("[INFO] Admin %s requested blacklist in chat %s (ID: %d)",
		ah.GetUserDisplayName(c.Sender()), c.Chat().Title, c.Chat().ID)

	phrases := ah.blacklist.List()
	log.Printf("[INFO] Retrieved %d blacklist phrases for display", len(phrases))

	if len(phrases) == 0 {
		ah.bot.Send(c.Chat(), "📭 Список пуст.")
		return nil
	}

	var sb strings.Builder
	sb.WriteString("🚫 Запрещённые словосочетания:\n\n")
	for i, p := range phrases {
		sb.WriteString(fmt.Sprintf("%d. `%s`\n", i+1, strings.Join(p, " ")))
	}

	ah.bot.Send(c.Chat(), sb.String(), tb.ModeMarkdown)
	return nil
}

// RegisterGroup registers a group chat ID
func (ah *AdminHandler) RegisterGroup(chat *tb.Chat) {
	if chat == nil || chat.Type == tb.ChatPrivate {
		return
	}
	ah.groupMu.Lock()
	defer ah.groupMu.Unlock()
	ah.groupIDs[chat.ID] = struct{}{}
}

// AllGroupIDs returns all registered group chat IDs
func (ah *AdminHandler) AllGroupIDs() []int64 {
	ah.groupMu.RLock()
	defer ah.groupMu.RUnlock()
	ids := make([]int64, 0, len(ah.groupIDs))
	for id := range ah.groupIDs {
		ids = append(ids, id)
	}
	return ids
}

// BanUserEverywhere bans a user from all group chats
func (ah *AdminHandler) BanUserEverywhere(user *tb.User) {
	for _, chatID := range ah.AllGroupIDs() {
		chat := &tb.Chat{ID: chatID}
		_ = ah.BanUser(chat, user)
	}
}

// HandleSpamBan handles /spamban command
func (ah *AdminHandler) HandleSpamBan(c tb.Context) error {
	if c.Message() == nil || c.Sender() == nil {
		return nil
	}

	if !ah.IsAdmin(c.Chat(), c.Sender()) {
		msg, _ := ah.bot.Send(c.Chat(), "⛔ Команда /spamban доступна только администрации.")
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}

	var targetUser *tb.User

	// Check if the command is a reply
	if c.Message().ReplyTo != nil && c.Message().ReplyTo.Sender != nil {
		targetUser = c.Message().ReplyTo.Sender
	} else {
		args := strings.Fields(c.Message().Text)
		if len(args) < 2 {
			msg, _ := ah.bot.Send(c.Chat(), "💡 Используй: /spamban в ответ на сообщение или /spamban айди/юзернейм")
			ah.DeleteAfter(msg, 10*time.Second)
			return nil
		}
		identifier := args[1]
		if strings.HasPrefix(identifier, "@") {
			// Username
			user, err := ah.bot.ChatMemberOf(c.Chat(), &tb.User{Username: identifier[1:]})
			if err != nil || user.User == nil {
				msg, _ := ah.bot.Send(c.Chat(), "❌ Не удалось найти пользователя по username.")
				ah.DeleteAfter(msg, 10*time.Second)
				return nil
			}
			targetUser = user.User
		} else {
			// ID
			id, err := strconv.ParseInt(identifier, 10, 64)
			if err != nil {
				msg, _ := ah.bot.Send(c.Chat(), "❌ Неверный формат ID.")
				ah.DeleteAfter(msg, 10*time.Second)
				return nil
			}
			user, err := ah.bot.ChatMemberOf(c.Chat(), &tb.User{ID: id})
			if err != nil || user.User == nil {
				msg, _ := ah.bot.Send(c.Chat(), "❌ Не удалось найти пользователя по ID.")
				ah.DeleteAfter(msg, 10*time.Second)
				return nil
			}
			targetUser = user.User
		}
	}

	if targetUser == nil {
		msg, _ := ah.bot.Send(c.Chat(), "❌ Не удалось определить пользователя для бана.")
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}

	if ah.IsAdmin(c.Chat(), targetUser) {
		msg, _ := ah.bot.Send(c.Chat(), "⛔ Нельзя забанить администратора.")
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}

	ah.BanUserEverywhere(targetUser)
	ah.ClearViolations(targetUser.ID)

	msg, _ := ah.bot.Send(c.Chat(), fmt.Sprintf("🔨 Пользователь %s забанен за спам.", ah.GetUserDisplayName(targetUser)))
	ah.DeleteAfter(msg, 10*time.Second)

	// Log to admin chat
	logMsg := fmt.Sprintf("🔨 Пользователь забанен за спам.\n\n"+
		"Забанен: %s\n"+
		"Админ: %s",
		ah.GetUserDisplayName(targetUser),
		ah.GetUserDisplayName(c.Sender()))
	ah.LogToAdmin(logMsg)

	return nil
}

// AddViolation adds a violation for a user
func (ah *AdminHandler) AddViolation(userID int64) {
	ah.violationsMu.Lock()
	defer ah.violationsMu.Unlock()
	ah.violations[userID]++
	ah.saveViolations()
}

// GetViolations gets violation count for a user
func (ah *AdminHandler) GetViolations(userID int64) int {
	ah.violationsMu.RLock()
	defer ah.violationsMu.RUnlock()
	return ah.violations[userID]
}

// ClearViolations clears violations for a user
func (ah *AdminHandler) ClearViolations(userID int64) {
	ah.violationsMu.Lock()
	defer ah.violationsMu.Unlock()
	delete(ah.violations, userID)
	ah.saveViolations()
}

func (ah *AdminHandler) saveViolations() {
	data, err := json.MarshalIndent(ah.violations, "", "  ")
	if err != nil {
		log.Printf("[ERROR] Failed to marshal violations: %v", err)
		return
	}

	absPath, _ := filepath.Abs(ah.violationsFile)
	log.Printf("[DEBUG] Saving violations to: %s", absPath)

	if err := os.WriteFile(ah.violationsFile, data, 0644); err != nil {
		log.Printf("[ERROR] Failed to write violations to %s: %v", absPath, err)
	} else {
		log.Printf("[DEBUG] Successfully saved violations to: %s", absPath)
	}
}

func (ah *AdminHandler) loadViolations() {
	absPath, _ := filepath.Abs(ah.violationsFile)
	log.Printf("[INFO] Loading violations from: %s", absPath)

	data, err := os.ReadFile(ah.violationsFile)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[INFO] Violations file %s does not exist, will create when needed", absPath)
			return
		}
		log.Printf("[ERROR] Failed to read violations from %s: %v", absPath, err)
		return
	}

	log.Printf("[DEBUG] Read %d bytes from violations file: %s", len(data), absPath)

	ah.violationsMu.Lock()
	defer ah.violationsMu.Unlock()

	if err := json.Unmarshal(data, &ah.violations); err != nil {
		log.Printf("[ERROR] Failed to unmarshal violations from %s: %v", absPath, err)
		return
	}

	if ah.violations == nil {
		ah.violations = make(map[int64]int)
	}

	log.Printf("[SUCCESS] Loaded %d violation records from: %s", len(ah.violations), absPath)
}
