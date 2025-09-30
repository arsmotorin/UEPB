package features

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"UEPB/utils/admin"
	"UEPB/utils/interfaces"

	tb "gopkg.in/telebot.v4"
)

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
	dataDir := "data"
	_ = os.MkdirAll(dataDir, 0755)
	file := "data/violations.json"
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

// LogToAdmin logs a message to the admin chat
func (ah *AdminHandler) LogToAdmin(message string) {
	adminChat := &tb.Chat{ID: ah.adminChatID}
	if _, err := ah.bot.Send(adminChat, message); err != nil {
		slog.Error("Failed to send admin log", "err", err)
	}
}

// IsAdmin checks if a user is an admin
func (ah *AdminHandler) IsAdmin(chat *tb.Chat, user *tb.User) bool {
	member, err := ah.bot.ChatMemberOf(chat, user)
	if err != nil {
		slog.Error("Failed to check member rights", "err", err)
		return false
	}
	return member.Role == tb.Administrator || member.Role == tb.Creator
}

// GetUserDisplayName returns a user's display name'
func (ah *AdminHandler) GetUserDisplayName(user *tb.User) string {
	if user.Username != "" {
		return "@" + user.Username
	}
	name := user.FirstName
	if user.LastName != "" {
		name += " " + user.LastName
	}
	return fmt.Sprintf("%s (ID: %d)", name, user.ID)
}

// DeleteAfter deletes a message after a given duration
func (ah *AdminHandler) DeleteAfter(m *tb.Message, d time.Duration) {
	if m == nil {
		return
	}
	go func() {
		time.Sleep(d)
		if err := ah.bot.Delete(m); err != nil {
			slog.Warn("Failed to delete message", "msgID", m.ID, "err", err)
		}
	}()
}

// BanUser bans a user in a chat
func (ah *AdminHandler) BanUser(chat *tb.Chat, user *tb.User) error {
	return ah.bot.Ban(chat, &tb.ChatMember{User: user, Rights: tb.Rights{}})
}

// HandleBan handles the /banword command
func (ah *AdminHandler) HandleBan(c tb.Context) error {
	if !admin.IsAdminOrWarn(ah, c, "⛔ Команда /banword доступна только администрации.") {
		return nil
	}
	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		admin.ReplyAndDelete(ah, c, "💡 Используй: /banword слово1 [слово2 ...]", 10*time.Second)
		return nil
	}
	ah.blacklist.AddPhrase(args[1:])
	admin.ReplyAndDelete(ah, c, "✅ Добавлено запрещённое словосочетание: "+strings.Join(args[1:], " "), 10*time.Second)
	ah.LogToAdmin(fmt.Sprintf("🚫 Добавлено запрещённое слово\n\nАдмин: %s\nЗапрещённые слова: `%s`", ah.GetUserDisplayName(c.Sender()), strings.Join(args[1:], " ")))
	return nil
}

// HandleUnban handles the /unbanword command
func (ah *AdminHandler) HandleUnban(c tb.Context) error {
	if !admin.IsAdminOrWarn(ah, c, "⛔ Команда /unbanword доступна только администрации.") {
		return nil
	}
	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		admin.ReplyAndDelete(ah, c, "💡 Используй: /unbanword слово1 [слово2 ...]", 10*time.Second)
		return nil
	}
	ok := ah.blacklist.RemovePhrase(args[1:])
	text := "❌ Такого словосочетания нет в списке."
	if ok {
		text = "✅ Удалено запрещённое словосочетание: " + strings.Join(args[1:], " ")
		ah.LogToAdmin(fmt.Sprintf("✅ Удалено запрещённое слово\n\nАдмин: %s\nУдалённые слова: `%s`", ah.GetUserDisplayName(c.Sender()), strings.Join(args[1:], " ")))
	}
	admin.ReplyAndDelete(ah, c, text, 10*time.Second)
	return nil
}

// HandleListBan handles the /listbanword command
func (ah *AdminHandler) HandleListBan(c tb.Context) error {
	if !admin.IsAdminOrWarn(ah, c, "⛔ Команда /listbanword доступна только администрации.") {
		return nil
	}
	phrases := ah.blacklist.List()
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

// RegisterGroup registers a group chat
func (ah *AdminHandler) RegisterGroup(chat *tb.Chat) {
	if chat == nil || chat.Type == tb.ChatPrivate {
		return
	}
	ah.groupMu.Lock()
	ah.groupIDs[chat.ID] = struct{}{}
	ah.groupMu.Unlock()
}

// AllGroupIDs returns all registered group IDs
func (ah *AdminHandler) AllGroupIDs() []int64 {
	ah.groupMu.RLock()
	defer ah.groupMu.RUnlock()
	ids := make([]int64, 0, len(ah.groupIDs))
	for id := range ah.groupIDs {
		ids = append(ids, id)
	}
	return ids
}

// BanUserEverywhere bans a user in all registered groups
func (ah *AdminHandler) BanUserEverywhere(user *tb.User) {
	groupIDs := ah.AllGroupIDs()
	if len(groupIDs) == 0 {
		slog.Warn("No group IDs registered, cannot perform global ban", "user", ah.GetUserDisplayName(user))
	}
	for _, chatID := range groupIDs {
		chat := &tb.Chat{ID: chatID}
		err := ah.BanUser(chat, user)
		if err != nil {
			slog.Error("Failed to ban user in group", "user", ah.GetUserDisplayName(user), "chatID", chatID, "err", err)
		} else {
			slog.Info("User banned in group", "user", ah.GetUserDisplayName(user), "chatID", chatID)
		}
	}
}

// HandleSpamBan handles the /spamban command
func (ah *AdminHandler) HandleSpamBan(c tb.Context) error {
	if !admin.IsAdminOrWarn(ah, c, "⛔ Команда /spamban доступна только администрации.") {
		return nil
	}
	targetUser := admin.ResolveTargetUser(ah, c)
	if targetUser == nil {
		admin.ReplyAndDelete(ah, c, "❌ Не удалось определить пользователя для бана.", 10*time.Second)
		return nil
	}
	if ah.IsAdmin(c.Chat(), targetUser) {
		admin.ReplyAndDelete(ah, c, "⛔ Нельзя забанить администратора.", 10*time.Second)
		return nil
	}
	ah.BanUserEverywhere(targetUser)
	ah.ClearViolations(targetUser.ID)
	admin.Reply(ah, c, fmt.Sprintf("🔨 Пользователь %s забанен за спам.", ah.GetUserDisplayName(targetUser)))
	ah.LogToAdmin(fmt.Sprintf("🔨 Пользователь забанен за спам.\n\nЗабанен: %s\nАдмин: %s", ah.GetUserDisplayName(targetUser), ah.GetUserDisplayName(c.Sender())))
	return nil
}

// Violation management
func (ah *AdminHandler) AddViolation(userID int64) {
	ah.violationsMu.Lock()
	ah.violations[userID]++
	ah.violationsMu.Unlock()
	ah.saveViolations()
}

func (ah *AdminHandler) GetViolations(userID int64) int {
	ah.violationsMu.RLock()
	defer ah.violationsMu.RUnlock()
	return ah.violations[userID]
}

func (ah *AdminHandler) ClearViolations(userID int64) {
	ah.violationsMu.Lock()
	delete(ah.violations, userID)
	ah.violationsMu.Unlock()
	ah.saveViolations()
}

func (ah *AdminHandler) saveViolations() {
	data, err := json.MarshalIndent(ah.violations, "", "  ")
	if err != nil {
		slog.Error("Failed to marshal violations", "err", err)
		return
	}
	if err := os.WriteFile(ah.violationsFile, data, 0644); err != nil {
		slog.Error("Failed to write violations", "file", ah.violationsFile, "err", err)
	}
}

func (ah *AdminHandler) loadViolations() {
	data, err := os.ReadFile(ah.violationsFile)
	if err != nil {
		return
	}
	ah.violationsMu.Lock()
	defer ah.violationsMu.Unlock()
	_ = json.Unmarshal(data, &ah.violations)
	if ah.violations == nil {
		ah.violations = make(map[int64]int)
	}
}

// Bot returns the bot instance
func (ah *AdminHandler) Bot() *tb.Bot {
	return ah.bot
}
