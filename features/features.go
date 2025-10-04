package features

import (
	"UEPB/utils/interfaces"
	"UEPB/utils/logger"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/sirupsen/logrus"
	tb "gopkg.in/telebot.v4"
)

// FeatureHandler handles all user feature functionality
type FeatureHandler struct {
	bot         *tb.Bot
	state       interfaces.UserState
	quiz        interfaces.QuizInterface
	blacklist   interfaces.BlacklistInterface
	adminChatID int64
	violations  map[int64]int
	rlMu        sync.Mutex
	rateLimit   map[int64]time.Time
	Btns        struct {
		Student, Guest, Ads tb.InlineButton
	}
	adminHandler         interfaces.AdminHandlerInterface
	eventsCache          []EventData
	eventsCacheMu        sync.RWMutex
	cacheTime            time.Time
	eventRateLimit       map[int64]time.Time
	eventRateLimitMu     sync.Mutex
	eventInterests       map[string][]int64 // eventID -> list of user IDs
	eventInterestsMu     sync.RWMutex
	userEventInterests   map[int64]map[string]bool // userID -> eventID -> interested
	userEventInterestsMu sync.RWMutex
	pendingActivations   map[int64]string // userID -> eventID waiting for bot activation
	pendingActivationsMu sync.Mutex
	activatedUsers       map[int64]bool // userID -> has activated bot in private chat
	activatedUsersMu     sync.RWMutex
	eventMessageOwners   map[string]int64 // messageID -> userID who called /events
	eventMessageOwnersMu sync.RWMutex
}

// EventData stores event information
type EventData struct {
	Day         string
	Month       string
	Time        string
	Category    string
	Title       string
	Description string
}

// GetEventID returns a unique identifier for an event
func (e EventData) GetEventID() string {
	// Create a hash from day, month, and title
	fullID := fmt.Sprintf("%s_%s_%s", e.Day, e.Month, e.Title)
	hash := md5.Sum([]byte(fullID))
	// Return first 16 characters of hex hash (32 hex chars total)
	return hex.EncodeToString(hash[:])[:16]
}

// NewFeatureHandler creates a new feature handler
func NewFeatureHandler(bot *tb.Bot, state interfaces.UserState, quiz interfaces.QuizInterface, blacklist interfaces.BlacklistInterface, adminChatID int64, violations map[int64]int, adminHandler interfaces.AdminHandlerInterface, btns struct{ Student, Guest, Ads tb.InlineButton }) *FeatureHandler {
	return &FeatureHandler{
		bot:                bot,
		state:              state,
		quiz:               quiz,
		blacklist:          blacklist,
		adminChatID:        adminChatID,
		violations:         violations,
		rateLimit:          make(map[int64]time.Time),
		Btns:               btns,
		adminHandler:       adminHandler,
		eventRateLimit:     make(map[int64]time.Time),
		eventInterests:     make(map[string][]int64),
		userEventInterests: make(map[int64]map[string]bool),
		pendingActivations: make(map[int64]string),
		activatedUsers:     make(map[int64]bool),
		eventMessageOwners: make(map[string]int64),
	}
}

// GLOBAL FEATURES

// OnlyNewbies middleware to restrict handlers to newbies only
func (fh *FeatureHandler) OnlyNewbies(handler func(tb.Context) error) func(tb.Context) error {
	return func(c tb.Context) error {
		if c.Sender() == nil || !fh.state.IsNewbie(int(c.Sender().ID)) {
			if cb := c.Callback(); cb != nil {
				_ = fh.bot.Respond(cb, &tb.CallbackResponse{
					Text:      "Это не твоя кнопка",
					ShowAlert: false,
				})
			}
			return nil
		}
		return handler(c)
	}
}

// RateLimit middleware to limit command usage to once per second per user
func (fh *FeatureHandler) RateLimit(handler func(tb.Context) error) func(tb.Context) error {
	return func(c tb.Context) error {
		if c.Sender() == nil {
			return handler(c)
		}
		uid := c.Sender().ID
		fh.rlMu.Lock()
		last, ok := fh.rateLimit[uid]
		now := time.Now()
		if ok && now.Sub(last) < time.Second {
			fh.rateLimit[uid] = now
			fh.rlMu.Unlock()
			if c.Chat() != nil {
				warnMsg, _ := fh.bot.Send(c.Chat(), "⏱️ Пожалуйста, не чаще одной команды в секунду.")
				if fh.adminHandler != nil {
					fh.adminHandler.DeleteAfter(warnMsg, 5*time.Second)
				}
			}
			return nil
		}
		// Reset rate limit
		fh.rateLimit[uid] = now
		fh.rlMu.Unlock()
		return handler(c)
	}
}

// MAIN FEATURES

// SendOrEdit sends a new message or edits an existing one
func (fh *FeatureHandler) SendOrEdit(chat *tb.Chat, msg *tb.Message, text string, rm *tb.ReplyMarkup) *tb.Message {
	var err error
	if msg == nil {
		msg, err = fh.bot.Send(chat, text, rm)
	} else {
		msg, err = fh.bot.Edit(msg, text, rm)
	}
	if err != nil {
		logger.Error("Message error", err, logrus.Fields{
			"chat_id": chat.ID,
			"action":  "send_or_edit",
		})
		return nil
	}
	return msg
}

// SetUserRestriction sets user permissions in chat
func (fh *FeatureHandler) SetUserRestriction(chat *tb.Chat, user *tb.User, allowAll bool) {
	if allowAll {
		rights := tb.Rights{
			CanSendMessages:   true,
			CanSendPhotos:     true,
			CanSendVideos:     true,
			CanSendVideoNotes: true,
			CanSendVoiceNotes: true,
			CanSendPolls:      true,
			CanSendOther:      true,
			CanAddPreviews:    true,
			CanInviteUsers:    true,
		}
		if err := fh.bot.Restrict(chat, &tb.ChatMember{
			User:            user,
			Rights:          rights,
			RestrictedUntil: tb.Forever(),
		}); err != nil {
			logger.Error("Failed to unrestrict user", err, logrus.Fields{
				"chat_id": chat.ID,
				"user_id": user.ID,
				"action":  "unrestrict",
			})
		}
	} else {
		rights := tb.Rights{
			CanSendMessages: false,
		}
		if err := fh.bot.Restrict(chat, &tb.ChatMember{
			User:   user,
			Rights: rights,
		}); err != nil {
			logger.Error("Failed to restrict user", err, logrus.Fields{
				"chat_id": chat.ID,
				"user_id": user.ID,
				"action":  "restrict",
			})
		}
	}
}

// GetNewUsers extracts new users from a join message
func GetNewUsers(msg *tb.Message) []*tb.User {
	if len(msg.UsersJoined) > 0 {
		users := make([]*tb.User, len(msg.UsersJoined))
		for i := range msg.UsersJoined {
			users[i] = &msg.UsersJoined[i]
		}
		return users
	}
	if msg.UserJoined != nil {
		return []*tb.User{msg.UserJoined}
	}
	return nil
}

// HandleUserJoined handles user joining the chat
func (fh *FeatureHandler) HandleUserJoined(c tb.Context) error {
	if c.Message() == nil || c.Chat() == nil {
		return nil
	}
	if reg, ok := fh.adminHandler.(interface{ RegisterGroup(*tb.Chat) }); ok {
		reg.RegisterGroup(c.Chat())
	}

	users := GetNewUsers(c.Message())
	keyboard := &tb.ReplyMarkup{
		InlineKeyboard: [][]tb.InlineButton{{fh.Btns.Student}, {fh.Btns.Guest}, {fh.Btns.Ads}},
	}

	for _, u := range users {
		fh.state.SetNewbie(int(u.ID))
		fh.SetUserRestriction(c.Chat(), u, false)

		text := "👋 Привет!\n\nВыбери, что тебя интересует, используя кнопки ниже."
		if u.Username != "" {
			text = fmt.Sprintf("👋 Привет, @%s!\n\nВыбери, что тебя интересует, используя кнопки ниже.", u.Username)
		}
		msg := fh.SendOrEdit(c.Chat(), nil, text, keyboard)
		fh.adminHandler.DeleteAfter(msg, 5*time.Minute)
		fh.state.InitUser(int(u.ID))

		logMsg := fmt.Sprintf("👤 Новый участник вошёл в чат.\n\n"+
			"Пользователь: %s",
			fh.adminHandler.GetUserDisplayName(u))
		fh.adminHandler.LogToAdmin(logMsg)
	}
	return nil
}

// HandleUserLeft handles user leaving the chat
func (fh *FeatureHandler) HandleUserLeft(c tb.Context) error {
	if c.Message() == nil || c.Chat() == nil || c.Message().UserLeft == nil {
		return nil
	}

	user := c.Message().UserLeft
	fh.state.ClearNewbie(int(user.ID))

	// Reset violations
	fh.adminHandler.ClearViolations(user.ID)

	// Log to admin chat
	logMsg := fmt.Sprintf("👋 Участник покинул чат.\n\n"+
		"Пользователь: %s",
		fh.adminHandler.GetUserDisplayName(user))
	fh.adminHandler.LogToAdmin(logMsg)

	return nil
}

// HandleStudent handles student button click
func (fh *FeatureHandler) HandleStudent(c tb.Context) error {
	fh.state.InitUser(int(c.Sender().ID))
	questions := fh.quiz.GetQuestions()
	if len(questions) > 0 {
		q := questions[0]
		_ = fh.SendOrEdit(c.Chat(), c.Message(), q.GetText(), &tb.ReplyMarkup{InlineKeyboard: [][]tb.InlineButton{q.GetButtons()}})
	}
	return nil
}

// HandleGuest handles guest button click
func (fh *FeatureHandler) HandleGuest(c tb.Context) error {
	fh.SetUserRestriction(c.Chat(), c.Sender(), true)
	fh.state.ClearNewbie(int(c.Sender().ID))
	msg := fh.SendOrEdit(c.Chat(), c.Message(), "✅ Теперь можно писать в чат. Задай свой вопрос.", nil)
	fh.adminHandler.DeleteAfter(msg, 5*time.Second)

	// Log to admin chat
	logMsg := fmt.Sprintf("🧐 Пользователь выбрал, что у него есть вопрос.\n\n"+
		"Пользователь: %s",
		fh.adminHandler.GetUserDisplayName(c.Sender()))
	fh.adminHandler.LogToAdmin(logMsg)

	return nil
}

// HandleAds handles ads button click
func (fh *FeatureHandler) HandleAds(c tb.Context) error {
	msg := fh.SendOrEdit(c.Chat(), c.Message(), "📢 Мы открыты к рекламе.\n\nНапиши @chathlp и опиши, что хочешь предложить.", nil)
	fh.adminHandler.DeleteAfter(msg, 10*time.Second)

	// Log to admin chat
	logMsg := fmt.Sprintf("📢 Пользователь выбрал рекламу.\n\n"+
		"Пользователь: %s",
		fh.adminHandler.GetUserDisplayName(c.Sender()))
	fh.adminHandler.LogToAdmin(logMsg)

	return nil
}

// HandlePing handles /ping command in private chat
func (fh *FeatureHandler) HandlePing(c tb.Context) error {
	start := time.Now()
	if c.Message() == nil || c.Chat() == nil || c.Sender() == nil {
		return nil
	}

	if c.Chat().Type != tb.ChatPrivate {
		warnMsg, err := fh.bot.Send(c.Chat(), "ℹ️ Команда /ping доступна только в личных сообщениях с ботом.")
		if err != nil {
			logger.Error("Failed to send ping warning in group", err, logrus.Fields{
				"chat_id": c.Chat().ID,
				"user_id": c.Sender().ID,
			})
			return err
		}
		if fh.adminHandler != nil {
			fh.adminHandler.DeleteAfter(warnMsg, 5*time.Second)
		}
		return nil
	}

	msg, err := fh.bot.Send(c.Chat(), "🏓 Понг!")
	if err != nil {
		logger.Error("Failed to send ping response", err, logrus.Fields{
			"chat_id": c.Chat().ID,
			"user_id": c.Sender().ID,
		})
		return err
	}

	responseTime := time.Since(start)
	responseMs := int(responseTime.Nanoseconds() / 1000000) // Convert to milliseconds

	finalText := fmt.Sprintf("🏓 Понг! (%d мс)", responseMs)
	_, err = fh.bot.Edit(msg, finalText)
	if err != nil {
		logger.Error("Failed to edit ping message", err, logrus.Fields{
			"chat_id": c.Chat().ID,
			"user_id": c.Sender().ID,
		})
	}

	return nil
}

// RegisterQuizHandlers registers all quiz button handlers
func (fh *FeatureHandler) RegisterQuizHandlers(bot *tb.Bot) {
	questions := fh.quiz.GetQuestions()
	for i, q := range questions {
		for _, btn := range q.GetButtons() {
			bot.Handle(&btn, fh.OnlyNewbies(fh.CreateQuizHandler(i, q, btn)))
		}
	}
}

// CreateQuizHandler creates a handler for the quiz button
func (fh *FeatureHandler) CreateQuizHandler(i int, q interfaces.QuestionInterface, btn tb.InlineButton) func(tb.Context) error {
	return func(c tb.Context) error {
		userID := int(c.Sender().ID)
		if btn.Unique == q.GetAnswer() {
			fh.state.IncCorrect(userID)
		}

		questions := fh.quiz.GetQuestions()
		if i+1 < len(questions) {
			next := questions[i+1]
			_ = fh.SendOrEdit(c.Chat(), c.Message(), next.GetText(), &tb.ReplyMarkup{InlineKeyboard: [][]tb.InlineButton{next.GetButtons()}})
			return nil
		}

		totalCorrect := fh.state.TotalCorrect(userID)
		totalQuestions := len(questions)
		if totalCorrect >= 2 {
			fh.SetUserRestriction(c.Chat(), c.Sender(), true)
			fh.state.ClearNewbie(userID)
			msg := fh.SendOrEdit(c.Chat(), c.Message(), "✅ Верификация пройдена! Теперь можно писать в чат.", nil)
			fh.adminHandler.DeleteAfter(msg, 5*time.Second)

			// Log successful verification to admin chat
			logMsg := fmt.Sprintf("✅ Пользователь успешно прошёл верификацию.\n\n"+
				"Пользователь: %s\n"+
				"Правильных ответов: %d/%d",
				fh.adminHandler.GetUserDisplayName(c.Sender()),
				totalCorrect,
				totalQuestions)
			fh.adminHandler.LogToAdmin(logMsg)
		} else {
			msg := fh.SendOrEdit(c.Chat(), c.Message(), "❌ Не удалось подтвердить статус студента.", nil)
			fh.adminHandler.DeleteAfter(msg, 5*time.Second)

			// Log failed verification to admin chat
			logMsg := fmt.Sprintf("❌ Пользователь не прошёл верификацию.\n\n"+
				"Пользователь: %s\n"+
				"Правильных ответов: %d/%d",
				fh.adminHandler.GetUserDisplayName(c.Sender()),
				totalCorrect,
				totalQuestions)
			fh.adminHandler.LogToAdmin(logMsg)
		}
		fh.state.Reset(userID)
		return nil
	}
}

// FilterMessage filters incoming messages for banned words
func (fh *FeatureHandler) FilterMessage(c tb.Context) error {
	msg := c.Message()
	if msg == nil || msg.Sender == nil {
		return nil
	}

	// Don't filter commands
	if strings.HasPrefix(msg.Text, "/") {
		return nil
	}

	// Don't filter admin messages in admin chat only
	if c.Chat().ID == fh.adminChatID {
		return nil
	}

	// Don't filter admin messages in the current chat if they are admin there
	if fh.adminHandler.IsAdmin(c.Chat(), msg.Sender) {
		return nil
	}

	logger.Debug("Checking message for blacklist violations", logrus.Fields{
		"user_id": msg.Sender.ID,
		"message": msg.Text,
	})

	if fh.blacklist.CheckMessage(msg.Text) {
		fh.adminHandler.AddViolation(msg.Sender.ID)
		violationCount := fh.adminHandler.GetViolations(msg.Sender.ID)

		// Delete the message
		if err := fh.bot.Delete(msg); err != nil {
			logger.Error("Failed to delete message", err, logrus.Fields{
				"message_id": msg.ID,
				"user_id":    msg.Sender.ID,
				"chat_id":    c.Chat().ID,
			})
		} else {
			logger.Debug("Deleted blacklisted message", logrus.Fields{
				"message_id": msg.ID,
				"user_id":    msg.Sender.ID,
				"violation":  violationCount,
			})
		}

		// If it's their second violation, ban
		if violationCount >= 2 {
			if err := fh.adminHandler.BanUser(c.Chat(), msg.Sender); err != nil {
				logger.Error("Failed to ban user", err, logrus.Fields{
					"user_id": msg.Sender.ID,
					"chat_id": c.Chat().ID,
				})
			} else {
				logger.Info("Banned user for repeated violations", logrus.Fields{
					"user_id":    msg.Sender.ID,
					"violations": violationCount,
				})

				fh.adminHandler.ClearViolations(msg.Sender.ID)

				// Log to admin chat
				logMsg := fmt.Sprintf("🔨 Выдан бан за спам.\n\n"+
					"Забанен: %s\n"+
					"Количество нарушений: %d",
					fh.adminHandler.GetUserDisplayName(msg.Sender),
					violationCount)
				fh.adminHandler.LogToAdmin(logMsg)
			}
		} else {
			// Warning if it's their first violation
			warningMsg, _ := fh.bot.Send(c.Chat(), fmt.Sprintf("⚠️ %s, сообщение удалено. При повторном нарушении будет бан.", fh.adminHandler.GetUserDisplayName(msg.Sender)))
			fh.adminHandler.DeleteAfter(warningMsg, 5*time.Second)

			// Log to admin chat
			logMsg := fmt.Sprintf("⚠️ Обнаружено нарушение.\n\n"+
				"Пользователь: %s\n"+
				"Нарушение: #%d\n"+
				"Сообщение: `%s`",
				fh.adminHandler.GetUserDisplayName(msg.Sender),
				violationCount,
				msg.Text)
			fh.adminHandler.LogToAdmin(logMsg)
		}
	}
	return nil
}

// EVENT FEATURES

// fetchEventsFromWebsite fetches events from the UE Poznan website
func (fh *FeatureHandler) fetchEventsFromWebsite() error {
	// Create HTTP client with custom transport to skip certificate verification
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	url := "https://ue.poznan.pl/wydarzenia/"
	resp, err := client.Get(url)
	if err != nil {
		logger.Error("Failed to fetch events page", err, logrus.Fields{"url": url})
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		logger.Error("Non-200 status code", nil, logrus.Fields{"url": url, "status": resp.StatusCode})
		return fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		logger.Error("Failed to parse HTML", err, logrus.Fields{"url": url})
		return err
	}

	currentMonth := strings.TrimSpace(doc.Find(".eventsList__monthTitle").First().Text())

	var events []EventData
	doc.Find(".eventsList__event").Each(func(i int, s *goquery.Selection) {
		day := strings.TrimSpace(s.Find(".eventsList__eventDay").Text())
		eventTime := strings.TrimSpace(s.Find(".eventsList__eventTime").Text())
		category := strings.TrimSpace(s.Find(".eventsList__eventCategory").Text())
		title := strings.TrimSpace(s.Find(".eventsList__eventTitle").Text())
		fullText := strings.TrimSpace(s.Find(".eventsList__eventFullText .wysiwyg").Text())

		if fullText == "" {
			fullText = strings.TrimSpace(s.Find(".eventsList__eventExcerpt").Text())
		}

		if title != "" {
			events = append(events, EventData{
				Day:         day,
				Month:       currentMonth,
				Time:        eventTime,
				Category:    category,
				Title:       title,
				Description: fullText,
			})
		}
	})

	fh.eventsCacheMu.Lock()
	fh.eventsCache = events
	fh.cacheTime = time.Now()
	fh.eventsCacheMu.Unlock()

	logger.Info("Events cached successfully", logrus.Fields{"count": len(events)})
	return nil
}

// formatEventText formats a single event for display
func (fh *FeatureHandler) formatEventText(event EventData, index int, total int) string {
	var result strings.Builder

	result.WriteString(fmt.Sprintf("📰 %s\n\n", escapeMarkdown(event.Title)))

	if event.Description != "" {
		desc := strings.ReplaceAll(event.Description, "\n\n\n", "\n\n")
		desc = strings.TrimSpace(desc)

		trainingScheduleURL := "https://app.ue.poznan.pl/TrainingsSchedule/Account/Login?ReturnUrl=%2fTrainingsSchedule%2f"
		lines := strings.Split(desc, "\n")
		for i, line := range lines {
			trimmedLine := strings.TrimSpace(line)
			if trimmedLine == "Więcej informacji" {
				lines[i] = fmt.Sprintf("[Więcej informacji](%s)", trainingScheduleURL)
			} else {
				lines[i] = escapeMarkdown(line)
			}
		}
		desc = strings.Join(lines, "\n")

		result.WriteString(fmt.Sprintf("%s\n\n", desc))
	}

	if event.Day != "" {
		timeStr := ""
		if event.Time != "" {
			timeStr = strings.TrimSpace(event.Time)
			timeStr = strings.TrimSuffix(timeStr, "-")
			timeStr = strings.TrimSpace(timeStr)
		}

		monthName := event.Month
		if strings.Contains(monthName, " ") {
			parts := strings.Split(monthName, " ")
			monthName = strings.ToLower(parts[0])
		}

		if timeStr != "" {
			result.WriteString(fmt.Sprintf("🕒 Wydarzenie odbędzie się %s %s %s", escapeMarkdown(event.Day), escapeMarkdown(monthName), escapeMarkdown(timeStr)))
		} else {
			result.WriteString(fmt.Sprintf("🕒 Wydarzenie odbędzie się %s %s", escapeMarkdown(event.Day), escapeMarkdown(monthName)))
		}
	}

	result.WriteString(fmt.Sprintf("\n\nWydarzenie %d z %d", index+1, total))
	return result.String()
}

// escapeMarkdown escapes special Markdown characters
func escapeMarkdown(text string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"|", "\\|",
	)
	return replacer.Replace(text)
}

// HandleEvent handles the /events command (only in private chats)
func (fh *FeatureHandler) HandleEvent(c tb.Context) error {
	// Check if command is used in private chat
	if c.Chat().Type != tb.ChatPrivate {
		warnMsg, _ := fh.bot.Send(c.Chat(), "ℹ️ Команда /events доступна только в личных сообщениях с ботом.")
		if fh.adminHandler != nil {
			fh.adminHandler.DeleteAfter(warnMsg, 5*time.Second)
		}
		return nil
	}

	// Rate limiting for all users (1 request per 30 seconds)
	fh.eventRateLimitMu.Lock()
	lastUsed, exists := fh.eventRateLimit[c.Sender().ID]
	now := time.Now()

	if exists && now.Sub(lastUsed) < 30*time.Second {
		remainingTime := 30*time.Second - now.Sub(lastUsed)
		remainingSeconds := int(remainingTime.Seconds())

		fh.eventRateLimitMu.Unlock()

		_, _ = fh.bot.Send(c.Chat(), fmt.Sprintf("⏱️ Команда /events доступна раз в 30 секунд. Повтори через %d сек.", remainingSeconds))
		return nil
	}

	fh.eventRateLimit[c.Sender().ID] = now
	fh.eventRateLimitMu.Unlock()

	statusMsg, _ := fh.bot.Send(c.Chat(), "🔄 Загрузка событий...")

	fh.eventsCacheMu.RLock()
	cacheValid := time.Since(fh.cacheTime) < 5*time.Minute && len(fh.eventsCache) > 0
	fh.eventsCacheMu.RUnlock()

	if !cacheValid {
		err := fh.fetchEventsFromWebsite()
		if err != nil {
			fh.bot.Edit(statusMsg, "❌ Ошибка при загрузке событий.")
			return nil
		}
	}

	fh.eventsCacheMu.RLock()
	defer fh.eventsCacheMu.RUnlock()

	if len(fh.eventsCache) == 0 {
		fh.bot.Edit(statusMsg, "❌ Событий нет.")
		return nil
	}

	event := fh.eventsCache[0]
	eventText := fh.formatEventText(event, 0, len(fh.eventsCache))

	nextBtn := tb.InlineButton{
		Unique: "next_event",
		Text:   "Далее ➡️",
		Data:   fmt.Sprintf("nav_%d", 0),
	}

	interestedBtn := tb.InlineButton{
		Unique: "event_interested",
		Text:   "Интересует 🔔",
		Data:   fmt.Sprintf("int_%s", event.GetEventID()),
	}

	markup := &tb.ReplyMarkup{
		InlineKeyboard: [][]tb.InlineButton{
			{nextBtn},
			{interestedBtn},
		},
	}

	editedMsg, err := fh.bot.Edit(statusMsg, eventText, markup, tb.ModeMarkdown)
	if err != nil {
		logger.Error("Failed to edit event message", err, logrus.Fields{
			"chat_id": c.Chat().ID,
			"user_id": c.Sender().ID,
		})
		// Try without markup as fallback
		editedMsg, err = fh.bot.Edit(statusMsg, eventText, tb.ModeMarkdown)
		if err != nil {
			logger.Error("Failed to edit event message (no markup)", err, logrus.Fields{
				"chat_id": c.Chat().ID,
				"user_id": c.Sender().ID,
			})
			return nil
		}
	}

	// Store message owner for private chat
	if editedMsg != nil {
		messageKey := fmt.Sprintf("%d_%d", editedMsg.Chat.ID, editedMsg.ID)
		fh.eventMessageOwnersMu.Lock()
		fh.eventMessageOwners[messageKey] = c.Sender().ID
		fh.eventMessageOwnersMu.Unlock()
	}

	logger.Info("Event displayed in private chat", logrus.Fields{
		"user":        fh.adminHandler.GetUserDisplayName(c.Sender()),
		"event_index": 0,
		"total":       len(fh.eventsCache),
	})

	return nil
}

// HandlePrevEvent handles the previous event button
func (fh *FeatureHandler) HandlePrevEvent(c tb.Context) error {
	if c.Callback() == nil || c.Sender() == nil || c.Callback().Message == nil {
		return nil
	}

	// Check if user is the owner of this message
	messageKey := fmt.Sprintf("%d_%d", c.Callback().Message.Chat.ID, c.Callback().Message.ID)
	fh.eventMessageOwnersMu.RLock()
	ownerID, exists := fh.eventMessageOwners[messageKey]
	fh.eventMessageOwnersMu.RUnlock()

	if exists && ownerID != c.Sender().ID {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{
			Text:      "Это не твоя кнопка",
			ShowAlert: false,
		})
	}

	data := c.Callback().Data
	var currentIndex int
	_, err := fmt.Sscanf(data, "nav_%d", &currentIndex)
	if err != nil {
		return nil
	}

	prevIndex := currentIndex - 1
	if prevIndex < 0 {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{
			Text:      "Это первое событие",
			ShowAlert: false,
		})
	}

	fh.eventsCacheMu.RLock()
	defer fh.eventsCacheMu.RUnlock()

	if prevIndex >= len(fh.eventsCache) {
		return nil
	}

	event := fh.eventsCache[prevIndex]
	eventText := fh.formatEventText(event, prevIndex, len(fh.eventsCache))

	var navButtons []tb.InlineButton

	if prevIndex > 0 {
		prevBtn := tb.InlineButton{
			Unique: "prev_event",
			Text:   "⬅️ Назад",
			Data:   fmt.Sprintf("nav_%d", prevIndex),
		}
		navButtons = append(navButtons, prevBtn)
	}
	if prevIndex < len(fh.eventsCache)-1 {
		nextBtn := tb.InlineButton{
			Unique: "next_event",
			Text:   "Далее ➡️",
			Data:   fmt.Sprintf("nav_%d", prevIndex),
		}
		navButtons = append(navButtons, nextBtn)
	}

	interestedBtn := tb.InlineButton{
		Unique: "event_interested",
		Text:   "Интересует 🔔",
		Data:   fmt.Sprintf("int_%s", event.GetEventID()),
	}

	markup := &tb.ReplyMarkup{
		InlineKeyboard: [][]tb.InlineButton{navButtons, {interestedBtn}},
	}

	_, err = fh.bot.Edit(c.Callback().Message, eventText, markup, tb.ModeMarkdown)
	if err != nil {
		logger.Error("Failed to edit prev event message", err, logrus.Fields{
			"chat_id": c.Callback().Message.Chat.ID,
			"user_id": c.Sender().ID,
		})
	}
	return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{})
}

// HandleNextEvent handles the next event button
func (fh *FeatureHandler) HandleNextEvent(c tb.Context) error {
	if c.Callback() == nil || c.Sender() == nil || c.Callback().Message == nil {
		return nil
	}

	// Check if user is the owner of this message
	messageKey := fmt.Sprintf("%d_%d", c.Callback().Message.Chat.ID, c.Callback().Message.ID)
	fh.eventMessageOwnersMu.RLock()
	ownerID, exists := fh.eventMessageOwners[messageKey]
	fh.eventMessageOwnersMu.RUnlock()

	if exists && ownerID != c.Sender().ID {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{
			Text:      "Это не твоя кнопка",
			ShowAlert: false,
		})
	}

	data := c.Callback().Data
	var currentIndex int
	_, err := fmt.Sscanf(data, "nav_%d", &currentIndex)
	if err != nil {
		return nil
	}

	nextIndex := currentIndex + 1

	fh.eventsCacheMu.RLock()
	defer fh.eventsCacheMu.RUnlock()

	if nextIndex >= len(fh.eventsCache) {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{
			Text:      "Это последнее событие",
			ShowAlert: false,
		})
	}

	event := fh.eventsCache[nextIndex]
	eventText := fh.formatEventText(event, nextIndex, len(fh.eventsCache))

	var navButtons []tb.InlineButton

	if nextIndex > 0 {
		prevBtn := tb.InlineButton{
			Unique: "prev_event",
			Text:   "⬅️ Назад",
			Data:   fmt.Sprintf("nav_%d", nextIndex),
		}
		navButtons = append(navButtons, prevBtn)
	}
	if nextIndex < len(fh.eventsCache)-1 {
		nextBtn := tb.InlineButton{
			Unique: "next_event",
			Text:   "Далее ➡️",
			Data:   fmt.Sprintf("nav_%d", nextIndex),
		}
		navButtons = append(navButtons, nextBtn)
	}

	interestedBtn := tb.InlineButton{
		Unique: "event_interested",
		Text:   "Интересует 🔔",
		Data:   fmt.Sprintf("int_%s", event.GetEventID()),
	}

	markup := &tb.ReplyMarkup{
		InlineKeyboard: [][]tb.InlineButton{navButtons, {interestedBtn}},
	}

	_, err = fh.bot.Edit(c.Callback().Message, eventText, markup, tb.ModeMarkdown)
	if err != nil {
		logger.Error("Failed to edit next event message", err, logrus.Fields{
			"chat_id": c.Callback().Message.Chat.ID,
			"user_id": c.Sender().ID,
		})
	}
	return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{})
}

// HandleEventInterested handles when user clicks "Интересует" button
func (fh *FeatureHandler) HandleEventInterested(c tb.Context) error {
	if c.Callback() == nil || c.Sender() == nil || c.Callback().Message == nil {
		return nil
	}

	// Check if the user is the owner of this message
	messageKey := fmt.Sprintf("%d_%d", c.Callback().Message.Chat.ID, c.Callback().Message.ID)
	fh.eventMessageOwnersMu.RLock()
	ownerID, exists := fh.eventMessageOwners[messageKey]
	fh.eventMessageOwnersMu.RUnlock()

	if exists && ownerID != c.Sender().ID {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{
			Text:      "Это не твоя кнопка",
			ShowAlert: false,
		})
	}

	// Extract event ID from callback data
	eventID := strings.TrimPrefix(c.Callback().Data, "int_")
	userID := c.Sender().ID

	// Check if user has already expressed interest
	fh.userEventInterestsMu.RLock()
	userInterests, existsInterest := fh.userEventInterests[userID]
	alreadyInterested := existsInterest && userInterests[eventID]
	fh.userEventInterestsMu.RUnlock()

	if alreadyInterested {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{
			Text:      "Ты уже подписан на это событие",
			ShowAlert: false,
		})
	}

	// Find the event details for confirmation message
	fh.eventsCacheMu.RLock()
	var currentEvent *EventData
	for i := range fh.eventsCache {
		if fh.eventsCache[i].GetEventID() == eventID {
			currentEvent = &fh.eventsCache[i]
			break
		}
	}
	fh.eventsCacheMu.RUnlock()

	// Register interest immediately (no activation needed)
	fh.eventInterestsMu.Lock()
	if fh.eventInterests[eventID] == nil {
		fh.eventInterests[eventID] = []int64{}
	}
	fh.eventInterests[eventID] = append(fh.eventInterests[eventID], userID)
	fh.eventInterestsMu.Unlock()

	fh.userEventInterestsMu.Lock()
	if fh.userEventInterests[userID] == nil {
		fh.userEventInterests[userID] = make(map[string]bool)
	}
	fh.userEventInterests[userID][eventID] = true
	fh.userEventInterestsMu.Unlock()

	logger.Info("User subscribed to event", logrus.Fields{
		"user_id":  userID,
		"event_id": eventID,
	})

	// Send detailed confirmation
	if currentEvent != nil {
		eventTimeInfo := ""
		if currentEvent.Day != "" && currentEvent.Month != "" {
			monthName := currentEvent.Month
			if strings.Contains(monthName, " ") {
				parts := strings.Split(monthName, " ")
				monthName = strings.ToLower(parts[0])
			}

			if currentEvent.Time != "" {
				timeStr := strings.TrimSpace(currentEvent.Time)
				timeStr = strings.TrimSuffix(timeStr, "-")
				timeStr = strings.TrimSpace(timeStr)
				eventTimeInfo = fmt.Sprintf("%s %s в %s", currentEvent.Day, monthName, timeStr)
			} else {
				eventTimeInfo = fmt.Sprintf("%s %s", currentEvent.Day, monthName)
			}
		}

		unsubBtn := tb.InlineButton{
			Unique: "event_unsubscribe",
			Text:   "Больше не интересно ❌",
			Data:   fmt.Sprintf("unsub_%s", eventID),
		}

		markup := &tb.ReplyMarkup{
			InlineKeyboard: [][]tb.InlineButton{{unsubBtn}},
		}

		confirmText := fmt.Sprintf(
			"✅ Ты успешно подписался на событие.\n\n"+
				"Событие: %s\n"+
				"Время: %s\n\n"+
				"Я пришлю тебе напоминания за сутки и за 2 часа до начала.",
			currentEvent.Title,
			eventTimeInfo,
		)

		_, err := fh.bot.Send(c.Chat(), confirmText, markup)
		if err != nil {
			logger.Error("Failed to send confirmation", err, logrus.Fields{
				"user_id": userID,
			})
		}
	}
	return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{})
}

// HandleStart handles /start command
func (fh *FeatureHandler) HandleStart(c tb.Context) error {
	if c.Chat().Type != tb.ChatPrivate || c.Sender() == nil {
		return nil
	}

	userID := c.Sender().ID

	// Mark user as activated
	fh.activatedUsersMu.Lock()
	fh.activatedUsers[userID] = true
	fh.activatedUsersMu.Unlock()

	// Regular start message
	_, err := fh.bot.Send(c.Chat(),
		"👋 Привет! Я – бот студенческой группы UEP.\n\nНачни вводить команды с / и я тебе покажу, что могу делать",
	)

	logger.Info("User started bot", logrus.Fields{
		"user_id": userID,
	})

	return err
}

// HandlePrivateMessage handles any private message
func (fh *FeatureHandler) HandlePrivateMessage(c tb.Context) error {
	if c.Chat().Type != tb.ChatPrivate || c.Sender() == nil || c.Message() == nil {
		return nil
	}

	// Ignore commands
	if strings.HasPrefix(c.Message().Text, "/") {
		return nil
	}

	userID := c.Sender().ID

	// Mark user as activated
	fh.activatedUsersMu.Lock()
	fh.activatedUsers[userID] = true
	fh.activatedUsersMu.Unlock()

	return nil
}

// HandleEventUnsubscribe handles when user clicks "Больше не интересно" button
func (fh *FeatureHandler) HandleEventUnsubscribe(c tb.Context) error {
	if c.Callback() == nil || c.Sender() == nil {
		return nil
	}

	eventID := strings.TrimPrefix(c.Callback().Data, "unsub_")
	userID := c.Sender().ID

	// Check if user is actually subscribed
	fh.userEventInterestsMu.RLock()
	userInterests, exists := fh.userEventInterests[userID]
	isSubscribed := exists && userInterests[eventID]
	fh.userEventInterestsMu.RUnlock()

	if !isSubscribed {
		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{
			Text:      "Ты не подписан на это событие",
			ShowAlert: false,
		})
	}

	// Remove user from event interests
	fh.eventInterestsMu.Lock()
	if users, exists := fh.eventInterests[eventID]; exists {
		newUsers := []int64{}
		for _, uid := range users {
			if uid != userID {
				newUsers = append(newUsers, uid)
			}
		}
		fh.eventInterests[eventID] = newUsers
	}
	fh.eventInterestsMu.Unlock()

	fh.userEventInterestsMu.Lock()
	if userInterests, exists := fh.userEventInterests[userID]; exists {
		delete(userInterests, eventID)
	}
	fh.userEventInterestsMu.Unlock()

	// Edit message to show unsubscribed
	fh.bot.Edit(c.Callback().Message,
		"❌ Ты отписался от уведомлений об этом событии.",
	)

	logger.Info("User unsubscribed from event", logrus.Fields{
		"user_id":  userID,
		"event_id": eventID,
	})

	return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{
		Text: "Ты отписался от уведомлений",
	})
}
