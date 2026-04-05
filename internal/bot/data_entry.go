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

// handleAddData shows the flat list of available criteria.
func (h *Handler) handleAddData(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := fmt.Sprintf("%d", msg.From.ID)
	userSex := h.getUserSex(ctx, telegramUserID)

	chat, _ := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	userID := ""
	if chat != nil && chat.UserID != nil {
		userID = chat.UserID.String()
	}

	resp, err := h.healthClient.ListCriteria(ctx, &healthpb.ListCriteriaRequest{
		UserId:  userID,
		UserSex: userSex,
	})
	if err != nil {
		log.Printf("list criteria: %v", err)
		h.sendText(msg.Chat.ID, "Не удалось загрузить список показателей. Попробуйте позже.")
		return
	}

	if len(resp.GetCriteria()) == 0 {
		h.sendText(msg.Chat.ID, "Нет доступных показателей.")
		return
	}

	// Cache criterion names and input types for callback handling.
	for _, c := range resp.GetCriteria() {
		criterionNames.Store(c.GetId(), c.GetName())
		criterionInputTypes.Store(c.GetId(), c.GetInputType())
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range resp.GetCriteria() {
		icon := criterionLevelIcon(int(c.GetLevel()))
		label := icon + " " + c.GetName()
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, "criterion_select_"+c.GetId()),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Назад", "back_main"),
	))

	m := tgbotapi.NewMessage(msg.Chat.ID,
		"➕ <b>Добавить данные</b>\n\nВыберите показатель:\n\n<i>Введите «отмена» в любой момент, чтобы сбросить все ваши данные.</i>")
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send criteria list: %v", err)
	}
}

// handleCriterionSelect asks the user for input based on the criterion's input_type.
func (h *Handler) handleCriterionSelect(ctx context.Context, chatID int64, telegramUserID string, criterionID string) {
	name := ""
	if v, ok := criterionNames.Load(criterionID); ok {
		name = v.(string)
	}
	inputType := "numeric"
	if v, ok := criterionInputTypes.Load(criterionID); ok {
		inputType = v.(string)
	}

	var promptText string
	switch inputType {
	case "check":
		promptText = fmt.Sprintf(
			"Отправьте <b>+</b>, если у вас уже есть <b>%s</b>, и <b>-</b>, если нет.\n\n<i>Введите «отмена» чтобы сбросить все ваши данные.</i>",
			name,
		)
	default:
		promptText = fmt.Sprintf(
			"Введите число для показателя <b>%s</b>:\n\n<i>Введите «отмена» чтобы сбросить все ваши данные.</i>",
			name,
		)
	}

	pendingNumericInput.Store(telegramUserID, PendingInput{
		CriterionID:   criterionID,
		CriterionName: name,
		InputType:     inputType,
	})
	h.sendText(chatID, promptText)
}

// handleCancelAll resets all user criteria.
func (h *Handler) handleCancelAll(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := fmt.Sprintf("%d", msg.From.ID)
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(msg.Chat.ID, "Вы не авторизованы.")
		return
	}
	_, err = h.healthClient.ResetCriteria(ctx, &healthpb.ResetCriteriaRequest{
		UserId: chat.UserID.String(),
	})
	if err != nil {
		log.Printf("reset criteria: %v", err)
		h.sendText(msg.Chat.ID, "Не удалось сбросить данные. Попробуйте позже.")
		return
	}
	h.sendWithMainMenu(msg.Chat.ID, "✅ Все ваши данные сброшены.")
}

// handleUserInput processes text input for a pending criterion.
func (h *Handler) handleUserInput(ctx context.Context, msg *tgbotapi.Message, pending PendingInput) {
	chatID := msg.Chat.ID
	telegramUserID := strconv.FormatInt(msg.From.ID, 10)
	text := strings.TrimSpace(msg.Text)

	var value string
	switch pending.InputType {
	case "check":
		switch text {
		case "+":
			value = "1"
		case "-":
			h.sendWithMainMenu(chatID, fmt.Sprintf("Понято — <b>%s</b> отмечен как отсутствующий.", pending.CriterionName))
			return
		default:
			h.sendText(chatID, "Пожалуйста, отправьте <b>+</b> или <b>-</b>.")
			pendingNumericInput.Store(telegramUserID, pending)
			return
		}
	default:
		numVal, err := strconv.ParseFloat(text, 64)
		if err != nil {
			h.sendText(chatID, "Пожалуйста, введите корректное число.")
			pendingNumericInput.Store(telegramUserID, pending)
			return
		}
		value = fmt.Sprintf("%.2f", numVal)
	}

	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(chatID, "Вы не авторизованы.")
		return
	}

	_, err = h.healthClient.SetUserCriterion(ctx, &healthpb.SetUserCriterionRequest{
		UserId:      chat.UserID.String(),
		CriterionId: pending.CriterionID,
		Value:       value,
		Source:      "telegram",
	})
	if err != nil {
		log.Printf("set user criterion: %v", err)
		h.sendText(chatID, "Не удалось сохранить значение. Попробуйте позже.")
		return
	}

	h.sendWithMainMenu(chatID, fmt.Sprintf("✅ <b>%s</b> сохранено!", pending.CriterionName))
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
