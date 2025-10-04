package features

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"UEPB/utils/admin"
	"UEPB/utils/interfaces"
	"UEPB/utils/logger"

	"github.com/PuerkitoBio/goquery"
	"github.com/sirupsen/logrus"
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
		logger.Error("Failed to send admin log", err, logrus.Fields{
			"admin_chat_id": ah.adminChatID,
		})
	}
}

// IsAdmin checks if a user is an admin
func (ah *AdminHandler) IsAdmin(chat *tb.Chat, user *tb.User) bool {
	member, err := ah.bot.ChatMemberOf(chat, user)
	if err != nil {
		logger.Error("Failed to check member rights", err, logrus.Fields{
			"chat_id": chat.ID,
			"user_id": user.ID,
		})
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
			logger.Warn("Failed to delete message", logrus.Fields{
				"message_id": m.ID,
			})
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
		logger.Warn("No group IDs registered, cannot perform global ban", logrus.Fields{
			"user": ah.GetUserDisplayName(user),
		})
	}
	for _, chatID := range groupIDs {
		chat := &tb.Chat{ID: chatID}
		err := ah.BanUser(chat, user)
		if err != nil {
			logger.Error("Failed to ban user in group", err, logrus.Fields{
				"user":    ah.GetUserDisplayName(user),
				"chat_id": chatID,
			})
		} else {
			logger.Info("User banned in group", logrus.Fields{
				"user":    ah.GetUserDisplayName(user),
				"chat_id": chatID,
			})
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
		logger.Error("Failed to marshal violations", err)
		return
	}
	if err := os.WriteFile(ah.violationsFile, data, 0644); err != nil {
		logger.Error("Failed to write violations", err, logrus.Fields{
			"file": ah.violationsFile,
		})
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

// HandleTestParsing handles the /testparsing command
func (ah *AdminHandler) HandleTestParsing(c tb.Context) error {
	if !admin.IsAdminOrWarn(ah, c, "⛔ Команда /testparsing доступна только администрации.") {
		return nil
	}

	// Send the initial message
	statusMsg, _ := ah.bot.Send(c.Chat(), "🔄 Парсинг...")

	// Create HTTP client with custom transport
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	// Parse the website
	url := "https://ue.poznan.pl/wydarzenia/"
	resp, err := client.Get(url)
	if err != nil {
		logger.Error("Failed to fetch events page", err, logrus.Fields{
			"url": url,
		})
		ah.bot.Edit(statusMsg, "❌ Ошибка при загрузке страницы.")
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		logger.Error("Non-200 status code", nil, logrus.Fields{
			"url":    url,
			"status": resp.StatusCode,
		})
		ah.bot.Edit(statusMsg, fmt.Sprintf("❌ Ошибка: HTTP статус %d", resp.StatusCode))
		return nil
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		logger.Error("Failed to parse HTML", err, logrus.Fields{
			"url": url,
		})
		ah.bot.Edit(statusMsg, "❌ Ошибка при парсинге HTML.")
		return nil
	}

	// Extract text content
	var result strings.Builder
	result.WriteString("📰 *События на сайте UE Poznań:*\n\n")

	// Find current month
	currentMonth := strings.TrimSpace(doc.Find(".eventsList__monthTitle").First().Text())
	if currentMonth != "" {
		result.WriteString(fmt.Sprintf("📅 *%s*\n\n", currentMonth))
	}

	// Find all event items
	eventCount := 0
	doc.Find(".eventsList__event").Each(func(i int, s *goquery.Selection) {
		if eventCount >= 10 { // Limit to first 10 events
			return
		}

		// Extract event date
		day := strings.TrimSpace(s.Find(".eventsList__eventDay").Text())
		eventTime := strings.TrimSpace(s.Find(".eventsList__eventTime").Text())

		// Extract event category
		category := strings.TrimSpace(s.Find(".eventsList__eventCategory").Text())

		// Extract event title
		title := strings.TrimSpace(s.Find(".eventsList__eventTitle").Text())

		// Extract event excerpt (short description)
		excerpt := strings.TrimSpace(s.Find(".eventsList__eventExcerpt").Text())

		if title != "" {
			eventCount++

			// Format date and time
			dateTimeStr := ""
			if day != "" {
				dateTimeStr = fmt.Sprintf("📅 %s", day)
				if eventTime != "" {
					dateTimeStr += fmt.Sprintf(" | ⏰ %s", eventTime)
				}
			}

			if dateTimeStr != "" {
				result.WriteString(fmt.Sprintf("%s\n", dateTimeStr))
			}

			// Add category if available
			if category != "" {
				result.WriteString(fmt.Sprintf("🏷 %s\n", category))
			}

			// Add title
			result.WriteString(fmt.Sprintf("*%d. %s*\n", eventCount, title))

			// Add excerpt if available and not too long
			if excerpt != "" {
				if len(excerpt) > 150 {
					excerpt = excerpt[:150] + "..."
				}
				result.WriteString(fmt.Sprintf("%s\n", excerpt))
			}

			result.WriteString("\n")
		}
	})

	// If no events found
	if eventCount == 0 {
		result.WriteString("❌ События не найдены на странице.\n\n")

		// Try to get page title as fallback
		pageTitle := strings.TrimSpace(doc.Find("title").First().Text())
		if pageTitle != "" {
			result.WriteString(fmt.Sprintf("📌 Заголовок страницы: %s", pageTitle))
		}
	} else {
		result.WriteString(fmt.Sprintf("_Найдено событий: %d_\n", eventCount))
		result.WriteString(fmt.Sprintf("🔗 [Посмотреть все события](%s)", url))
	}

	finalText := result.String()

	// Telegram message limit is 4096 characters
	if len(finalText) > 512 {
		finalText = finalText[:512] + "\n\n..."
	}

	// Send the result
	ah.bot.Edit(statusMsg, finalText, tb.ModeMarkdown)

	// Log to admin
	ah.LogToAdmin(fmt.Sprintf("🔍 Парсинг выполнен\n\nАдмин: %s\nURL: %s\nНайдено событий: %d",
		ah.GetUserDisplayName(c.Sender()),
		url,
		eventCount))

	logger.Info("Website parsing completed", logrus.Fields{
		"url":         url,
		"admin":       ah.GetUserDisplayName(c.Sender()),
		"event_count": eventCount,
	})

	return nil
}
