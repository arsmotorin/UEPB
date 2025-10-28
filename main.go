package main

import (
	"os"
	"strconv"
	"time"

	"UEPB/internal/bot"
	"UEPB/internal/core"
	"UEPB/internal/i18n"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	tb "gopkg.in/telebot.v4"
)

// Handler aggregates bot dependencies
type Handler struct {
	bot            *tb.Bot
	state          core.UserState
	quiz           core.QuizInterface
	blacklist      core.BlacklistInterface
	adminChatID    int64
	violations     map[int64]int
	adminHandler   core.AdminHandlerInterface
	featureHandler core.FeatureHandlerInterface
	Btns           struct{ Student, Guest, Ads tb.InlineButton }
}

func main() {
	logrus.Info("Bot is starting...")
	_ = godotenv.Load()

	// Initialize localization
	defaultLang := i18n.PL
	langEnv := os.Getenv("DEFAULT_LANG")
	switch langEnv {
	case "pl":
		defaultLang = i18n.PL
	case "en":
		defaultLang = i18n.EN
	case "ru":
		defaultLang = i18n.RU
	case "uk":
		defaultLang = i18n.UK
	case "be":
		defaultLang = i18n.BE
	}

	if err := i18n.Init(defaultLang); err != nil {
		logrus.WithError(err).Fatal("Failed to initialize i18n")
	}

	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		logrus.Fatal("BOT_TOKEN missing")
	}
	adminChatIDStr := os.Getenv("ADMIN_CHAT_ID")
	if adminChatIDStr == "" {
		logrus.Fatal("ADMIN_CHAT_ID missing")
	}
	adminChatID, err := strconv.ParseInt(adminChatIDStr, 10, 64)
	if err != nil {
		logrus.Fatal("ADMIN_CHAT_ID invalid")
	}
	b, err := tb.NewBot(tb.Settings{Token: token, Poller: &tb.LongPoller{Timeout: 10 * time.Second}})
	if err != nil {
		logrus.WithError(err).Fatal("bot create failed")
	}
	h := NewHandler(b, adminChatID)
	h.Register()
	logrus.WithField("admin_chat_id", adminChatID).Info("Bot started")
	b.Start()
}

// NewHandler wires dependencies
func NewHandler(b *tb.Bot, adminChatID int64) *Handler {
	violations := make(map[int64]int)
	state := core.NewState()
	quiz := bot.DefaultQuiz()
	black := bot.NewBlacklist("blacklist.json")

	h := &Handler{bot: b, state: state, quiz: quiz, blacklist: black, adminChatID: adminChatID, violations: violations}

	// Buttons
	h.Btns.Student = bot.StudentButton()
	h.Btns.Guest = bot.GuestButton()
	h.Btns.Ads = bot.AdsButton()

	// Admin
	adminHandler := bot.NewAdminHandler(b, black, adminChatID, violations)
	h.adminHandler = adminHandler

	// Feature
	featureHandler := bot.NewFeatureHandler(b, state, quiz, black, adminChatID, violations, adminHandler, h.Btns)
	h.featureHandler = featureHandler
	return h
}

// Register sets handlers
func (h *Handler) Register() {
	h.bot.Handle(tb.OnUserJoined, h.featureHandler.HandleUserJoined)
	h.bot.Handle(tb.OnUserLeft, h.featureHandler.HandleUserLeft)
	h.bot.Handle(&h.Btns.Student, h.featureHandler.OnlyNewbies(h.featureHandler.HandleStudent))
	h.bot.Handle(&h.Btns.Guest, h.featureHandler.OnlyNewbies(h.featureHandler.HandleGuest))
	h.bot.Handle(&h.Btns.Ads, h.featureHandler.OnlyNewbies(h.featureHandler.HandleAds))
	h.featureHandler.RegisterQuizHandlers(h.bot)
	h.bot.Handle("/banword", h.adminHandler.HandleBan)
	h.bot.Handle("/unbanword", h.adminHandler.HandleUnban)
	h.bot.Handle("/listbanword", h.adminHandler.HandleListBan)
	h.bot.Handle("/spamban", h.adminHandler.HandleSpamBan)
	h.bot.Handle("/ping", h.featureHandler.RateLimit(h.featureHandler.HandlePing))
	h.bot.Handle("/start", h.featureHandler.HandleStart)
	h.bot.Handle(tb.OnText, h.handleTextMessage)
	h.setBotCommands()
}

// handleTextMessage handles text messages
func (h *Handler) handleTextMessage(c tb.Context) error {
	if c.Chat().Type == tb.ChatPrivate {
		if err := h.featureHandler.HandlePrivateMessage(c); err != nil {
			return err
		}
	}
	return h.featureHandler.FilterMessage(c)
}

// setBotCommands sets bot commands
func (h *Handler) setBotCommands() {
	languageMapping := map[string]i18n.Lang{
		"pl": i18n.PL,
		"en": i18n.EN,
		"ru": i18n.RU,
		"uk": i18n.UK,
		"be": i18n.BE,
		"de": i18n.EN,
	}

	// Set commands for each supported language code
	for langCode, lang := range languageMapping {
		msgs := i18n.Get().T(lang)
		commands := []tb.Command{
			{Text: "ping", Description: msgs.Commands.PingDesc},
			{Text: "banword", Description: msgs.Commands.BanwordDesc},
			{Text: "unbanword", Description: msgs.Commands.UnbanwordDesc},
			{Text: "listbanword", Description: msgs.Commands.ListbanwordDesc},
			{Text: "spamban", Description: msgs.Commands.SpambanDesc},
		}

		_ = h.bot.SetCommands(commands, langCode)
	}

	// Set default commands
	msgsPL := i18n.Get().T(i18n.PL)
	commandsDefault := []tb.Command{
		{Text: "ping", Description: msgsPL.Commands.PingDesc},
		{Text: "banword", Description: msgsPL.Commands.BanwordDesc},
		{Text: "unbanword", Description: msgsPL.Commands.UnbanwordDesc},
		{Text: "listbanword", Description: msgsPL.Commands.ListbanwordDesc},
		{Text: "spamban", Description: msgsPL.Commands.SpambanDesc},
	}
	_ = h.bot.SetCommands(commandsDefault)
}
