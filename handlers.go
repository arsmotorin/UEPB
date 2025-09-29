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
		blacklist: NewBlacklist(),
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

	h.bot.Handle("/ban", h.handleBan)
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
			log.Println("Delete error:", err)
		}
	}()
}

func (h *Handler) setUserRestriction(chat *tb.Chat, user *tb.User, canSend bool) {
	if err := h.bot.Restrict(chat, &tb.ChatMember{
		User:   user,
		Rights: tb.Rights{CanSendMessages: canSend},
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
			msg := h.sendOrEdit(c.Chat(), c.Message(), "❌ Не удалось подтвердить статус студента(ки).", nil)
			h.deleteAfter(msg, 5*time.Second)
		}
		h.state.Reset(userID)
		return nil
	}
}

// Blacklist
func (h *Handler) handleBan(c tb.Context) error {
	if c.Message() == nil || c.Sender() == nil {
		return nil
	}
	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		_, _ = h.bot.Send(c.Chat(), "Используй: /ban слово1 [слово2 ...]")
		return nil
	}
	h.blacklist.AddPhrase(args[1:])
	_, _ = h.bot.Send(c.Chat(), "✅ Добавлено запрещённое словосочетание: "+strings.Join(args[1:], " "))
	return nil
}

func (h *Handler) filterMessage(c tb.Context) error {
	msg := c.Message()
	if msg == nil || msg.Sender == nil {
		return nil
	}
	if h.blacklist.CheckMessage(msg.Text) {
		_ = h.bot.Delete(msg)
		return nil
	}
	return nil
}
