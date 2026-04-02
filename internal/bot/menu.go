package bot

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

const (
	BtnAddData       = "➕ Добавить данные"
	BtnProgress      = "📊 Мой прогресс"
	BtnChecklist     = "✅ Чеклист здоровья"
	BtnNotifications = "🔔 Уведомления"
)

func MainMenuKeyboard() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(BtnAddData),
			tgbotapi.NewKeyboardButton(BtnProgress),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(BtnChecklist),
			tgbotapi.NewKeyboardButton(BtnNotifications),
		),
	)
}

func AddDataInlineKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📄 Загрузить файл", "add_upload"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Ввести вручную", "add_manual"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Отметить визит", "add_visit"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("« Назад", "back_main"),
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
