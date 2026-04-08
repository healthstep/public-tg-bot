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

// handleAddData shows the list of criteria groups.
func (h *Handler) handleAddData(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := fmt.Sprintf("%d", msg.From.ID)
	userSex := h.getUserSex(ctx, telegramUserID)

	chat, _ := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	userID := ""
	if chat != nil && chat.UserID != nil {
		userID = chat.UserID.String()
	}

	// Fetch groups.
	groupResp, err := h.healthClient.ListGroups(ctx, &healthpb.ListGroupsRequest{})
	if err != nil {
		log.Printf("list groups: %v", err)
	}

	// Fetch all criteria with user values.
	criteriaResp, err := h.healthClient.ListCriteria(ctx, &healthpb.ListCriteriaRequest{
		UserId:  userID,
		UserSex: userSex,
	})
	if err != nil {
		log.Printf("list criteria: %v", err)
		h.sendText(msg.Chat.ID, "Не удалось загрузить список показателей. Попробуйте позже.")
		return
	}

	// Get user values for ✅ markers.
	var userEntries []*healthpb.UserCriterionEntry
	if userID != "" {
		ucResp, err := h.healthClient.GetUserCriteria(ctx, &healthpb.GetUserCriteriaRequest{UserId: userID, UserSex: userSex})
		if err == nil {
			userEntries = ucResp.GetEntries()
		}
	}
	filledMap := make(map[string]bool)
	for _, e := range userEntries {
		if e.GetValue() != "" {
			filledMap[e.GetCriterionId()] = true
		}
	}

	// Cache criterion names and input types.
	for _, c := range criteriaResp.GetCriteria() {
		criterionNames.Store(c.GetId(), c.GetName())
		criterionInputTypes.Store(c.GetId(), c.GetInputType())
	}

	// Group criteria by group_id.
	byGroup := make(map[string][]*healthpb.Criterion)
	ungrouped := []*healthpb.Criterion{}
	for _, c := range criteriaResp.GetCriteria() {
		gid := c.GetGroupId()
		if gid == "" {
			ungrouped = append(ungrouped, c)
		} else {
			byGroup[gid] = append(byGroup[gid], c)
		}
	}

	groups := groupResp.GetGroups()
	if len(groups) == 0 {
		// No groups — show flat list (fallback).
		h.showFlatCriteriaList(msg.Chat.ID, criteriaResp.GetCriteria(), filledMap)
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, g := range groups {
		items := byGroup[g.GetId()]
		if len(items) == 0 {
			continue
		}
		total := len(items)
		filled := 0
		for _, c := range items {
			if filledMap[c.GetId()] {
				filled++
			}
		}
		label := fmt.Sprintf("%s (%d/%d)", g.GetName(), filled, total)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, "group_"+g.GetId()),
		))
		// Cache group criteria list.
		criterionGroups.Store(g.GetId(), items)
	}

	if len(ungrouped) > 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Другое", "group___ungrouped"),
		))
		criterionGroups.Store("__ungrouped", ungrouped)
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Назад", "back_main"),
	))

	m := tgbotapi.NewMessage(msg.Chat.ID,
		"➕ <b>Добавить данные</b>\n\nВыберите группу показателей:\n\n<i>Введите «отмена» в любой момент, чтобы сбросить все ваши данные.</i>")
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send groups list: %v", err)
	}
}

// handleGroupSelect shows the criteria list for a specific group.
func (h *Handler) handleGroupSelect(ctx context.Context, chatID int64, telegramUserID string, groupID string) {
	val, ok := criterionGroups.Load(groupID)
	if !ok {
		h.sendText(chatID, "Группа не найдена. Попробуйте ещё раз.")
		return
	}
	criteria := val.([]*healthpb.Criterion)

	// Get user values for ✅ markers.
	chat, _ := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	filledMap := make(map[string]bool)
	if chat != nil && chat.UserID != nil {
		userSex := h.getUserSex(ctx, telegramUserID)
		ucResp, err := h.healthClient.GetUserCriteria(ctx, &healthpb.GetUserCriteriaRequest{
			UserId:  chat.UserID.String(),
			UserSex: userSex,
		})
		if err == nil {
			for _, e := range ucResp.GetEntries() {
				if e.GetValue() != "" {
					filledMap[e.GetCriterionId()] = true
				}
			}
		}
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range criteria {
		label := c.GetName()
		if filledMap[c.GetId()] {
			label = "✅ " + label
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, "criterion_select_"+c.GetId()),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Назад", "back_criteria"),
	))

	m := tgbotapi.NewMessage(chatID, "Выберите показатель:")
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send group criteria list: %v", err)
	}
}

// showFlatCriteriaList renders a flat (ungrouped) criteria list.
func (h *Handler) showFlatCriteriaList(chatID int64, criteria []*healthpb.Criterion, filledMap map[string]bool) {
	if len(criteria) == 0 {
		h.sendText(chatID, "Нет доступных показателей.")
		return
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range criteria {
		label := c.GetName()
		if filledMap[c.GetId()] {
			label = "✅ " + label
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, "criterion_select_"+c.GetId()),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Назад", "back_main"),
	))
	m := tgbotapi.NewMessage(chatID,
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
	case "boolean":
		promptText = fmt.Sprintf(
			"Отправьте <b>+</b>, если результат <b>%s</b> положительный, и <b>-</b>, если отрицательный.\n\n<i>Введите «отмена» чтобы сбросить все ваши данные.</i>",
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
	case "boolean":
		switch text {
		case "+":
			value = "1"
		case "-":
			value = "0"
		default:
			h.sendText(chatID, "Пожалуйста, отправьте <b>+</b> (положительный) или <b>-</b> (отрицательный).")
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
