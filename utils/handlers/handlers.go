package handlers

import (
	"UEPB/features"
	"UEPB/utils/blacklist"
	"UEPB/utils/interfaces"
	"UEPB/utils/logger"
	"UEPB/utils/quiz"
	"UEPB/utils/state"

	"github.com/sirupsen/logrus"
	tb "gopkg.in/telebot.v4"
)

type Handler struct {
	bot            *tb.Bot
	state          interfaces.UserState
	quiz           interfaces.QuizInterface
	blacklist      interfaces.BlacklistInterface
	adminChatID    int64
	violations     map[int64]int
	adminHandler   interfaces.AdminHandlerInterface
	featureHandler interfaces.FeatureHandlerInterface
	Btns           struct {
		Student, Guest, Ads tb.InlineButton
	}
}

// NewHandler creates a new Handler
func NewHandler(bot *tb.Bot, adminChatID int64) *Handler {
	violations := make(map[int64]int)

	// Initialize dependencies
	userState := state.NewState()
	quizInterface := quiz.DefaultQuiz()
	blacklistInterface := blacklist.NewBlacklist("blacklist.json")

	h := &Handler{
		bot:         bot,
		state:       userState,
		quiz:        quizInterface,
		blacklist:   blacklistInterface,
		adminChatID: adminChatID,
		violations:  violations,
	}

	// Initialize buttons
	h.Btns.Student = quiz.StudentButton()
	h.Btns.Guest = quiz.GuestButton()
	h.Btns.Ads = quiz.AdsButton()

	// Initialize admin handler
	adminHandler := features.NewAdminHandler(bot, blacklistInterface, adminChatID, violations)
	h.adminHandler = adminHandler

	// Initialize feature handler
	featureHandler := features.NewFeatureHandler(bot, userState, quizInterface, blacklistInterface, adminChatID, violations, adminHandler, h.Btns)
	h.featureHandler = featureHandler

	return h
}

// Register registers all handlers
func (h *Handler) Register() {
	// User events
	h.bot.Handle(tb.OnUserJoined, h.featureHandler.HandleUserJoined)
	h.bot.Handle(tb.OnUserLeft, h.featureHandler.HandleUserLeft)

	// Feature buttons
	h.bot.Handle(&h.Btns.Student, h.featureHandler.OnlyNewbies(h.featureHandler.HandleStudent))
	h.bot.Handle(&h.Btns.Guest, h.featureHandler.OnlyNewbies(h.featureHandler.HandleGuest))
	h.bot.Handle(&h.Btns.Ads, h.featureHandler.OnlyNewbies(h.featureHandler.HandleAds))

	// Quiz handlers
	h.featureHandler.RegisterQuizHandlers(h.bot)

	// Admin commands
	h.bot.Handle("/banword", h.adminHandler.HandleBan)
	h.bot.Handle("/unbanword", h.adminHandler.HandleUnban)
	h.bot.Handle("/listbanword", h.adminHandler.HandleListBan)
	h.bot.Handle("/spamban", h.adminHandler.HandleSpamBan)

	// Feature commands
	// h.bot.Handle("/ping", h.featureHandler.HandlePing)

	// Message filter
	h.bot.Handle(tb.OnText, h.featureHandler.FilterMessage)

	// Set bot commands
	h.setBotCommands()
}

// setBotCommands sets bot commands
func (h *Handler) setBotCommands() {
	commands := []tb.Command{
		// {Text: "ping", Description: "Проверить отклик бота"},
		{Text: "banword", Description: "Добавить запрещённое слово"},
		{Text: "unbanword", Description: "Удалить запрещённое слово"},
		{Text: "listbanword", Description: "Показать список запрещённых слов"},
		{Text: "spamban", Description: "Забанить пользователя за спам"},
	}

	if err := h.bot.SetCommands(commands); err != nil {
		logger.Error("Failed to set bot commands", err, logrus.Fields{
			"commands_count": len(commands),
		})
	}
}
