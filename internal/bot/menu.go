package bot

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

const (
	BtnAddData    = "➕ Добавить данные"
	BtnProgress   = "📊 Мой прогресс"
	BtnWeeklyRecs = "📅 Рекомендации недели"
)

func MainMenuKeyboard() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(BtnAddData),
			tgbotapi.NewKeyboardButton(BtnProgress),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(BtnWeeklyRecs),
		),
	)
}

func BackToMainInlineKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("« Назад в меню", "back_main"),
		),
	)
}

func (h *Handler) sendMainMenu(chatID int64) {
	h.sendWithMainMenu(chatID, "Главное меню ЗдравоШаг.\nВыберите действие:")
}
