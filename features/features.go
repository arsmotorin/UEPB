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
	registeredGroups     map[int64]bool // chatID -> registered for broadcasts
	registeredGroupsMu   sync.RWMutex
	broadcastedEvents    map[string]bool // eventID -> already broadcasted
	broadcastedEventsMu  sync.Mutex
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
		registeredGroups:   make(map[int64]bool),
		broadcastedEvents:  make(map[string]bool),
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

	// Register group for event broadcasts
	if c.Chat().Type == tb.ChatGroup || c.Chat().Type == tb.ChatSuperGroup {
		fh.RegisterGroup(c.Chat().ID)
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

// Polish month data structure
type polishMonth struct {
	normalized string     // normalized form (genitive case)
	timeMonth  time.Month // Go time.Month constant
}

// polishMonths maps various Polish month forms to their normalized data
var polishMonths = map[string]polishMonth{
	// January
	"stycznia": {"stycznia", time.January},
	"styczeń":  {"stycznia", time.January},
	"styczen":  {"stycznia", time.January},
	// February
	"lutego": {"lutego", time.February},
	"luty":   {"lutego", time.February},
	// March
	"marca":  {"marca", time.March},
	"marzec": {"marca", time.March},
	// April
	"kwietnia": {"kwietnia", time.April},
	"kwiecień": {"kwietnia", time.April},
	"kwiecien": {"kwietnia", time.April},
	// May
	"maja": {"maja", time.May},
	"maj":  {"maja", time.May},
	// June
	"czerwca":  {"czerwca", time.June},
	"czerwiec": {"czerwca", time.June},
	// July
	"lipca":  {"lipca", time.July},
	"lipiec": {"lipca", time.July},
	// August
	"sierpnia": {"sierpnia", time.August},
	"sierpień": {"sierpnia", time.August},
	"sierpien": {"sierpnia", time.August},
	// September
	"września": {"września", time.September},
	"wrzesień": {"września", time.September},
	"wrzesien": {"września", time.September},
	// October
	"października": {"października", time.October},
	"październik":  {"października", time.October},
	"pazdziernik":  {"października", time.October},
	// November
	"listopada": {"listopada", time.November},
	"listopad":  {"listopada", time.November},
	// December
	"grudnia":  {"grudnia", time.December},
	"grudzień": {"grudnia", time.December},
	"grudzien": {"grudnia", time.December},
}

// escapeMarkdown escapes special Markdown characters
func escapeMarkdown(text string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"`", "\\`",
	)
	return replacer.Replace(text)
}

// normalizeMonthName extracts and normalizes Polish month name
func normalizeMonthName(monthStr string) string {
	monthName := strings.TrimSpace(monthStr)
	if strings.Contains(monthName, " ") {
		parts := strings.Split(monthName, " ")
		monthName = parts[0]
	}
	monthName = strings.ToLower(monthName)

	if monthData, exists := polishMonths[monthName]; exists {
		return monthData.normalized
	}
	return monthName // return original if not found
}

// parseMonthToTime converts Polish month name to time.Month
func parseMonthToTime(monthStr string) (time.Month, bool) {
	monthName := strings.TrimSpace(monthStr)
	if strings.Contains(monthName, " ") {
		parts := strings.Split(monthName, " ")
		monthName = parts[0]
	}
	monthName = strings.ToLower(monthName)

	if monthData, exists := polishMonths[monthName]; exists {
		return monthData.timeMonth, true
	}
	return 0, false
}

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

		// Normalize Polish month name
		normalizedMonth := normalizeMonthName(event.Month)

		if timeStr != "" {
			result.WriteString(fmt.Sprintf("🕒 Wydarzenie odbędzie się %s %s %s", escapeMarkdown(event.Day), escapeMarkdown(normalizedMonth), escapeMarkdown(timeStr)))
		} else {
			result.WriteString(fmt.Sprintf("🕒 Wydarzenie odbędzie się %s %s", escapeMarkdown(event.Day), escapeMarkdown(normalizedMonth)))
		}
	}

	result.WriteString(fmt.Sprintf("\n\nWydarzenie %d z %d", index+1, total))
	return result.String()
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

	// Check the user is the owner of this message
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

// sendEventSubscriptionConfirmation sends a detailed confirmation message to the user
func (fh *FeatureHandler) sendEventSubscriptionConfirmation(chat *tb.Chat, eventID string) error {
	// Find the event details
	fh.eventsCacheMu.RLock()
	var currentEvent *EventData
	for i := range fh.eventsCache {
		if fh.eventsCache[i].GetEventID() == eventID {
			currentEvent = &fh.eventsCache[i]
			break
		}
	}
	fh.eventsCacheMu.RUnlock()

	if currentEvent == nil {
		// If event not found in the cache, send simple confirmation
		_, err := fh.bot.Send(chat, "✅ Ты успешно подписался на событие.")
		return err
	}

	// Format event time info
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

	_, err := fh.bot.Send(chat, confirmText, markup)
	return err
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

	// Check for pending activation
	fh.pendingActivationsMu.Lock()
	eventID, hasPending := fh.pendingActivations[userID]
	if hasPending {
		delete(fh.pendingActivations, userID)
	}
	fh.pendingActivationsMu.Unlock()

	if hasPending {
		// Register user's interest in the event
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

		err := fh.sendEventSubscriptionConfirmation(c.Chat(), eventID)

		logger.Info("User subscribed to event via start", logrus.Fields{
			"user_id":  userID,
			"event_id": eventID,
		})

		return err
	}

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

	// Check for pending activation
	fh.pendingActivationsMu.Lock()
	eventID, hasPending := fh.pendingActivations[userID]
	if hasPending {
		delete(fh.pendingActivations, userID)
	}
	fh.pendingActivationsMu.Unlock()

	if hasPending {
		// Register user's interest in the event
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

		err := fh.sendEventSubscriptionConfirmation(c.Chat(), eventID)

		logger.Info("User subscribed to event via message", logrus.Fields{
			"user_id":  userID,
			"event_id": eventID,
		})

		return err
	}

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

// RegisterGroup registers a group chat for event broadcasting
func (fh *FeatureHandler) RegisterGroup(chatID int64) {
	fh.registeredGroupsMu.Lock()
	fh.registeredGroups[chatID] = true
	fh.registeredGroupsMu.Unlock()

	logger.Info("Group registered for event broadcasts", logrus.Fields{
		"chat_id": chatID,
	})
}

// StartEventBroadcaster starts hourly event checker
func (fh *FeatureHandler) StartEventBroadcaster() {
	ticker := time.NewTicker(1 * time.Hour)

	// Initial check and logging
	go func() {
		fh.logUpcomingEvents()
		fh.checkAndBroadcastEvents()
	}()

	go func() {
		for range ticker.C {
			fh.checkAndBroadcastEvents()
		}
	}()

	logger.Info("Event broadcaster started (checking every hour)")
}

// logUpcomingEvents logs the next 10 upcoming events and their broadcast times
func (fh *FeatureHandler) logUpcomingEvents() {
	// Fetch latest events
	err := fh.fetchEventsFromWebsite()
	if err != nil {
		logger.Error("Failed to fetch events for initial logging", err, nil)
		return
	}

	fh.eventsCacheMu.RLock()
	events := fh.eventsCache
	fh.eventsCacheMu.RUnlock()

	if len(events) == 0 {
		logger.Info("No events found on the website")
		return
	}

	logger.Info("UPCOMING EVENTS OVERVIEW:")

	now := time.Now()
	eventsToShow := 10
	if len(events) < eventsToShow {
		eventsToShow = len(events)
	}

	for i, event := range events[:eventsToShow] {
		eventDate := fh.parseEventDate(event)

		// Format event info
		eventInfo := fmt.Sprintf("Event %d: %s", i+1, event.Title)

		if !eventDate.IsZero() {
			daysDiff := int(eventDate.Sub(now).Hours() / 24)
			broadcastDate := eventDate.AddDate(0, 0, -5) // 5 days before event

			eventInfo += fmt.Sprintf(" | Date: %s (%d days from now)",
				eventDate.Format("2006-01-02"), daysDiff)

			if daysDiff >= 5 {
				eventInfo += fmt.Sprintf(" | Will broadcast: %s",
					broadcastDate.Format("2006-01-02 15:04"))
			} else if daysDiff >= 0 {
				eventInfo += " | Too close to broadcast (less than 5 days)"
			} else {
				eventInfo += " | Event already passed"
			}
		} else {
			eventInfo += " | Date: Unable to parse"
		}

		logger.Info(eventInfo)
	}

	// Show registered groups count
	fh.registeredGroupsMu.RLock()
	groupCount := len(fh.registeredGroups)
	fh.registeredGroupsMu.RUnlock()

	logger.Info(fmt.Sprintf("Total registered groups for broadcasting: %d", groupCount))

	// Show next 5 days events
	targetDates := []time.Time{}
	for i := 1; i <= 5; i++ {
		targetDates = append(targetDates, now.AddDate(0, 0, i))
	}

	logger.Info("EVENTS IN NEXT 5 DAYS:")
	for i, targetDate := range targetDates {
		dayEvents := []string{}

		for _, event := range events {
			eventDate := fh.parseEventDate(event)
			if !eventDate.IsZero() &&
				eventDate.Year() == targetDate.Year() &&
				eventDate.Month() == targetDate.Month() &&
				eventDate.Day() == targetDate.Day() {
				dayEvents = append(dayEvents, event.Title)
			}
		}

		dayInfo := fmt.Sprintf("Day +%d (%s): ", i, targetDate.Format("2006-01-02"))
		if len(dayEvents) > 0 {
			dayInfo += fmt.Sprintf("%d events - %s", len(dayEvents), strings.Join(dayEvents, ", "))
		} else {
			dayInfo += "No events"
		}

		logger.Info(dayInfo)
	}
}

// checkAndBroadcastEvents checks for events happening in 5 days and broadcasts them
func (fh *FeatureHandler) checkAndBroadcastEvents() {
	// Fetch latest events
	err := fh.fetchEventsFromWebsite()
	if err != nil {
		logger.Error("Failed to fetch events for broadcasting", err, nil)
		return
	}

	fh.eventsCacheMu.RLock()
	events := fh.eventsCache
	fh.eventsCacheMu.RUnlock()

	if len(events) == 0 {
		return
	}

	// Calculate target date (5 days from now)
	targetDate := time.Now().AddDate(0, 0, 5)

	// Find events happening in 5 days
	for _, event := range events {
		eventDate := fh.parseEventDate(event)
		if eventDate.IsZero() {
			continue
		}

		// Check if event is on target date (same day)
		if eventDate.Year() == targetDate.Year() &&
			eventDate.Month() == targetDate.Month() &&
			eventDate.Day() == targetDate.Day() {

			eventID := event.GetEventID()

			// Check if already broadcasted
			fh.broadcastedEventsMu.Lock()
			if fh.broadcastedEvents[eventID] {
				fh.broadcastedEventsMu.Unlock()
				continue
			}
			fh.broadcastedEvents[eventID] = true
			fh.broadcastedEventsMu.Unlock()

			// Broadcast this event
			fh.broadcastEventToGroups(event)
		}
	}
}

// parseEventDate parses event date from Day and Month fields
func (fh *FeatureHandler) parseEventDate(event EventData) time.Time {
	if event.Day == "" || event.Month == "" {
		return time.Time{}
	}

	// Parse day
	var day int
	_, err := fmt.Sscanf(event.Day, "%d", &day)
	if err != nil {
		return time.Time{}
	}

	// Parse month
	month, ok := parseMonthToTime(event.Month)
	if !ok {
		return time.Time{}
	}

	// Use current year
	year := time.Now().Year()

	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}

// broadcastEventToGroups sends event to all registered groups
func (fh *FeatureHandler) broadcastEventToGroups(event EventData) {
	eventID := event.GetEventID()
	eventText := fh.formatBroadcastEventText(event)

	// Create "Интересует" button
	interestedBtn := tb.InlineButton{
		Unique: "broadcast_interested",
		Text:   "Интересует 🔔",
		Data:   fmt.Sprintf("bcast_%s", eventID),
	}

	markup := &tb.ReplyMarkup{
		InlineKeyboard: [][]tb.InlineButton{{interestedBtn}},
	}

	// Get all registered groups
	fh.registeredGroupsMu.RLock()
	groupIDs := make([]int64, 0, len(fh.registeredGroups))
	for chatID := range fh.registeredGroups {
		groupIDs = append(groupIDs, chatID)
	}
	fh.registeredGroupsMu.RUnlock()

	logger.Info("Broadcasting event to groups", logrus.Fields{
		"event_id":    eventID,
		"event_title": event.Title,
		"groups":      len(groupIDs),
	})

	// Broadcast to all groups
	for _, chatID := range groupIDs {
		chat := &tb.Chat{ID: chatID}
		_, err := fh.bot.Send(chat, eventText, markup, tb.ModeMarkdown)
		if err != nil {
			logger.Error("Failed to broadcast event to group", err, logrus.Fields{
				"chat_id":  chatID,
				"event_id": eventID,
			})
		} else {
			logger.Info("Event broadcasted successfully", logrus.Fields{
				"chat_id":  chatID,
				"event_id": eventID,
			})
		}
	}
}

// formatBroadcastEventText formats event for group broadcast
func (fh *FeatureHandler) formatBroadcastEventText(event EventData) string {
	var result strings.Builder

	result.WriteString(fmt.Sprintf("📰 %s\n\n", escapeMarkdown(event.Title)))

	if event.Description != "" {
		desc := strings.ReplaceAll(event.Description, "\n\n\n", "\n\n")
		desc = strings.TrimSpace(desc)

		// Limit description for broadcast
		if len(desc) > 500 {
			desc = desc[:500] + "..."
		}

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

		// Normalize Polish month name
		normalizedMonth := normalizeMonthName(event.Month)

		if timeStr != "" {
			result.WriteString(fmt.Sprintf("🕒 Wydarzenie odbędzie się %s %s %s", escapeMarkdown(event.Day), escapeMarkdown(normalizedMonth), escapeMarkdown(timeStr)))
		} else {
			result.WriteString(fmt.Sprintf("🕒 Wydarzenie odbędzie się %s %s", escapeMarkdown(event.Day), escapeMarkdown(normalizedMonth)))
		}
	}

	return result.String()
}

// HandleBroadcastInterested handles "Интересует" button from broadcast
func (fh *FeatureHandler) HandleBroadcastInterested(c tb.Context) error {
	if c.Callback() == nil || c.Sender() == nil {
		return nil
	}

	eventID := strings.TrimPrefix(c.Callback().Data, "bcast_")
	userID := c.Sender().ID

	// Check if already subscribed
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

	// Try to send confirmation to private chat first (regardless of activation status)
	privateChat := &tb.Chat{ID: userID}
	err := fh.sendEventSubscriptionConfirmation(privateChat, eventID)

	if err != nil {
		// Failed to send message, bot is blocked or chat was deleted
		logger.Warn("Failed to send message to user, requesting activation", logrus.Fields{
			"user_id":  userID,
			"event_id": eventID,
			"error":    err.Error(),
		})

		// Mark as not activated
		fh.activatedUsersMu.Lock()
		delete(fh.activatedUsers, userID)
		fh.activatedUsersMu.Unlock()

		// Add to pending activations
		fh.pendingActivationsMu.Lock()
		fh.pendingActivations[userID] = eventID
		fh.pendingActivationsMu.Unlock()

		warnMsg, _ := fh.bot.Send(c.Chat(), fmt.Sprintf(
			"⚠️ %s, для подписки на событие активируй бота в личных сообщениях.",
			fh.adminHandler.GetUserDisplayName(c.Sender()),
		))

		if fh.adminHandler != nil {
			fh.adminHandler.DeleteAfter(warnMsg, 15*time.Second)
		}

		return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{})
	}

	// Message sent successfully, subscribe user to event
	fh.activatedUsersMu.Lock()
	fh.activatedUsers[userID] = true
	fh.activatedUsersMu.Unlock()

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

	logger.Info("User subscribed to broadcast event", logrus.Fields{
		"user_id":  userID,
		"event_id": eventID,
	})

	return fh.bot.Respond(c.Callback(), &tb.CallbackResponse{})
}
