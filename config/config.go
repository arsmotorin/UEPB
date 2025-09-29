package config

type Config struct {
	BotToken string
}

func Load() Config {
	return Config{
		BotToken: "token",
	}
}
