package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	userspb "github.com/helthtech/core-users/pkg/proto/users"
)

// getUserSex resolves the user's sex from core-users for the given Telegram user.
func (h *Handler) getUserSex(ctx context.Context, telegramUserID string) string {
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		return ""
	}
	resp, err := h.usersClient.GetUser(ctx, &userspb.GetUserRequest{UserId: chat.UserID.String()})
	if err != nil {
		return ""
	}
	return resp.GetSex()
}

// handleAddData is Step 1: show the list of analyses to select from.
func (h *Handler) handleAddData(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := fmt.Sprintf("%d", msg.From.ID)
	userSex := h.getUserSex(ctx, telegramUserID)

	chat, _ := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	userID := ""
	if chat != nil && chat.UserID != nil {
		userID = chat.UserID.String()
	}

	resp, err := h.healthClient.ListAnalysis(ctx, &healthpb.ListAnalysisRequest{
		UserId:  userID,
		UserSex: userSex,
	})
	if err != nil {
		log.Printf("list analysis: %v", err)
		h.sendText(msg.Chat.ID, "Не удалось загрузить список анализов. Попробуйте позже.")
		return
	}

	if len(resp.GetAnalyses()) == 0 {
		h.sendText(msg.Chat.ID, "Нет доступных анализов.")
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, a := range resp.GetAnalyses() {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔬 "+a.GetName(), "analysis_"+a.GetId()),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Назад", "back_main"),
	))

	m := tgbotapi.NewMessage(msg.Chat.ID, "➕ <b>Добавить данные</b>\n\nВыберите анализ:")
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send analysis list: %v", err)
	}
}

// showCriteriaForAnalysis is Step 2: show criteria for the selected analysis.
func (h *Handler) showCriteriaForAnalysis(ctx context.Context, chatID int64, analysisID string) {
	resp, err := h.healthClient.ListCriteria(ctx, &healthpb.ListCriteriaRequest{
		AnalysisId: analysisID,
	})
	if err != nil {
		log.Printf("list criteria for analysis %s: %v", analysisID, err)
		h.sendText(chatID, "Не удалось загрузить показатели. Попробуйте позже.")
		return
	}

	if len(resp.GetCriteria()) == 0 {
		h.sendText(chatID, "В этом анализе нет показателей.")
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range resp.GetCriteria() {
		levelIcon := criterionLevelIcon(int(c.GetLevel()))
		label := levelIcon + " " + c.GetName()

		// Embed analysisID in the callback so cancel works even from within criterion entry.
		rows = append(rows,
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label+" — ввести значение", "criterion_manual_"+c.GetId()+":"+c.GetName()+":"+analysisID),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(label+" — отметить выполнение", "criterion_done_"+c.GetId()+":"+analysisID),
				tgbotapi.NewInlineKeyboardButtonData("📎 загрузить файл", "criterion_upload_"+c.GetId()+":"+analysisID),
			),
		)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Назад к анализам", "back_analysis"),
	))

	prompt := "Выберите показатель и способ ввода:\n\n<i>Введите «отмена» в любой момент, чтобы сбросить все данные этого анализа.</i>"
	m := tgbotapi.NewMessage(chatID, prompt)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send criteria list: %v", err)
	}
}

// handleCancelAnalysis resets all user criteria for an analysis.
func (h *Handler) handleCancelAnalysis(ctx context.Context, msg *tgbotapi.Message, analysisID string) {
	telegramUserID := fmt.Sprintf("%d", msg.From.ID)
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(msg.Chat.ID, "Вы не авторизованы.")
		return
	}
	_, err = h.healthClient.ResetAnalysisCriteria(ctx, &healthpb.ResetAnalysisCriteriaRequest{
		UserId:     chat.UserID.String(),
		AnalysisId: analysisID,
	})
	if err != nil {
		log.Printf("reset analysis criteria: %v", err)
		h.sendText(msg.Chat.ID, "Не удалось сбросить данные. Попробуйте позже.")
		return
	}
	h.sendWithMainMenu(msg.Chat.ID, "✅ Все данные этого анализа сброшены.")
}

// handleNumericInput processes the numeric value typed by the user.
func (h *Handler) handleNumericInput(ctx context.Context, msg *tgbotapi.Message, pending PendingInput) {
	chatID := msg.Chat.ID
	telegramUserID := strconv.FormatInt(msg.From.ID, 10)

	numVal, err := strconv.ParseFloat(strings.TrimSpace(msg.Text), 64)
	if err != nil {
		h.sendText(chatID, "Пожалуйста, введите корректное число.")
		pendingNumericInput.Store(telegramUserID, pending)
		return
	}

	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(chatID, "Вы не авторизованы. Перейдите по ссылке из приложения ЗдравоШаг.")
		return
	}

	_, err = h.healthClient.SetUserCriterion(ctx, &healthpb.SetUserCriterionRequest{
		UserId:      chat.UserID.String(),
		CriterionId: pending.CriterionID,
		Value:       fmt.Sprintf("%.2f", numVal),
		Source:      "telegram",
	})
	if err != nil {
		log.Printf("set user criterion: %v", err)
		h.sendText(chatID, "Не удалось сохранить значение. Попробуйте позже.")
		return
	}

	h.sendWithMainMenu(chatID, fmt.Sprintf("✅ <b>%s</b>: %.2f — сохранено!", pending.CriterionName, numVal))
}

// handleMarkDone marks a criterion as done (value = "1").
func (h *Handler) handleMarkDone(ctx context.Context, chatID int64, telegramUserID int64, criterionID string) {
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, strconv.FormatInt(telegramUserID, 10))
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(chatID, "Вы не авторизованы. Перейдите по ссылке из приложения ЗдравоШаг.")
		return
	}

	_, err = h.healthClient.SetUserCriterion(ctx, &healthpb.SetUserCriterionRequest{
		UserId:      chat.UserID.String(),
		CriterionId: criterionID,
		Value:       "1",
		Source:      "telegram",
	})
	if err != nil {
		log.Printf("set user criterion (mark done): %v", err)
		h.sendText(chatID, "Ошибка при сохранении. Попробуйте позже.")
		return
	}

	h.sendWithMainMenu(chatID, "✅ Отмечено как выполнено!")
}

func criterionLevelIcon(level int) string {
	switch level {
	case 1:
		return "⭐"
	case 2:
		return "⭐⭐"
	case 3:
		return "⭐⭐⭐"
	default:
		return "•"
	}
}
