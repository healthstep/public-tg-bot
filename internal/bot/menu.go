package bot

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

const (
	BtnAddData        = "➕ Добавить данные"
	BtnProgress       = "📊 Мой прогресс"
	BtnWeeklyRecs     = "📅 Рекомендации недели"
	BtnUploadAnalyses = "📄 Загрузить анализы"
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
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(BtnUploadAnalyses),
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

func (h *Handler) handleUploadAnalyses(chatID int64) {
	var text string
	if h.siteURL != "" {
		u := h.siteURL + "/profile"
		text = "Загрузите PDF с анализами (до 5 файлов) в <b>личном кабинете</b> — раздел «Профиль».\n\n<a href=\"" + u + "\">Открыть профиль</a>"
	} else {
		text = "Загрузка анализов доступна в личном кабинете на сайте ЗдравоШаг: раздел «Профиль» — «Загрузить анализы»."
	}
	h.sendWithMainMenu(chatID, text)
}
