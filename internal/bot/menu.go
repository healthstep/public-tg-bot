package bot

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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

func (h *Handler) handleUploadAnalyses(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := fmt.Sprintf("%d", msg.From.ID)
	h.startLabUploadFromMenu(ctx, msg.Chat.ID, telegramUserID)
}
