package main

import (
	"UEPB/config"
	"log"
	"time"

	tb "gopkg.in/telebot.v4"
)

// Start bot
func main() {
	log.Println("Bot is starting...")

	cfg := config.Load()
	bot, err := tb.NewBot(tb.Settings{Token: cfg.BotToken, Poller: &tb.LongPoller{Timeout: 10 * time.Second}})
	if err != nil {
		log.Fatal(err)
	}

	h := NewHandler(bot, NewState(), DefaultQuiz())
	h.Register()

	log.Println("Bot has started!")
	bot.Start()
}
