package main

import (
	"os"
	"strconv"
	"time"

	"UEPB/utils/handlers"
	"UEPB/utils/logger"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	tb "gopkg.in/telebot.v4"
)

// Start bot
func main() {
	logger.Info("Bot is starting...")

	// Load .env file
	godotenv.Load()
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		logger.Logger.Fatal("BOT_TOKEN is required in .env file")
	}

	adminChatIDStr := os.Getenv("ADMIN_CHAT_ID")
	if adminChatIDStr == "" {
		logger.Logger.Fatal("ADMIN_CHAT_ID is required in .env file")
	}

	adminChatID, err := strconv.ParseInt(adminChatIDStr, 10, 64)
	if err != nil {
		logger.Logger.WithField("admin_chat_id", adminChatIDStr).Fatal("ADMIN_CHAT_ID must be a valid integer")
	}

	bot, err := tb.NewBot(tb.Settings{Token: token, Poller: &tb.LongPoller{Timeout: 10 * time.Second}})
	if err != nil {
		logger.Error("Failed to create bot", err, logrus.Fields{
			"token_length": len(token),
		})
		logger.Logger.Fatal(err)
	}

	h := handlers.NewHandler(bot, adminChatID)
	h.Register()

	logger.Info("Bot has started successfully!", logrus.Fields{
		"admin_chat_id": adminChatID,
	})
	bot.Start()
}
