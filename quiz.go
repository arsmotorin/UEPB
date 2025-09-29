package main

import tb "gopkg.in/telebot.v4"

type Question struct {
	Text    string
	Buttons []tb.InlineButton
	Answer  string
}

type Quiz struct {
	Questions []Question
}

func ib(unique, text string) tb.InlineButton {
	return tb.InlineButton{Unique: unique, Text: text}
}

func DefaultQuiz() Quiz {
	return Quiz{
		Questions: []Question{
			{"1️⃣ Какую систему использует университет для управления обучением?",
				[]tb.InlineButton{ib("q1_usos", "USOS"), ib("q1_uepp", "UEPP"), ib("q1_muci", "MUCI")},
				"q1_usos"},
			{"2️⃣ Какую почту использует ВУЗ для учётных записей студентов?",
				[]tb.InlineButton{ib("q2_gmail", "Gmail"), ib("q2_outlook", "Outlook"), ib("q2_yahoo", "Yahoo")},
				"q2_outlook"},
			{"3️⃣ На какой улице находится главный корпус университета?",
				[]tb.InlineButton{ib("q3_niepodleglosci", "Ul. Niepodległości"), ib("q3_chinska", "Ul. Chińska"), ib("q3_roz", "Ul. Róż")},
				"q3_niepodleglosci"},
		},
	}
}

func StudentButton() tb.InlineButton {
	return ib("student", "👨‍🎓 Я студент, могу подтвердить")
}
func GuestButton() tb.InlineButton { return ib("guest", "🧐 У меня есть вопрос") }
func AdsButton() tb.InlineButton {
	return ib("ads", "📢 Хочу разместить рекламу")
}
