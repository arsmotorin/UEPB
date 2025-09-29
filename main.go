package main

import (
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	tb "gopkg.in/telebot.v4"
)

// Start bot
func main() {
	log.Println("Bot is starting...")

	// Load .env file
	godotenv.Load()
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("BOT_TOKEN is required in .env file")
	}

	bot, err := tb.NewBot(tb.Settings{Token: token, Poller: &tb.LongPoller{Timeout: 10 * time.Second}})
	if err != nil {
		log.Fatal(err)
	}

	h := NewHandler(bot, NewState(), DefaultQuiz())
	h.Register()

	log.Println("Bot has started!")
	bot.Start()
}
