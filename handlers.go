package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	tb "gopkg.in/telebot.v4"
)

type Handler struct {
	bot         *tb.Bot
	state       *State
	quiz        Quiz
	blacklist   *Blacklist
	adminChatID int64
	violations  map[int64]int
	Btns        struct {
		Student, Guest, Ads tb.InlineButton
	}
}

func NewHandler(bot *tb.Bot, state *State, quiz Quiz, adminChatID int64) *Handler {
	h := &Handler{
		bot:         bot,
		state:       state,
		quiz:        quiz,
		blacklist:   NewBlacklist("blacklist.json"),
		adminChatID: adminChatID,
		violations:  make(map[int64]int),
	}
	h.Btns.Student, h.Btns.Guest, h.Btns.Ads = StudentButton(), GuestButton(), AdsButton()
	return h
}

func (h *Handler) Register() {
	h.bot.Handle(tb.OnUserJoined, h.handleUserJoined)
	h.bot.Handle(tb.OnUserLeft, h.handleUserLeft)
	h.bot.Handle(&h.Btns.Student, h.onlyNewbies(h.handleStudent))
	h.bot.Handle(&h.Btns.Guest, h.onlyNewbies(h.handleGuest))
	h.bot.Handle(&h.Btns.Ads, h.onlyNewbies(h.handleAds))
	h.registerQuizHandlers()

	h.bot.Handle("/banword", h.handleBan)
	h.bot.Handle("/unbanword", h.handleUnban)
	h.bot.Handle("/listbanword", h.handleListBan)
	h.bot.Handle("/spamban", h.handleSpamBan)
	h.bot.Handle("/ping", h.handlePing)
	h.bot.Handle(tb.OnText, h.filterMessage)

	// Set bot commands for better UX
	h.setBotCommands()
}

func (h *Handler) setBotCommands() {
	commands := []tb.Command{
		{Text: "ping", Description: "Проверить отклик бота"},
		{Text: "banword", Description: "Добавить запрещённое слово"},
		{Text: "unbanword", Description: "Удалить запрещённое слово"},
		{Text: "listbanword", Description: "Показать список запрещённых слов"},
		{Text: "spamban", Description: "Забанить пользователя за спам (в ответ на сообщение)"},
	}

	if err := h.bot.SetCommands(commands); err != nil {
		log.Printf("[ERROR] Failed to set bot commands: %v", err)
	}
}

func (h *Handler) logToAdmin(message string) {
	adminChat := &tb.Chat{ID: h.adminChatID}
	if _, err := h.bot.Send(adminChat, message); err != nil {
		log.Printf("[ERROR] Failed to send admin log: %v", err)
	}
}

func (h *Handler) onlyNewbies(handler func(tb.Context) error) func(tb.Context) error {
	return func(c tb.Context) error {
		if c.Sender() == nil || !h.state.IsNewbie(int(c.Sender().ID)) {
			if cb := c.Callback(); cb != nil {
				_ = h.bot.Respond(cb, &tb.CallbackResponse{
					Text:      "Ты не можешь нажимать на чужие кнопки",
					ShowAlert: false,
				})
			}
			return nil
		}
		return handler(c)
	}
}

// General functions
func (h *Handler) sendOrEdit(chat *tb.Chat, msg *tb.Message, text string, rm *tb.ReplyMarkup) *tb.Message {
	var err error
	if msg == nil {
		msg, err = h.bot.Send(chat, text, rm)
	} else {
		msg, err = h.bot.Edit(msg, text, rm)
	}
	if err != nil {
		log.Println("Message error:", err)
		return nil
	}
	return msg
}

func (h *Handler) deleteAfter(m *tb.Message, d time.Duration) {
	if m == nil {
		return
	}
	go func() {
		time.Sleep(d)
		if err := h.bot.Delete(m); err != nil {
			log.Printf("[ERROR] Failed to delete message %d: %v", m.ID, err)
		}
	}()
}

func (h *Handler) setUserRestriction(chat *tb.Chat, user *tb.User, allowAll bool) {
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

	if err := h.bot.Restrict(chat, &tb.ChatMember{
		User:   user,
		Rights: rights,
	}); err != nil {
		log.Println("Restrict error:", err)
	}
}

func (h *Handler) banUser(chat *tb.Chat, user *tb.User) error {
	member := &tb.ChatMember{
		User:   user,
		Rights: tb.Rights{},
	}

	return h.bot.Ban(chat, member)
}

func getNewUsers(msg *tb.Message) []*tb.User {
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

func (h *Handler) getUserDisplayName(user *tb.User) string {
	if user.Username != "" {
		return "@" + user.Username
	}
	name := user.FirstName
	if user.LastName != "" {
		name += " " + user.LastName
	}
	return name + fmt.Sprintf(" (ID: %d)", user.ID)
}

func (h *Handler) isAdmin(chat *tb.Chat, user *tb.User) bool {
	member, err := h.bot.ChatMemberOf(chat, user)
	if err != nil {
		log.Printf("[ERROR] Failed to check member rights: %v", err)
		return false
	}
	return member.Role == tb.Administrator || member.Role == tb.Creator
}

// Handlers
func (h *Handler) handleUserJoined(c tb.Context) error {
	if c.Message() == nil || c.Chat() == nil {
		return nil
	}
	users := getNewUsers(c.Message())
	keyboard := &tb.ReplyMarkup{
		InlineKeyboard: [][]tb.InlineButton{{h.Btns.Student}, {h.Btns.Guest}, {h.Btns.Ads}},
	}

	for _, u := range users {
		h.state.SetNewbie(int(u.ID))
		h.setUserRestriction(c.Chat(), u, false)

		text := "👋 Привет!\n\nВыбери, что тебя интересует, используя кнопки ниже."
		if u.Username != "" {
			text = fmt.Sprintf("👋 Привет, @%s!\n\nВыбери, что тебя интересует, используя кнопки ниже.", u.Username)
		}
		msg := h.sendOrEdit(c.Chat(), nil, text, keyboard)
		h.deleteAfter(msg, 2*time.Minute)
		h.state.InitUser(int(u.ID))

		// Log to admin chat
		logMsg := fmt.Sprintf("👤 Новый участник вошёл в чат\n\n"+
			"Пользователь: %s\n"+
			"Чат: %s (ID: %d)",
			h.getUserDisplayName(u),
			c.Chat().Title,
			c.Chat().ID)
		h.logToAdmin(logMsg)
	}
	return nil
}

func (h *Handler) handleUserLeft(c tb.Context) error {
	if c.Message() == nil || c.Chat() == nil || c.Message().UserLeft == nil {
		return nil
	}

	user := c.Message().UserLeft
	h.state.ClearNewbie(int(user.ID))

	// Reset
	delete(h.violations, user.ID)

	// Log to admin chat
	logMsg := fmt.Sprintf("👋 Участник покинул чат\n\n"+
		"Пользователь: %s\n"+
		"Чат: %s (ID: %d)",
		h.getUserDisplayName(user),
		c.Chat().Title,
		c.Chat().ID)
	h.logToAdmin(logMsg)

	return nil
}

func (h *Handler) handleStudent(c tb.Context) error {
	h.state.InitUser(int(c.Sender().ID))
	q := h.quiz.Questions[0]
	_ = h.sendOrEdit(c.Chat(), c.Message(), q.Text, &tb.ReplyMarkup{InlineKeyboard: [][]tb.InlineButton{q.Buttons}})
	return nil
}

func (h *Handler) handleGuest(c tb.Context) error {
	h.setUserRestriction(c.Chat(), c.Sender(), true)
	h.state.ClearNewbie(int(c.Sender().ID))
	msg := h.sendOrEdit(c.Chat(), c.Message(), "✅ Теперь можно писать в чат. Задай интересующий вопрос.", nil)
	h.deleteAfter(msg, 5*time.Second)
	return nil
}

func (h *Handler) handleAds(c tb.Context) error {
	msg := h.sendOrEdit(c.Chat(), c.Message(), "📢 Мы открыты к рекламе.\n\nНапиши @chathlp и опиши, что хочешь предложить.", nil)
	h.deleteAfter(msg, 10*time.Second)

	// Log to admin chat
	logMsg := fmt.Sprintf("📢 Пользователь выбрал рекламу\n\n"+
		"Пользователь: %s\n"+
		"Чат: %s (ID: %d)",
		h.getUserDisplayName(c.Sender()),
		c.Chat().Title,
		c.Chat().ID)
	h.logToAdmin(logMsg)

	return nil
}

func (h *Handler) handlePing(c tb.Context) error {
	start := time.Now()

	// Send the response and measure time
	msg, err := h.bot.Send(c.Chat(), "🏓 Понг!")
	if err != nil {
		log.Printf("[ERROR] Failed to send ping response: %v", err)
		return err
	}

	// Calculate response time
	responseTime := time.Since(start)
	responseMs := int(responseTime.Nanoseconds() / 1000000) // Convert to milliseconds

	// Edit the message with response time
	finalText := fmt.Sprintf("🏓 Понг! (%d мс)", responseMs)
	_, err = h.bot.Edit(msg, finalText)
	if err != nil {
		log.Printf("[ERROR] Failed to edit ping message: %v", err)
	}

	return nil
}

func (h *Handler) handleSpamBan(c tb.Context) error {
	if c.Message() == nil || c.Sender() == nil {
		return nil
	}

	if !h.isAdmin(c.Chat(), c.Sender()) {
		msg, _ := h.bot.Send(c.Chat(), "⛔ Команда /spamban доступна только администрации.")
		h.deleteAfter(msg, 10*time.Second)
		return nil
	}

	// Check if the command is a reply to a message
	if c.Message().ReplyTo == nil {
		msg, _ := h.bot.Send(c.Chat(), "💡 Используй команду /spamban в ответ на сообщение пользователя, которого нужно забанить.")
		h.deleteAfter(msg, 10*time.Second)
		return nil
	}

	targetUser := c.Message().ReplyTo.Sender
	if targetUser == nil {
		msg, _ := h.bot.Send(c.Chat(), "❌ Не удалось определить пользователя для бана.")
		h.deleteAfter(msg, 10*time.Second)
		return nil
	}

	if h.isAdmin(c.Chat(), targetUser) {
		msg, _ := h.bot.Send(c.Chat(), "⛔ Нельзя забанить администратора.")
		h.deleteAfter(msg, 10*time.Second)
		return nil
	}

	if err := h.banUser(c.Chat(), targetUser); err != nil {
		log.Printf("[ERROR] Failed to ban user %d: %v", targetUser.ID, err)
		msg, _ := h.bot.Send(c.Chat(), "❌ Не удалось забанить пользователя: "+err.Error())
		h.deleteAfter(msg, 10*time.Second)
		return nil
	}

	if err := h.bot.Delete(c.Message().ReplyTo); err != nil {
		log.Printf("[ERROR] Failed to delete target message: %v", err)
	}

	// Reset the violation counter for the user
	delete(h.violations, targetUser.ID)

	msg, _ := h.bot.Send(c.Chat(), fmt.Sprintf("🔨 Пользователь %s забанен за спам.", h.getUserDisplayName(targetUser)))
	h.deleteAfter(msg, 10*time.Second)

	// Log to admin chat
	logMsg := fmt.Sprintf("🔨 Пользователь забанен за спам\n\n"+
		"Забанен: %s\n"+
		"Админ: %s\n"+
		"Чат: %s (ID: %d)",
		h.getUserDisplayName(targetUser),
		h.getUserDisplayName(c.Sender()),
		c.Chat().Title,
		c.Chat().ID)
	h.logToAdmin(logMsg)

	return nil
}

// Quiz
func (h *Handler) registerQuizHandlers() {
	for i, q := range h.quiz.Questions {
		for _, btn := range q.Buttons {
			h.bot.Handle(&btn, h.onlyNewbies(h.createQuizHandler(i, q, btn)))
		}
	}
}

func (h *Handler) createQuizHandler(i int, q Question, btn tb.InlineButton) func(tb.Context) error {
	return func(c tb.Context) error {
		userID := int(c.Sender().ID)
		if btn.Unique == q.Answer {
			h.state.IncCorrect(userID)
		}

		if i+1 < len(h.quiz.Questions) {
			next := h.quiz.Questions[i+1]
			_ = h.sendOrEdit(c.Chat(), c.Message(), next.Text, &tb.ReplyMarkup{InlineKeyboard: [][]tb.InlineButton{next.Buttons}})
			return nil
		}

		if h.state.TotalCorrect(userID) == len(h.quiz.Questions) {
			h.setUserRestriction(c.Chat(), c.Sender(), true)
			h.state.ClearNewbie(userID)
			msg := h.sendOrEdit(c.Chat(), c.Message(), "✅ Верификация пройдена! Теперь можно писать в чат.", nil)
			h.deleteAfter(msg, 3*time.Second)

			// Log successful verification to admin chat
			logMsg := fmt.Sprintf("✅ Пользователь успешно прошёл верификацию\n\n"+
				"Пользователь: %s\n"+
				"Правильных ответов: %d/%d\n"+
				"Чат: %s (ID: %d)",
				h.getUserDisplayName(c.Sender()),
				h.state.TotalCorrect(userID),
				len(h.quiz.Questions),
				c.Chat().Title,
				c.Chat().ID)
			h.logToAdmin(logMsg)
		} else {
			msg := h.sendOrEdit(c.Chat(), c.Message(), "❌ Не удалось подтвердить статус студента.", nil)
			h.deleteAfter(msg, 5*time.Second)

			// Log failed verification to admin chat
			logMsg := fmt.Sprintf("❌ Пользователь не прошёл верификацию\n\n"+
				"Пользователь: %s\n"+
				"Правильных ответов: %d/%d\n"+
				"Чат: %s (ID: %d)",
				h.getUserDisplayName(c.Sender()),
				h.state.TotalCorrect(userID),
				len(h.quiz.Questions),
				c.Chat().Title,
				c.Chat().ID)
			h.logToAdmin(logMsg)
		}
		h.state.Reset(userID)
		return nil
	}
}

// Blacklist
func (h *Handler) handleBan(c tb.Context) error {
	log.Printf("[DEBUG] /banword command received from user %d", c.Sender().ID)

	if c.Message() == nil || c.Sender() == nil {
		return nil
	}

	if !h.isAdmin(c.Chat(), c.Sender()) {
		msg, _ := h.bot.Send(c.Chat(), "⛔ Команда /banword доступна только администрации.")
		h.deleteAfter(msg, 10*time.Second)
		return nil
	}

	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		msg, _ := h.bot.Send(c.Chat(), "💡 Используй: /banword слово1 [слово2 ...]")
		h.deleteAfter(msg, 10*time.Second)
		return nil
	}

	h.blacklist.AddPhrase(args[1:])
	log.Printf("[DEBUG] Added blacklist phrase: %v", args[1:])

	msg, _ := h.bot.Send(c.Chat(), "✅ Добавлено запрещённое словосочетание: "+strings.Join(args[1:], " "))
	h.deleteAfter(msg, 10*time.Second)

	// Log to admin chat
	logMsg := fmt.Sprintf("🚫 Добавлено запрещённое слово\n\n"+
		"Админ: %s\n"+
		"Запрещённые слова: `%s`\n"+
		"Чат: %s (ID: %d)",
		h.getUserDisplayName(c.Sender()),
		strings.Join(args[1:], " "),
		c.Chat().Title,
		c.Chat().ID)
	h.logToAdmin(logMsg)

	return nil
}

func (h *Handler) handleUnban(c tb.Context) error {
	log.Printf("[DEBUG] /unbanword command received from user %d", c.Sender().ID)

	if c.Message() == nil || c.Sender() == nil {
		return nil
	}

	if !h.isAdmin(c.Chat(), c.Sender()) {
		msg, _ := h.bot.Send(c.Chat(), "⛔ Команда /unbanword доступна только администрации.")
		h.deleteAfter(msg, 10*time.Second)
		return nil
	}

	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		msg, _ := h.bot.Send(c.Chat(), "💡 Используй: /unbanword слово1 [слово2 ...]")
		h.deleteAfter(msg, 10*time.Second)
		return nil
	}

	ok := h.blacklist.RemovePhrase(args[1:])
	var text string
	if ok {
		text = "✅ Удалено запрещённое словосочетание: " + strings.Join(args[1:], " ")
		log.Printf("[DEBUG] Removed blacklist phrase: %v", args[1:])

		// Log to admin chat
		logMsg := fmt.Sprintf("✅ Удалено запрещённое слово\n\n"+
			"Админ: %s\n"+
			"Удалённые слова: `%s`\n"+
			"Чат: %s (ID: %d)",
			h.getUserDisplayName(c.Sender()),
			strings.Join(args[1:], " "),
			c.Chat().Title,
			c.Chat().ID)
		h.logToAdmin(logMsg)
	} else {
		text = "❌ Такого словосочетания нет в списке."
		log.Printf("[DEBUG] Phrase not found in blacklist: %v", args[1:])
	}
	msg, _ := h.bot.Send(c.Chat(), text)
	h.deleteAfter(msg, 10*time.Second)
	return nil
}

func (h *Handler) handleListBan(c tb.Context) error {
	if c.Chat().Type != tb.ChatPrivate || c.Chat().ID != h.adminChatID {
		return nil
	}

	phrases := h.blacklist.List()
	if len(phrases) == 0 {
		h.bot.Send(c.Chat(), "📭 Список пуст.")
		return nil
	}

	var sb strings.Builder
	sb.WriteString("🚫 Запрещённые словосочетания:\n\n")
	for i, p := range phrases {
		sb.WriteString(fmt.Sprintf("%d. `%s`\n", i+1, strings.Join(p, " ")))
	}

	h.bot.Send(c.Chat(), sb.String(), tb.ModeMarkdown)
	return nil
}

func (h *Handler) filterMessage(c tb.Context) error {
	msg := c.Message()
	if msg == nil || msg.Sender == nil {
		return nil
	}

	// Don't filter commands
	if strings.HasPrefix(msg.Text, "/") {
		return nil
	}

	// Don't filter admin messages
	member, err := h.bot.ChatMemberOf(c.Chat(), msg.Sender)
	if err == nil && (member.Role == tb.Administrator || member.Role == tb.Creator) {
		return nil
	}

	log.Printf("[DEBUG] Checking message from user %d: '%s'", msg.Sender.ID, msg.Text)

	if h.blacklist.CheckMessage(msg.Text) {
		h.violations[msg.Sender.ID]++
		violationCount := h.violations[msg.Sender.ID]

		// Delete message
		if err := h.bot.Delete(msg); err != nil {
			log.Printf("[ERROR] Failed to delete message %d from %d: %v", msg.ID, msg.Sender.ID, err)
		} else {
			log.Printf("[DEBUG] Deleted message %d from %d (violation #%d)", msg.ID, msg.Sender.ID, violationCount)
		}

		// If it's their second violation, ban
		if violationCount >= 2 {
			if err := h.banUser(c.Chat(), msg.Sender); err != nil {
				log.Printf("[ERROR] Failed to ban user %d: %v", msg.Sender.ID, err)
			} else {
				log.Printf("[DEBUG] Banned user %d for repeated violations", msg.Sender.ID)

				delete(h.violations, msg.Sender.ID)

				// Log to admin chat
				logMsg := fmt.Sprintf("🔨 Автоматический бан за банворды\n\n"+
					"Забанен: %s\n"+
					"Количество нарушений: %d\n"+
					"Чат: %s (ID: %d)",
					h.getUserDisplayName(msg.Sender),
					violationCount,
					c.Chat().Title,
					c.Chat().ID)
				h.logToAdmin(logMsg)
			}
		} else {
			// Warning if it's their first violation
			warningMsg, _ := h.bot.Send(c.Chat(), fmt.Sprintf("⚠️ %s, сообщение удалено. При повторном нарушении будет бан.", h.getUserDisplayName(msg.Sender)))
			h.deleteAfter(warningMsg, 15*time.Second)

			// Log to admin chat
			logMsg := fmt.Sprintf("⚠️ Обнаружено нарушение\n\n"+
				"Пользователь: %s\n"+
				"Нарушение: #%d\n"+
				"Сообщение: `%s`\n"+
				"Чат: %s (ID: %d)",
				h.getUserDisplayName(msg.Sender),
				violationCount,
				msg.Text,
				c.Chat().Title,
				c.Chat().ID)
			h.logToAdmin(logMsg)
		}
	}
	return nil
}
