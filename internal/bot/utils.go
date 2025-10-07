package bot

import (
	"UEPB/internal/core"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	tb "gopkg.in/telebot.v4"
)

// FeatureHandler aggregates bot feature state and logic
type FeatureHandler struct {
	bot                  *tb.Bot
	state                core.UserState
	quiz                 core.QuizInterface
	blacklist            core.BlacklistInterface
	adminChatID          int64
	violations           map[int64]int
	rlMu                 sync.Mutex
	rateLimit            map[int64]time.Time
	Btns                 struct{ Student, Guest, Ads tb.InlineButton }
	adminHandler         core.AdminHandlerInterface
	eventsCache          []EventData
	eventsCacheMu        sync.RWMutex
	cacheTime            time.Time
	eventRateLimit       map[int64]time.Time
	eventRateLimitMu     sync.Mutex
	eventInterests       map[string][]int64
	eventInterestsMu     sync.RWMutex
	userEventInterests   map[int64]map[string]bool
	userEventInterestsMu sync.RWMutex
	pendingActivations   map[int64]string
	pendingActivationsMu sync.Mutex
	activatedUsers       map[int64]bool
	activatedUsersMu     sync.RWMutex
	eventMessageOwners   map[string]int64
	eventMessageOwnersMu sync.RWMutex
	registeredGroups     map[int64]bool
	registeredGroupsMu   sync.RWMutex
	broadcastedEvents    map[string]bool
	broadcastedEventsMu  sync.Mutex
}

// NewFeatureHandler constructs feature handler
func NewFeatureHandler(bot *tb.Bot, state core.UserState, quiz core.QuizInterface, blacklist core.BlacklistInterface, adminChatID int64, violations map[int64]int, adminHandler core.AdminHandlerInterface, btns struct{ Student, Guest, Ads tb.InlineButton }) *FeatureHandler {
	return &FeatureHandler{bot: bot, state: state, quiz: quiz, blacklist: blacklist, adminChatID: adminChatID, violations: violations, rateLimit: make(map[int64]time.Time), Btns: btns, adminHandler: adminHandler, eventRateLimit: make(map[int64]time.Time), eventInterests: make(map[string][]int64), userEventInterests: make(map[int64]map[string]bool), pendingActivations: make(map[int64]string), activatedUsers: make(map[int64]bool), eventMessageOwners: make(map[string]int64), registeredGroups: make(map[int64]bool), broadcastedEvents: make(map[string]bool)}
}

// OnlyNewbies restricts handler to newbies
func (fh *FeatureHandler) OnlyNewbies(handler func(tb.Context) error) func(tb.Context) error {
	return func(c tb.Context) error {
		if c.Sender() == nil || !fh.state.IsNewbie(int(c.Sender().ID)) {
			if cb := c.Callback(); cb != nil {
				_ = fh.bot.Respond(cb, &tb.CallbackResponse{Text: "Это не твоя кнопка"})
			}
			return nil
		}
		return handler(c)
	}
}

// SendOrEdit sends or edits a message
func (fh *FeatureHandler) SendOrEdit(chat *tb.Chat, msg *tb.Message, text string, rm *tb.ReplyMarkup) *tb.Message {
	var err error
	if msg == nil {
		msg, err = fh.bot.Send(chat, text, rm)
	} else {
		msg, err = fh.bot.Edit(msg, text, rm)
	}
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"chat_id": chat.ID, "action": "send_or_edit"}).Error("Message error")
		return nil
	}
	return msg
}

// SetUserRestriction applies chat permissions
func (fh *FeatureHandler) SetUserRestriction(chat *tb.Chat, user *tb.User, allowAll bool) {
	if allowAll {
		rights := tb.Rights{CanSendMessages: true, CanSendPhotos: true, CanSendVideos: true, CanSendVideoNotes: true, CanSendVoiceNotes: true, CanSendPolls: true, CanSendOther: true, CanAddPreviews: true, CanInviteUsers: true}
		if err := fh.bot.Restrict(chat, &tb.ChatMember{User: user, Rights: rights, RestrictedUntil: tb.Forever()}); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"chat_id": chat.ID, "user_id": user.ID, "action": "unrestrict"}).Error("Failed to unrestrict")
		}
	} else {
		if err := fh.bot.Restrict(chat, &tb.ChatMember{User: user, Rights: tb.Rights{CanSendMessages: false}}); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"chat_id": chat.ID, "user_id": user.ID, "action": "restrict"}).Error("Failed to restrict")
		}
	}
}

// GetNewUsers extracts users from join
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

// HandleUserJoined processes join
func (fh *FeatureHandler) HandleUserJoined(c tb.Context) error {
	if c.Message() == nil || c.Chat() == nil {
		return nil
	}
	if c.Chat().Type == tb.ChatGroup || c.Chat().Type == tb.ChatSuperGroup {
		fh.RegisterGroup(c.Chat().ID)
	}
	if reg, ok := fh.adminHandler.(interface{ RegisterGroup(*tb.Chat) }); ok {
		reg.RegisterGroup(c.Chat())
	}
	users := GetNewUsers(c.Message())
	kb := &tb.ReplyMarkup{InlineKeyboard: [][]tb.InlineButton{{fh.Btns.Student}, {fh.Btns.Guest}, {fh.Btns.Ads}}}
	for _, u := range users {
		fh.state.SetNewbie(int(u.ID))
		fh.SetUserRestriction(c.Chat(), u, false)
		txt := "👋 Привет!\n\nВыбери, что тебя интересует, используя кнопки ниже."
		if u.Username != "" {
			txt = fmt.Sprintf("👋 Привет, @%s!\n\nВыбери, что тебя интересует, используя кнопки ниже.", u.Username)
		}
		msg := fh.SendOrEdit(c.Chat(), nil, txt, kb)
		fh.adminHandler.DeleteAfter(msg, 5*time.Minute)
		fh.state.InitUser(int(u.ID))
		logMsg := fmt.Sprintf("👤 Новый участник вошёл в чат.\n\nПользователь: %s", fh.adminHandler.GetUserDisplayName(u))
		fh.adminHandler.LogToAdmin(logMsg)
	}
	return nil
}

// HandleUserLeft clears the state on leave
func (fh *FeatureHandler) HandleUserLeft(c tb.Context) error {
	if c.Message() == nil || c.Chat() == nil || c.Message().UserLeft == nil {
		return nil
	}
	user := c.Message().UserLeft
	fh.state.ClearNewbie(int(user.ID))
	fh.adminHandler.ClearViolations(user.ID)
	logMsg := fmt.Sprintf("👋 Участник покинул чат.\n\nПользователь: %s", fh.adminHandler.GetUserDisplayName(user))
	fh.adminHandler.LogToAdmin(logMsg)
	return nil
}

// HandleGuest lifts restriction for guest.
func (fh *FeatureHandler) HandleGuest(c tb.Context) error {
	fh.SetUserRestriction(c.Chat(), c.Sender(), true)
	fh.state.ClearNewbie(int(c.Sender().ID))
	msg := fh.SendOrEdit(c.Chat(), c.Message(), "✅ Теперь можно писать в чат. Задай свой вопрос.", nil)
	fh.adminHandler.DeleteAfter(msg, 5*time.Second)
	logMsg := fmt.Sprintf("🧐 Пользователь выбрал, что у него есть вопрос.\n\nПользователь: %s", fh.adminHandler.GetUserDisplayName(c.Sender()))
	fh.adminHandler.LogToAdmin(logMsg)
	return nil
}

// HandleAds informs about ads
func (fh *FeatureHandler) HandleAds(c tb.Context) error {
	msg := fh.SendOrEdit(c.Chat(), c.Message(), "📢 Мы открыты к рекламе.\n\nНапиши @chathlp и опиши, что хочешь предложить.", nil)
	fh.adminHandler.DeleteAfter(msg, 10*time.Second)
	logMsg := fmt.Sprintf("📢 Пользователь выбрал рекламу.\n\nПользователь: %s", fh.adminHandler.GetUserDisplayName(c.Sender()))
	fh.adminHandler.LogToAdmin(logMsg)
	return nil
}

// HandleStart handles /start in private
func (fh *FeatureHandler) HandleStart(c tb.Context) error {
	if c.Chat().Type != tb.ChatPrivate || c.Sender() == nil {
		return nil
	}
	uid := c.Sender().ID
	fh.activatedUsersMu.Lock()
	fh.activatedUsers[uid] = true
	fh.activatedUsersMu.Unlock()
	fh.pendingActivationsMu.Lock()
	eventID, pending := fh.pendingActivations[uid]
	if pending {
		delete(fh.pendingActivations, uid)
	}
	fh.pendingActivationsMu.Unlock()
	if pending {
		fh.eventInterestsMu.Lock()
		if fh.eventInterests[eventID] == nil {
			fh.eventInterests[eventID] = []int64{}
		}
		fh.eventInterests[eventID] = append(fh.eventInterests[eventID], uid)
		fh.eventInterestsMu.Unlock()
		fh.userEventInterestsMu.Lock()
		if fh.userEventInterests[uid] == nil {
			fh.userEventInterests[uid] = make(map[string]bool)
		}
		fh.userEventInterests[uid][eventID] = true
		fh.userEventInterestsMu.Unlock()
		err := fh.sendEventSubscriptionConfirmation(c.Chat(), eventID)
		logrus.WithFields(logrus.Fields{"user_id": uid, "event_id": eventID}).Info("User subscribed via start")
		return err
	}
	_, err := fh.bot.Send(c.Chat(), "👋 Привет! Я – бот студенческой группы UEP.\n\nНачни вводить команды с / и я тебе покажу, что могу делать")
	logrus.WithField("user_id", uid).Info("User started bot")
	return err
}

// HandlePrivateMessage handles any non-command private message
func (fh *FeatureHandler) HandlePrivateMessage(c tb.Context) error {
	if c.Chat().Type != tb.ChatPrivate || c.Sender() == nil || c.Message() == nil {
		return nil
	}
	if strings.HasPrefix(c.Message().Text, "/") {
		return nil
	}
	uid := c.Sender().ID
	fh.activatedUsersMu.Lock()
	fh.activatedUsers[uid] = true
	fh.activatedUsersMu.Unlock()
	fh.pendingActivationsMu.Lock()
	eventID, pending := fh.pendingActivations[uid]
	if pending {
		delete(fh.pendingActivations, uid)
	}
	fh.pendingActivationsMu.Unlock()
	if pending {
		fh.eventInterestsMu.Lock()
		if fh.eventInterests[eventID] == nil {
			fh.eventInterests[eventID] = []int64{}
		}
		fh.eventInterests[eventID] = append(fh.eventInterests[eventID], uid)
		fh.eventInterestsMu.Unlock()
		fh.userEventInterestsMu.Lock()
		if fh.userEventInterests[uid] == nil {
			fh.userEventInterests[uid] = make(map[string]bool)
		}
		fh.userEventInterests[uid][eventID] = true
		fh.userEventInterestsMu.Unlock()
		err := fh.sendEventSubscriptionConfirmation(c.Chat(), eventID)
		logrus.WithFields(logrus.Fields{"user_id": uid, "event_id": eventID}).Info("User subscribed via message")
		return err
	}
	return nil
}

// RegisterGroup registers chat for broadcasts
func (fh *FeatureHandler) RegisterGroup(chatID int64) {
	fh.registeredGroupsMu.Lock()
	fh.registeredGroups[chatID] = true
	fh.registeredGroupsMu.Unlock()
	logrus.WithField("chat_id", chatID).Info("Group registered for broadcasts")
}

// sendEventSubscriptionConfirmation sends confirmation message
func (fh *FeatureHandler) sendEventSubscriptionConfirmation(chat *tb.Chat, eventID string) error {
	fh.eventsCacheMu.RLock()
	var current *EventData
	for i := range fh.eventsCache {
		if fh.eventsCache[i].GetEventID() == eventID {
			current = &fh.eventsCache[i]
			break
		}
	}
	fh.eventsCacheMu.RUnlock()
	if current == nil {
		_, err := fh.bot.Send(chat, "✅ Ты успешно подписался на событие.")
		return err
	}
	timeInfo := ""
	if current.Day != "" && current.Month != "" {
		monthName := current.Month
		if strings.Contains(monthName, " ") {
			monthName = strings.ToLower(strings.Split(monthName, " ")[0])
		}
		if current.Time != "" {
			ts := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(current.Time), "-"))
			timeInfo = fmt.Sprintf("%s %s в %s", current.Day, monthName, ts)
		} else {
			timeInfo = fmt.Sprintf("%s %s", current.Day, monthName)
		}
	}
	unsub := tb.InlineButton{Unique: "event_unsubscribe", Text: "Больше не интересно ❌", Data: fmt.Sprintf("unsub_%s", eventID)}
	markup := &tb.ReplyMarkup{InlineKeyboard: [][]tb.InlineButton{{unsub}}}
	confirm := fmt.Sprintf("✅ Ты успешно подписался на событие.\n\nСобытие: %s\nВремя: %s\n\nЯ пришлю тебе напоминания за сутки и за 2 часа до начала.", current.Title, timeInfo)
	_, err := fh.bot.Send(chat, confirm, markup)
	return err
}
