package bot

import (
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	tb "gopkg.in/telebot.v4"
)

// FilterMessage checks a text message against the blacklist and applies sanctions
func (fh *FeatureHandler) FilterMessage(c tb.Context) error {
	msg := c.Message()
	if msg == nil || msg.Sender == nil || c.Chat() == nil {
		return nil
	}

	// Ignore commands
	if strings.HasPrefix(msg.Text, "/") {
		return nil
	}

	// Skip admin chat
	if c.Chat().ID == fh.adminChatID {
		return nil
	}

	// Skip admins
	if fh.adminHandler != nil && fh.adminHandler.IsAdmin(c.Chat(), msg.Sender) {
		return nil
	}

	// Debug log
	logrus.WithFields(logrus.Fields{
		"chat_id": c.Chat().ID,
		"user_id": msg.Sender.ID,
		"message": msg.Text,
	}).Debug("Filtering message")

	if fh.blacklist != nil && fh.blacklist.CheckMessage(msg.Text) {
		// Record violation
		if fh.adminHandler != nil {
			fh.adminHandler.AddViolation(msg.Sender.ID)
		}
		violationCount := 0
		if fh.adminHandler != nil {
			violationCount = fh.adminHandler.GetViolations(msg.Sender.ID)
		}

		// Try to delete original
		if err := fh.bot.Delete(msg); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"message_id": msg.ID,
				"chat_id":    c.Chat().ID,
				"user_id":    msg.Sender.ID,
			}).Warn("Failed to delete blacklisted message")
		} else {
			logrus.WithFields(logrus.Fields{
				"message_id": msg.ID,
				"user_id":    msg.Sender.ID,
				"violations": violationCount,
			}).Info("Deleted blacklisted message")
		}

		if violationCount >= 2 {
			// Ban after the second violation
			if fh.adminHandler != nil {
				if err := fh.adminHandler.BanUser(c.Chat(), msg.Sender); err != nil {
					logrus.WithError(err).WithFields(logrus.Fields{
						"chat_id": c.Chat().ID,
						"user_id": msg.Sender.ID,
					}).Error("Failed to ban user for repeated violations")
				} else {
					fh.adminHandler.ClearViolations(msg.Sender.ID)
					banLog := fmt.Sprintf("🔨 Выдан бан за спам.\n\nЗабанен: %s\nНарушений: %d", fh.adminHandler.GetUserDisplayName(msg.Sender), violationCount)
					fh.adminHandler.LogToAdmin(banLog)
					logrus.WithFields(logrus.Fields{"user_id": msg.Sender.ID, "violations": violationCount}).Info("User banned after violations")
				}
			}
			return nil
		}

		// First violation -> ephemeral warning
		warningText := fmt.Sprintf("⚠️ %s, сообщение удалено. При повторном нарушении будет бан.", func() string {
			if fh.adminHandler != nil {
				return fh.adminHandler.GetUserDisplayName(msg.Sender)
			}
			return msg.Sender.Username
		}())
		warnMsg, _ := fh.bot.Send(c.Chat(), warningText)
		if fh.adminHandler != nil {
			fh.adminHandler.DeleteAfter(warnMsg, 5*time.Second)
			logMsg := fmt.Sprintf("⚠️ Обнаружено нарушение.\n\nПользователь: %s\nНарушение: #%d\nСообщение: `%s`", fh.adminHandler.GetUserDisplayName(msg.Sender), violationCount, msg.Text)
			fh.adminHandler.LogToAdmin(logMsg)
		}
	}
	return nil
}
