package bot

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"UEPB/internal/core"
	"UEPB/internal/i18n"

	"github.com/sirupsen/logrus"
	tb "gopkg.in/telebot.v4"
)

// AdminHandler manages admin actions, logs and violations
type AdminHandler struct {
	bot             *tb.Bot
	blacklist       core.BlacklistInterface
	adminChatID     int64
	violations      map[int64]int
	violationsMu    sync.RWMutex
	violationsFile  string
	groupIDs        map[int64]struct{}
	groupMu         sync.RWMutex
	userLanguages   map[int64]i18n.Lang
	userLanguagesMu sync.RWMutex
}

// NewAdminHandler creates a new admin handler with persisted violations
func NewAdminHandler(bot *tb.Bot, blacklist core.BlacklistInterface, adminChatID int64, violations map[int64]int) *AdminHandler {
	_ = os.MkdirAll("data", 0755)
	ah := &AdminHandler{
		bot:            bot,
		blacklist:      blacklist,
		adminChatID:    adminChatID,
		violations:     violations,
		violationsFile: "data/violations.json",
		groupIDs:       make(map[int64]struct{}),
		userLanguages:  make(map[int64]i18n.Lang),
	}
	ah.loadViolations()
	return ah
}

// LogToAdmin sends a message to admin chat
func (ah *AdminHandler) LogToAdmin(message string) {
	if _, err := ah.bot.Send(&tb.Chat{ID: ah.adminChatID}, message); err != nil {
		logrus.WithError(err).WithField("admin_chat_id", ah.adminChatID).Error("Failed to send admin log")
	}
}

// IsAdmin checks if a user is admin in chat
func (ah *AdminHandler) IsAdmin(chat *tb.Chat, user *tb.User) bool {
	member, err := ah.bot.ChatMemberOf(chat, user)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"chat_id": chat.ID, "user_id": user.ID}).Error("Failed to check member rights")
		return false
	}
	return member.Role == tb.Administrator || member.Role == tb.Creator
}

// GetUserDisplayName returns display name
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

// getLangForUser returns language for a specific user
func (ah *AdminHandler) getLangForUser(user *tb.User) i18n.Lang {
	return getLangForUser(user, ah.userLanguages, &ah.userLanguagesMu)
}

// DeleteAfter deletes message after delay
func (ah *AdminHandler) DeleteAfter(m *tb.Message, d time.Duration) {
	if m == nil {
		return
	}
	go func() {
		time.Sleep(d)
		_ = ah.bot.Delete(m)
	}()
}

// BanUser bans a user in chat
func (ah *AdminHandler) BanUser(chat *tb.Chat, user *tb.User) error {
	return ah.bot.Ban(chat, &tb.ChatMember{User: user, Rights: tb.Rights{}})
}

// HandleBan adds a phrase to the blocklist
func (ah *AdminHandler) HandleBan(c tb.Context) error {
	lang := ah.getLangForUser(c.Sender())
	msgs := i18n.Get().T(lang)

	if c.Message() == nil || c.Sender() == nil || !ah.IsAdmin(c.Chat(), c.Sender()) {
		msg, _ := ah.bot.Send(c.Chat(), msgs.Admin.BanCommandAdminOnly)
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}
	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		msg, _ := ah.bot.Send(c.Chat(), msgs.Admin.BanUsage)
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}
	ah.blacklist.AddPhrase(args[1:])
	msg, _ := ah.bot.Send(c.Chat(), fmt.Sprintf(msgs.Admin.BanAdded, strings.Join(args[1:], " ")))
	ah.DeleteAfter(msg, 10*time.Second)
	ah.LogToAdmin(fmt.Sprintf("🚫 Добавлено запрещённое слово\n\nАдмин: %s\nЗапрещённые слова: `%s`", ah.GetUserDisplayName(c.Sender()), strings.Join(args[1:], " ")))
	return nil
}

// HandleUnban removes a phrase
func (ah *AdminHandler) HandleUnban(c tb.Context) error {
	lang := ah.getLangForUser(c.Sender())
	msgs := i18n.Get().T(lang)

	if c.Message() == nil || c.Sender() == nil || !ah.IsAdmin(c.Chat(), c.Sender()) {
		msg, _ := ah.bot.Send(c.Chat(), msgs.Admin.UnbanCommandAdminOnly)
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}
	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		msg, _ := ah.bot.Send(c.Chat(), msgs.Admin.UnbanUsage)
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}
	ok := ah.blacklist.RemovePhrase(args[1:])
	text := msgs.Admin.UnbanNotFound
	if ok {
		text = fmt.Sprintf(msgs.Admin.UnbanRemoved, strings.Join(args[1:], " "))
		ah.LogToAdmin(fmt.Sprintf("✅ Удалено запрещённое слово\n\nАдмин: %s\nУдалённые слова: `%s`", ah.GetUserDisplayName(c.Sender()), strings.Join(args[1:], " ")))
	}
	msg, _ := ah.bot.Send(c.Chat(), text)
	ah.DeleteAfter(msg, 10*time.Second)
	return nil
}

// HandleListBan shows the banned list
func (ah *AdminHandler) HandleListBan(c tb.Context) error {
	lang := ah.getLangForUser(c.Sender())
	msgs := i18n.Get().T(lang)

	if c.Message() == nil || c.Sender() == nil || !ah.IsAdmin(c.Chat(), c.Sender()) {
		msg, _ := ah.bot.Send(c.Chat(), msgs.Admin.ListCommandAdminOnly)
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}
	phrases := ah.blacklist.List()
	if len(phrases) == 0 {
		_, _ = ah.bot.Send(c.Chat(), msgs.Admin.ListEmpty)
		return nil
	}
	var sb strings.Builder
	sb.WriteString(msgs.Admin.ListHeader)
	for i, p := range phrases {
		sb.WriteString(fmt.Sprintf("%d. `%s`\n", i+1, strings.Join(p, " ")))
	}
	_, _ = ah.bot.Send(c.Chat(), sb.String(), tb.ModeMarkdown)
	return nil
}

// RegisterGroup remembers group chat for global actions
func (ah *AdminHandler) RegisterGroup(chat *tb.Chat) {
	if chat == nil || chat.Type == tb.ChatPrivate {
		return
	}
	ah.groupMu.Lock()
	ah.groupIDs[chat.ID] = struct{}{}
	ah.groupMu.Unlock()
}

// AllGroupIDs returns all stored group IDs
func (ah *AdminHandler) AllGroupIDs() []int64 {
	ah.groupMu.RLock()
	defer ah.groupMu.RUnlock()
	ids := make([]int64, 0, len(ah.groupIDs))
	for id := range ah.groupIDs {
		ids = append(ids, id)
	}
	return ids
}

// BanUserEverywhere bans user in all groups
func (ah *AdminHandler) BanUserEverywhere(user *tb.User) {
	groupIDs := ah.AllGroupIDs()
	if len(groupIDs) == 0 {
		logrus.WithField("user", ah.GetUserDisplayName(user)).Warn("No group IDs registered")
	}
	for _, chatID := range groupIDs {
		chat := &tb.Chat{ID: chatID}
		err := ah.BanUser(chat, user)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"user": ah.GetUserDisplayName(user), "chat_id": chatID}).Error("Failed to ban user in group")
		} else {
			logrus.WithFields(logrus.Fields{"user": ah.GetUserDisplayName(user), "chat_id": chatID}).Info("User banned in group")
		}
	}
}

// HandleSpamBan performs the spam ban command.
func (ah *AdminHandler) HandleSpamBan(c tb.Context) error {
	lang := ah.getLangForUser(c.Sender())
	msgs := i18n.Get().T(lang)

	if c.Message() == nil || c.Sender() == nil || !ah.IsAdmin(c.Chat(), c.Sender()) {
		msg, _ := ah.bot.Send(c.Chat(), msgs.Admin.SpambanCommandAdminOnly)
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}
	target := ah.resolveTargetUser(c)
	if target == nil {
		msg, _ := ah.bot.Send(c.Chat(), msgs.Admin.SpambanUserNotFound)
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}
	if ah.IsAdmin(c.Chat(), target) {
		msg, _ := ah.bot.Send(c.Chat(), msgs.Admin.SpambanCannotBanAdmin)
		ah.DeleteAfter(msg, 10*time.Second)
		return nil
	}
	ah.BanUserEverywhere(target)
	ah.ClearViolations(target.ID)
	_, _ = ah.bot.Send(c.Chat(), fmt.Sprintf(msgs.Admin.SpambanSuccess, ah.GetUserDisplayName(target)))
	ah.LogToAdmin(fmt.Sprintf("🔨 Пользователь забанен за спам.\n\nЗабанен: %s\nАдмин: %s", ah.GetUserDisplayName(target), ah.GetUserDisplayName(c.Sender())))
	return nil
}

// resolveTargetUser finds user from reply or argument
func (ah *AdminHandler) resolveTargetUser(c tb.Context) *tb.User {
	if c.Message().ReplyTo != nil && c.Message().ReplyTo.Sender != nil {
		return c.Message().ReplyTo.Sender
	}
	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		return nil
	}
	idStr := args[1]
	if strings.HasPrefix(idStr, "@") {
		m, err := ah.bot.ChatMemberOf(c.Chat(), &tb.User{Username: idStr[1:]})
		if err == nil && m.User != nil {
			return m.User
		}
	} else if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
		m, err := ah.bot.ChatMemberOf(c.Chat(), &tb.User{ID: id})
		if err == nil && m.User != nil {
			return m.User
		}
	}
	return nil
}

// AddViolation increments violation count
func (ah *AdminHandler) AddViolation(userID int64) {
	ah.violationsMu.Lock()
	ah.violations[userID]++
	ah.violationsMu.Unlock()
	ah.saveViolations()
}

// GetViolations returns count
func (ah *AdminHandler) GetViolations(userID int64) int {
	ah.violationsMu.RLock()
	v := ah.violations[userID]
	ah.violationsMu.RUnlock()
	return v
}

// ClearViolations removes record
func (ah *AdminHandler) ClearViolations(userID int64) {
	ah.violationsMu.Lock()
	delete(ah.violations, userID)
	ah.violationsMu.Unlock()
	ah.saveViolations()
}

// saveViolations persists violation count to disk
func (ah *AdminHandler) saveViolations() {
	data, err := json.MarshalIndent(ah.violations, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(ah.violationsFile, data, 0644)
}

// loadViolations reads violation count from disk
func (ah *AdminHandler) loadViolations() {
	data, err := os.ReadFile(ah.violationsFile)
	if err != nil {
		return
	}
	ah.violationsMu.Lock()
	_ = json.Unmarshal(data, &ah.violations)
	if ah.violations == nil {
		ah.violations = make(map[int64]int)
	}
	ah.violationsMu.Unlock()
}

// Bot returns bot instance
func (ah *AdminHandler) Bot() *tb.Bot { return ah.bot }
