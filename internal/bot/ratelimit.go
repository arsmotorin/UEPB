package bot

import (
	"time"

	tb "gopkg.in/telebot.v4"
)

// RateLimit limits 1 command / second per user
func (fh *FeatureHandler) RateLimit(handler func(tb.Context) error) func(tb.Context) error {
	return func(c tb.Context) error {
		if c.Sender() == nil {
			return handler(c)
		}
		uid := c.Sender().ID
		fh.rlMu.Lock()
		last := fh.rateLimit[uid]
		now := time.Now()
		if !last.IsZero() && now.Sub(last) < time.Second {
			fh.rateLimit[uid] = now
			fh.rlMu.Unlock()
			if c.Chat() != nil {
				warn, _ := fh.bot.Send(c.Chat(), "⏱️ Пожалуйста, не чаще одной команды в секунду.")
				if fh.adminHandler != nil {
					fh.adminHandler.DeleteAfter(warn, 5*time.Second)
				}
			}
			return nil
		}
		fh.rateLimit[uid] = now
		fh.rlMu.Unlock()
		return handler(c)
	}
}
