package features

import (
	"fmt"
	"log"
	"strings"
	"time"

	"UEPB/interfaces"

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
	Btns        struct {
		Student, Guest, Ads tb.InlineButton
	}
	adminHandler interfaces.AdminHandlerInterface
}

// NewFeatureHandler creates a new feature handler
func NewFeatureHandler(bot *tb.Bot, state interfaces.UserState, quiz interfaces.QuizInterface, blacklist interfaces.BlacklistInterface, adminChatID int64, violations map[int64]int, adminHandler interfaces.AdminHandlerInterface, btns struct{ Student, Guest, Ads tb.InlineButton }) *FeatureHandler {
	return &FeatureHandler{
		bot:          bot,
		state:        state,
		quiz:         quiz,
		blacklist:    blacklist,
		adminChatID:  adminChatID,
		violations:   violations,
		Btns:         btns,
		adminHandler: adminHandler,
	}
}

// OnlyNewbies middleware to restrict handlers to newbies only
func (fh *FeatureHandler) OnlyNewbies(handler func(tb.Context) error) func(tb.Context) error {
	return func(c tb.Context) error {
		if c.Sender() == nil || !fh.state.IsNewbie(int(c.Sender().ID)) {
			if cb := c.Callback(); cb != nil {
				_ = fh.bot.Respond(cb, &tb.CallbackResponse{
					Text:      "Ты не можешь нажимать на чужие кнопки",
					ShowAlert: false,
				})
			}
			return nil
		}
		return handler(c)
	}
}

// SendOrEdit sends a new message or edits an existing one
func (fh *FeatureHandler) SendOrEdit(chat *tb.Chat, msg *tb.Message, text string, rm *tb.ReplyMarkup) *tb.Message {
	var err error
	if msg == nil {
		msg, err = fh.bot.Send(chat, text, rm)
	} else {
		msg, err = fh.bot.Edit(msg, text, rm)
	}
	if err != nil {
		log.Println("Message error:", err)
		return nil
	}
	return msg
}

// SetUserRestriction sets user permissions in chat
func (fh *FeatureHandler) SetUserRestriction(chat *tb.Chat, user *tb.User, allowAll bool) {
	var rights tb.Rights
	if allowAll {
		rights = tb.Rights{
			CanSendMessages:   true,
			CanSendAudios:     true,
			CanSendDocuments:  true,
			CanSendPhotos:     true,
			CanSendVideos:     true,
			CanSendVideoNotes: true,
			CanSendVoiceNotes: true,
			CanSendPolls:      true,
		}
	} else {
		rights = tb.Rights{
			CanSendMessages: false,
		}
	}

	if err := fh.bot.Restrict(chat, &tb.ChatMember{
		User:   user,
		Rights: rights,
	}); err != nil {
		log.Println("Restrict error:", err)
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

		// Log to admin chat
		logMsg := fmt.Sprintf("👤 Новый участник вошёл в чат\n\n"+
			"Пользователь: %s\n"+
			"Чат: %s (ID: %d)",
			fh.adminHandler.GetUserDisplayName(u),
			c.Chat().Title,
			c.Chat().ID)
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
	logMsg := fmt.Sprintf("👋 Участник покинул чат\n\n"+
		"Пользователь: %s\n"+
		"Чат: %s (ID: %d)",
		fh.adminHandler.GetUserDisplayName(user),
		c.Chat().Title,
		c.Chat().ID)
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
	msg := fh.SendOrEdit(c.Chat(), c.Message(), "✅ Теперь можно писать в чат. Задай интересующий вопрос.", nil)
	fh.adminHandler.DeleteAfter(msg, 5*time.Second)
	return nil
}

// HandleAds handles ads button click
func (fh *FeatureHandler) HandleAds(c tb.Context) error {
	msg := fh.SendOrEdit(c.Chat(), c.Message(), "📢 Мы открыты к рекламе.\n\nНапиши @chathlp и опиши, что хочешь предложить.", nil)
	fh.adminHandler.DeleteAfter(msg, 10*time.Second)

	// Log to admin chat
	logMsg := fmt.Sprintf("📢 Пользователь выбрал рекламу\n\n"+
		"Пользователь: %s\n"+
		"Чат: %s (ID: %d)",
		fh.adminHandler.GetUserDisplayName(c.Sender()),
		c.Chat().Title,
		c.Chat().ID)
	fh.adminHandler.LogToAdmin(logMsg)

	return nil
}

// HandlePing handles /ping command
func (fh *FeatureHandler) HandlePing(c tb.Context) error {
	start := time.Now()

	// Send the response and measure time
	msg, err := fh.bot.Send(c.Chat(), "🏓 Понг!")
	if err != nil {
		log.Printf("[ERROR] Failed to send ping response: %v", err)
		return err
	}

	// Calculate response time
	responseTime := time.Since(start)
	responseMs := int(responseTime.Nanoseconds() / 1000000) // Convert to milliseconds

	// Edit the message with response time
	finalText := fmt.Sprintf("🏓 Понг! (%d мс)", responseMs)
	_, err = fh.bot.Edit(msg, finalText)
	if err != nil {
		log.Printf("[ERROR] Failed to edit ping message: %v", err)
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
			fh.adminHandler.DeleteAfter(msg, 3*time.Second)

			// Log successful verification to admin chat
			logMsg := fmt.Sprintf("✅ Пользователь успешно прошёл верификацию\n\n"+
				"Пользователь: %s\n"+
				"Правильных ответов: %d/%d\n"+
				"Чат: %s (ID: %d)",
				fh.adminHandler.GetUserDisplayName(c.Sender()),
				totalCorrect,
				totalQuestions,
				c.Chat().Title,
				c.Chat().ID)
			fh.adminHandler.LogToAdmin(logMsg)
		} else {
			msg := fh.SendOrEdit(c.Chat(), c.Message(), "❌ Не удалось подтвердить статус студента.", nil)
			fh.adminHandler.DeleteAfter(msg, 5*time.Second)

			// Log failed verification to admin chat
			logMsg := fmt.Sprintf("❌ Пользователь не прошёл верификацию\n\n"+
				"Пользователь: %s\n"+
				"Правильных ответов: %d/%d\n"+
				"Чат: %s (ID: %d)",
				fh.adminHandler.GetUserDisplayName(c.Sender()),
				totalCorrect,
				totalQuestions,
				c.Chat().Title,
				c.Chat().ID)
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

	log.Printf("[DEBUG] Checking message from user %d: '%s'", msg.Sender.ID, msg.Text)

	if fh.blacklist.CheckMessage(msg.Text) {
		fh.adminHandler.AddViolation(msg.Sender.ID)
		violationCount := fh.adminHandler.GetViolations(msg.Sender.ID)

		// Delete the message
		if err := fh.bot.Delete(msg); err != nil {
			log.Printf("[ERROR] Failed to delete message %d from %d: %v", msg.ID, msg.Sender.ID, err)
		} else {
			log.Printf("[DEBUG] Deleted message %d from %d (violation #%d)", msg.ID, msg.Sender.ID, violationCount)
		}

		// If it's their second violation, ban
		if violationCount >= 2 {
			if err := fh.adminHandler.BanUser(c.Chat(), msg.Sender); err != nil {
				log.Printf("[ERROR] Failed to ban user %d: %v", msg.Sender.ID, err)
			} else {
				log.Printf("[DEBUG] Banned user %d for repeated violations", msg.Sender.ID)

				fh.adminHandler.ClearViolations(msg.Sender.ID)

				// Log to admin chat
				logMsg := fmt.Sprintf("🔨 Автоматический бан за банворды\n\n"+
					"Забанен: %s\n"+
					"Количество нарушений: %d\n"+
					"Чат: %s (ID: %d)",
					fh.adminHandler.GetUserDisplayName(msg.Sender),
					violationCount,
					c.Chat().Title,
					c.Chat().ID)
				fh.adminHandler.LogToAdmin(logMsg)
			}
		} else {
			// Warning if it's their first violation
			warningMsg, _ := fh.bot.Send(c.Chat(), fmt.Sprintf("⚠️ %s, сообщение удалено. При повторном нарушении будет бан.", fh.adminHandler.GetUserDisplayName(msg.Sender)))
			fh.adminHandler.DeleteAfter(warningMsg, 15*time.Second)

			// Log to admin chat
			logMsg := fmt.Sprintf("⚠️ Обнаружено нарушение\n\n"+
				"Пользователь: %s\n"+
				"Нарушение: #%d\n"+
				"Сообщение: `%s`\n"+
				"Чат: %s (ID: %d)",
				fh.adminHandler.GetUserDisplayName(msg.Sender),
				violationCount,
				msg.Text,
				c.Chat().Title,
				c.Chat().ID)
			fh.adminHandler.LogToAdmin(logMsg)
		}
	}
	return nil
}
