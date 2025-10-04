package quiz

import (
	"UEPB/utils/interfaces"

	tb "gopkg.in/telebot.v4"
)

type Question struct {
	Text    string
	Buttons []tb.InlineButton
	Answer  string
}

func (q Question) GetText() string {
	return q.Text
}

func (q Question) GetButtons() []tb.InlineButton {
	return q.Buttons
}

func (q Question) GetAnswer() string {
	return q.Answer
}

type Quiz struct {
	Questions []Question
}

func (quiz Quiz) GetQuestions() []interfaces.QuestionInterface {
	questions := make([]interfaces.QuestionInterface, len(quiz.Questions))
	for i, q := range quiz.Questions {
		questions[i] = q
	}
	return questions
}

// CreateInlineButton creates a new inline button
func CreateInlineButton(unique, text string) tb.InlineButton {
	return tb.InlineButton{Unique: unique, Text: text}
}

// DefaultQuiz returns the default quiz
func DefaultQuiz() interfaces.QuizInterface {
	return Quiz{
		Questions: []Question{
			{"1️⃣ Какую систему использует университет для управления обучением?",
				[]tb.InlineButton{
					CreateInlineButton("q1_usos", "USOS"),
					CreateInlineButton("q1_edupl", "EDUPL"),
					CreateInlineButton("q1_muci", "MUCI"),
				},
				"q1_usos"},
			{"2️⃣ Какую почту использует ВУЗ для учётных записей студентов?",
				[]tb.InlineButton{
					CreateInlineButton("q2_gmail", "Gmail"),
					CreateInlineButton("q2_outlook", "Outlook"),
					CreateInlineButton("q2_yahoo", "Yahoo"),
				},
				"q2_outlook"},
			{"3️⃣ На какой улице находится главный корпус университета?",
				[]tb.InlineButton{
					CreateInlineButton("q3_niepodleglosci", "Ul. Niepodległości"),
					CreateInlineButton("q3_chinska", "Ul. Chińska"),
					CreateInlineButton("q3_roz", "Ul. Róż"),
				},
				"q3_niepodleglosci"},
		},
	}
}

// StudentButton returns a button for students
func StudentButton() tb.InlineButton {
	return CreateInlineButton("student", "👨‍🎓 Я студент, могу подтвердить")
}

// GuestButton returns a button for guests
func GuestButton() tb.InlineButton {
	return CreateInlineButton("guest", "🧐 У меня есть вопрос")
}

// AdsButton returns a button for ads
func AdsButton() tb.InlineButton {
	return CreateInlineButton("ads", "📢 Хочу разместить рекламу")
}
