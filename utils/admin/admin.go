package admin

import (
	"strconv"
	"strings"
	"time"

	"UEPB/utils/interfaces"

	tb "gopkg.in/telebot.v4"
)

// IsAdminOrWarn checks if the user is admin or warns if not
func IsAdminOrWarn(ah interfaces.AdminHandlerInterface, c tb.Context, warn string) bool {
	if c.Message() == nil || c.Sender() == nil || !ah.IsAdmin(c.Chat(), c.Sender()) {
		ReplyAndDelete(ah, c, warn, 10*time.Second)
		return false
	}
	return true
}

// ReplyAndDelete sends a reply and deletes it after a duration
func ReplyAndDelete(ah interfaces.AdminHandlerInterface, c tb.Context, text string, d time.Duration) {
	msg, _ := ah.Bot().Send(c.Chat(), text)
	ah.DeleteAfter(msg, d)
}

// Reply sends a reply
func Reply(ah interfaces.AdminHandlerInterface, c tb.Context, text string) {
	_, _ = ah.Bot().Send(c.Chat(), text)
}

// ResolveTargetUser resolves a user from a reply or command argument
func ResolveTargetUser(ah interfaces.AdminHandlerInterface, c tb.Context) *tb.User {
	if c.Message().ReplyTo != nil && c.Message().ReplyTo.Sender != nil {
		return c.Message().ReplyTo.Sender
	}
	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		return nil
	}
	identifier := args[1]
	if strings.HasPrefix(identifier, "@") {
		user, err := ah.Bot().ChatMemberOf(c.Chat(), &tb.User{Username: identifier[1:]})
		if err == nil && user.User != nil {
			return user.User
		}
	} else {
		id, err := strconv.ParseInt(identifier, 10, 64)
		if err == nil {
			user, err := ah.Bot().ChatMemberOf(c.Chat(), &tb.User{ID: id})
			if err == nil && user.User != nil {
				return user.User
			}
		}
	}
	return nil
}
