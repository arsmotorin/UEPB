package bot

import (
	"UEPB/internal/core"
	"time"

	tb "gopkg.in/telebot.v4"
)

// QuestionInterface lists question methods.
type QuestionInterface = core.QuestionInterface

type QuizInterface = core.QuizInterface

type BlacklistInterface = core.BlacklistInterface

type AdminHandlerInterface = core.AdminHandlerInterface

// FeatureHandlerInterface lists feature methods.
type FeatureHandlerInterface interface {
	OnlyNewbies(handler func(tb.Context) error) func(tb.Context) error
	SendOrEdit(chat *tb.Chat, msg *tb.Message, text string, rm *tb.ReplyMarkup) *tb.Message
	SetUserRestriction(chat *tb.Chat, user *tb.User, allowAll bool)
	HandleUserJoined(c tb.Context) error
	HandleUserLeft(c tb.Context) error
	HandleStudent(c tb.Context) error
	HandleGuest(c tb.Context) error
	HandleAds(c tb.Context) error
	HandlePing(c tb.Context) error
	HandleEvent(c tb.Context) error
	HandlePrevEvent(c tb.Context) error
	HandleNextEvent(c tb.Context) error
	HandleEventInterested(c tb.Context) error
	HandleEventUnsubscribe(c tb.Context) error
	HandleBroadcastInterested(c tb.Context) error
	HandleStart(c tb.Context) error
	HandlePrivateMessage(c tb.Context) error
	RateLimit(handler func(tb.Context) error) func(tb.Context) error
	RegisterQuizHandlers(bot *tb.Bot)
	CreateQuizHandler(i int, q QuestionInterface, btn tb.InlineButton) func(tb.Context) error
	FilterMessage(c tb.Context) error
	RegisterGroup(chatID int64)
	StartEventBroadcaster()
}

var _ = time.Now
