package interfaces

import (
	"time"

	tb "gopkg.in/telebot.v4"
)

// UserState interface for state management
type UserState interface {
	InitUser(id int)
	IncCorrect(id int)
	TotalCorrect(id int) int
	Reset(id int)
	SetNewbie(id int)
	ClearNewbie(id int)
	IsNewbie(id int) bool
}

// QuestionInterface interface for quiz questions
type QuestionInterface interface {
	GetText() string
	GetButtons() []tb.InlineButton
	GetAnswer() string
}

// QuizInterface interface for quiz
type QuizInterface interface {
	GetQuestions() []QuestionInterface
}

// BlacklistInterface interface for blocklist functionality
type BlacklistInterface interface {
	AddPhrase(words []string)
	RemovePhrase(words []string) bool
	List() [][]string
	CheckMessage(msg string) bool
}

// AdminHandlerInterface interface for admin functionality
type AdminHandlerInterface interface {
	LogToAdmin(message string)
	IsAdmin(chat *tb.Chat, user *tb.User) bool
	GetUserDisplayName(user *tb.User) string
	DeleteAfter(m *tb.Message, d time.Duration)
	BanUser(chat *tb.Chat, user *tb.User) error
	HandleBan(c tb.Context) error
	HandleUnban(c tb.Context) error
	HandleListBan(c tb.Context) error
	HandleSpamBan(c tb.Context) error
	AddViolation(userID int64)
	GetViolations(userID int64) int
	ClearViolations(userID int64)
	Bot() *tb.Bot
}

// FeatureHandlerInterface interface for feature functionality
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
