package bot

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	tb "gopkg.in/telebot.v4"
)

// HandlePing replies with latency (private only).
func (fh *FeatureHandler) HandlePing(c tb.Context) error {
	start := time.Now()
	if c.Message() == nil || c.Chat() == nil || c.Sender() == nil {
		return nil
	}
	if c.Chat().Type != tb.ChatPrivate {
		warnMsg, err := fh.bot.Send(c.Chat(), "ℹ️ Команда /ping доступна только в личных сообщениях с ботом.")
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{"chat_id": c.Chat().ID, "user_id": c.Sender().ID}).Error("Failed to send ping warning in group")
			return err
		}
		if fh.adminHandler != nil {
			fh.adminHandler.DeleteAfter(warnMsg, 5*time.Second)
		}
		return nil
	}
	msg, err := fh.bot.Send(c.Chat(), "🏓 Понг!")
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"chat_id": c.Chat().ID, "user_id": c.Sender().ID}).Error("Failed to send ping response")
		return err
	}
	ms := time.Since(start).Milliseconds()
	final := fmt.Sprintf("🏓 Понг! (%d мс)", ms)
	_, err = fh.bot.Edit(msg, final)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{"chat_id": c.Chat().ID, "user_id": c.Sender().ID}).Error("Failed to edit ping message")
	}
	return nil
}
