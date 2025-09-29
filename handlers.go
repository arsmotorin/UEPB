package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	tb "gopkg.in/telebot.v4"
)

type Handler struct {
	bot       *tb.Bot
	state     *State
	quiz      Quiz
	blacklist *Blacklist
	Btns      struct {
		Student, Guest, Ads tb.InlineButton
	}
}

func NewHandler(bot *tb.Bot, state *State, quiz Quiz) *Handler {
	h := &Handler{
		bot:       bot,
		state:     state,
		quiz:      quiz,
		blacklist: NewBlacklist("blacklist.json"),
	}
	h.Btns.Student, h.Btns.Guest, h.Btns.Ads = StudentButton(), GuestButton(), AdsButton()
	return h
}

func (h *Handler) Register() {
	h.bot.Handle(tb.OnUserJoined, h.handleUserJoined)
	h.bot.Handle(&h.Btns.Student, h.onlyNewbies(h.handleStudent))
	h.bot.Handle(&h.Btns.Guest, h.onlyNewbies(h.handleGuest))
	h.bot.Handle(&h.Btns.Ads, h.onlyNewbies(h.handleAds))
	h.registerQuizHandlers()

	h.bot.Handle("/banword", h.handleBan)
	h.bot.Handle("/unbanword", h.handleUnban)
	h.bot.Handle("/listbanword", h.handleListBan)
	h.bot.Handle(tb.OnText, h.filterMessage)
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
	}
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
		} else {
			msg := h.sendOrEdit(c.Chat(), c.Message(), "❌ Не удалось подтвердить статус студента.", nil)
			h.deleteAfter(msg, 5*time.Second)
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

	member, err := h.bot.ChatMemberOf(c.Chat(), c.Sender())
	if err != nil {
		log.Printf("[ERROR] Failed to check member rights: %v", err)
		msg, _ := h.bot.Send(c.Chat(), "❌ Не удалось проверить права: "+err.Error())
		h.deleteAfter(msg, 10*time.Second)
		return nil
	}

	log.Printf("[DEBUG] User %d has role: %s", c.Sender().ID, member.Role)

	if member.Role != tb.Administrator && member.Role != tb.Creator {
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
	return nil
}

func (h *Handler) handleUnban(c tb.Context) error {
	log.Printf("[DEBUG] /unbanword command received from user %d", c.Sender().ID)

	if c.Message() == nil || c.Sender() == nil {
		return nil
	}

	member, err := h.bot.ChatMemberOf(c.Chat(), c.Sender())
	if err != nil {
		log.Printf("[ERROR] Failed to check member rights: %v", err)
		msg, _ := h.bot.Send(c.Chat(), "❌ Не удалось проверить права: "+err.Error())
		h.deleteAfter(msg, 10*time.Second)
		return nil
	}
	if member.Role != tb.Administrator && member.Role != tb.Creator {
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
	} else {
		text = "❌ Такого словосочетания нет в списке."
		log.Printf("[DEBUG] Phrase not found in blacklist: %v", args[1:])
	}
	msg, _ := h.bot.Send(c.Chat(), text)
	h.deleteAfter(msg, 10*time.Second)
	return nil
}

func (h *Handler) handleListBan(c tb.Context) error {
	phrases := h.blacklist.List()
	if len(phrases) == 0 {
		msg, _ := h.bot.Send(c.Chat(), "📭 Список пуст.")
		h.deleteAfter(msg, 10*time.Second)
		return nil
	}

	var sb strings.Builder
	sb.WriteString("🚫 Запрещённые словосочетания:\n")
	for i, p := range phrases {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.Join(p, " ")))
	}

	msg, _ := h.bot.Send(c.Chat(), sb.String())
	h.deleteAfter(msg, 10*time.Second)
	return nil
}

func (h *Handler) filterMessage(c tb.Context) error {
	msg := c.Message()
	if msg == nil || msg.Sender == nil {
		return nil
	}

	// Don't filter admin messages
	member, err := h.bot.ChatMemberOf(c.Chat(), msg.Sender)
	if err == nil && (member.Role == tb.Administrator || member.Role == tb.Creator) {
		return nil
	}

	log.Printf("[DEBUG] Checking message from user %d: '%s'", msg.Sender.ID, msg.Text)

	if h.blacklist.CheckMessage(msg.Text) {
		if err := h.bot.Delete(msg); err != nil {
			log.Printf("[ERROR] Failed to delete message %d from %d: %v", msg.ID, msg.Sender.ID, err)
		} else {
			log.Printf("[DEBUG] Deleted message %d from %d", msg.ID, msg.Sender.ID)
		}
	}
	return nil
}
