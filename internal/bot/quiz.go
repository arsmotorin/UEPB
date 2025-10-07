package bot

import (
	"UEPB/internal/core"
	"fmt"
	"time"

	tb "gopkg.in/telebot.v4"
)

// CreateInlineButton helper
func CreateInlineButton(unique, text string) tb.InlineButton {
	return tb.InlineButton{Unique: unique, Text: text}
}

// StudentButton returns student button
func StudentButton() tb.InlineButton {
	return CreateInlineButton("student", "👨‍🎓 Я студент, могу подтвердить")
}

// GuestButton returns guest button
func GuestButton() tb.InlineButton {
	return CreateInlineButton("guest", "🧐 У меня есть вопрос")
}

// AdsButton returns ads button
func AdsButton() tb.InlineButton {
	return CreateInlineButton("ads", "📢 Хочу разместить рекламу")
}

// HandleStudent starts quiz
func (fh *FeatureHandler) HandleStudent(c tb.Context) error {
	fh.state.InitUser(int(c.Sender().ID))
	questions := fh.quiz.GetQuestions()
	if len(questions) > 0 {
		q := questions[0]
		_ = fh.SendOrEdit(c.Chat(), c.Message(), q.GetText(), &tb.ReplyMarkup{InlineKeyboard: [][]tb.InlineButton{q.GetButtons()}})
	}
	return nil
}

// RegisterQuizHandlers registers quiz buttons
func (fh *FeatureHandler) RegisterQuizHandlers(bot *tb.Bot) {
	questions := fh.quiz.GetQuestions()
	for i, q := range questions {
		for _, btn := range q.GetButtons() {
			bot.Handle(&btn, fh.OnlyNewbies(fh.CreateQuizHandler(i, q, btn)))
		}
	}
}

// CreateQuizHandler builds handler for quiz button
func (fh *FeatureHandler) CreateQuizHandler(i int, q core.QuestionInterface, btn tb.InlineButton) func(tb.Context) error {
	return func(c tb.Context) error {
		userID := int(c.Sender().ID)
		if btn.Unique == q.GetAnswer() {
			fh.state.IncCorrect(userID)
		}
		questions := fh.quiz.GetQuestions()
		if i+1 < len(questions) {
			next := questions[i+1]
			_ = fh.SendOrEdit(c.Chat(), c.Message(), next.GetText(), &tb.ReplyMarkup{InlineKeyboard: [][]tb.InlineButton{next.GetButtons()}})
			return nil
		}
		totalCorrect := fh.state.TotalCorrect(userID)
		totalQuestions := len(questions)
		if totalCorrect >= 2 {
			fh.SetUserRestriction(c.Chat(), c.Sender(), true)
			fh.state.ClearNewbie(userID)
			msg := fh.SendOrEdit(c.Chat(), c.Message(), "✅ Верификация пройдена! Теперь можно писать в чат.", nil)
			if fh.adminHandler != nil {
				fh.adminHandler.DeleteAfter(msg, 5*time.Second)
			}
			logMsg := fmt.Sprintf("✅ Пользователь успешно прошёл верификацию.\n\nПользователь: %s\nПравильных ответов: %d/%d", fh.adminHandler.GetUserDisplayName(c.Sender()), totalCorrect, totalQuestions)
			fh.adminHandler.LogToAdmin(logMsg)
		} else {
			msg := fh.SendOrEdit(c.Chat(), c.Message(), "❌ Не удалось подтвердить статус студента.", nil)
			if fh.adminHandler != nil {
				fh.adminHandler.DeleteAfter(msg, 5*time.Second)
			}
			logMsg := fmt.Sprintf("❌ Пользователь не прошёл верификацию.\n\nПользователь: %s\nПравильных ответов: %d/%d", fh.adminHandler.GetUserDisplayName(c.Sender()), totalCorrect, totalQuestions)
			fh.adminHandler.LogToAdmin(logMsg)
		}
		fh.state.Reset(userID)
		return nil
	}
}

// Question holds quiz data
type Question struct {
	Text    string
	Buttons []tb.InlineButton
	Answer  string
}

func (q Question) GetText() string               { return q.Text }
func (q Question) GetButtons() []tb.InlineButton { return q.Buttons }
func (q Question) GetAnswer() string             { return q.Answer }

// Quiz holds questions
type Quiz struct{ Questions []Question }

func (quiz Quiz) GetQuestions() []core.QuestionInterface {
	list := make([]core.QuestionInterface, len(quiz.Questions))
	for i, qq := range quiz.Questions {
		list[i] = qq
	}
	return list
}

// DefaultQuiz returns default quiz
func DefaultQuiz() core.QuizInterface {
	return Quiz{Questions: []Question{
		{"1️⃣ Какую систему использует университет для управления обучением?", []tb.InlineButton{{Unique: "q1_usos", Text: "USOS"}, {Unique: "q1_edupl", Text: "EDUPL"}, {Unique: "q1_muci", Text: "MUCI"}}, "q1_usos"},
		{"2️⃣ Какую почту использует ВУЗ для учётных записей студентов?", []tb.InlineButton{{Unique: "q2_gmail", Text: "Gmail"}, {Unique: "q2_outlook", Text: "Outlook"}, {Unique: "q2_yahoo", Text: "Yahoo"}}, "q2_outlook"},
		{"3️⃣ На какой улице находится главный корпус университета?", []tb.InlineButton{{Unique: "q3_niepodleglosci", Text: "Ul. Niepodległości"}, {Unique: "q3_chinska", Text: "Ul. Chińska"}, {Unique: "q3_roz", Text: "Ul. Róż"}}, "q3_niepodleglosci"},
	}}
}

var _ = time.Now
