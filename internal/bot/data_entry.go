package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	userspb "github.com/helthtech/core-users/pkg/proto/users"
	"github.com/helthtech/public-tg-bot/internal/obs"
	"github.com/porebric/logger"
)

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

func (h *Handler) handleAddData(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := fmt.Sprintf("%d", msg.From.ID)
	h.clearLabUpload(telegramUserID)
	userSex := h.getUserSex(ctx, telegramUserID)

	chat, _ := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	userID := ""
	if chat != nil && chat.UserID != nil {
		userID = chat.UserID.String()
	}

	groupResp, err := h.healthClient.ListGroups(ctx, &healthpb.ListGroupsRequest{})
	if err != nil {
		logger.Error(ctx, err, "list groups")
	}

	criteriaResp, err := h.healthClient.ListCriteria(ctx, &healthpb.ListCriteriaRequest{
		UserId:  userID,
		UserSex: userSex,
	})
	if err != nil {
		logger.Error(ctx, err, "list criteria")
		h.sendText(msg.Chat.ID, "Не удалось загрузить список показателей. Попробуйте позже.")
		return
	}

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

	for _, c := range criteriaResp.GetCriteria() {
		criterionNames.Store(c.GetId(), c.GetName())
		criterionInputTypes.Store(c.GetId(), c.GetInputType())
	}

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
		"➕ <b>Добавить данные</b>\n\n"+
			"📄 В этот же чат можно просто отправить <b>PDF</b> с анализами (до 5 за раз) — бот извлечёт показатели; подтвердите или отклоните, как в личном кабинете на сайте.\n\n"+
			"Выберите группу показателей:\n\n<i>Введите «отмена» в любой момент, чтобы сбросить все ваши данные.</i>")
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := h.bot.Send(m); err != nil {
		logger.Error(ctx, err, "send groups list")
	}
}

func (h *Handler) handleGroupSelect(ctx context.Context, chatID int64, telegramUserID string, groupID string) {
	val, ok := criterionGroups.Load(groupID)
	if !ok {
		h.sendText(chatID, "Группа не найдена. Попробуйте ещё раз.")
		return
	}
	criteria := val.([]*healthpb.Criterion)

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
		logger.Error(ctx, err, "send group criteria list")
	}
}

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
		"➕ <b>Добавить данные</b>\n\n"+
			"📄 Сюда же можно отправить <b>PDF</b> с анализами — бот обработает их без выбора в меню (до 5 за раз).\n\n"+
			"Выберите показатель:\n\n<i>Введите «отмена» в любой момент, чтобы сбросить все ваши данные.</i>")
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := h.bot.Send(m); err != nil {
		obs.BG("tg").Error(err, "send criteria list")
	}
}

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

func (h *Handler) handleCancelAll(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := fmt.Sprintf("%d", msg.From.ID)
	h.clearLabUpload(telegramUserID)
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(msg.Chat.ID, "Вы не авторизованы.")
		return
	}
	_, err = h.healthClient.ResetCriteria(ctx, &healthpb.ResetCriteriaRequest{
		UserId: chat.UserID.String(),
	})
	if err != nil {
		logger.Error(ctx, err, "reset criteria")
		h.sendText(msg.Chat.ID, "Не удалось сбросить данные. Попробуйте позже.")
		return
	}
	h.sendWithMainMenu(msg.Chat.ID, "✅ Все ваши данные сброшены.")
}

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

	pendingDateSelection.Store(telegramUserID, PendingDate{
		Kind:          "criterion",
		CriterionID:   pending.CriterionID,
		CriterionName: pending.CriterionName,
		Value:         value,
	})
	h.sendDateKeyboard(chatID, fmt.Sprintf("Когда сдан <b>%s</b>?", pending.CriterionName))
}

func dateKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 Сегодня", "date_today"),
			tgbotapi.NewInlineKeyboardButtonData("📅 Вчера", "date_yesterday"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Ввести дату", "date_pick"),
			tgbotapi.NewInlineKeyboardButtonData("⏭ Пропустить", "date_skip"),
		),
	)
}

func (h *Handler) sendDateKeyboard(chatID int64, text string) {
	m := tgbotapi.NewMessage(chatID, text)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = dateKeyboard()
	if _, err := h.bot.Send(m); err != nil {
		obs.BG("tg").Error(err, "send date keyboard", "chat_id", chatID)
	}
}

func (h *Handler) handleDateCallback(ctx context.Context, chatID int64, telegramUserID string, data string) {
	val, ok := pendingDateSelection.Load(telegramUserID)
	if !ok {
		h.sendWithMainMenu(chatID, "Нет ожидающих данных. Попробуйте заново.")
		return
	}
	pd := val.(PendingDate)

	switch data {
	case "date_today":
		pendingDateSelection.Delete(telegramUserID)
		h.finishWithDate(ctx, chatID, telegramUserID, pd, time.Now().Format("2006-01-02"))
	case "date_yesterday":
		pendingDateSelection.Delete(telegramUserID)
		h.finishWithDate(ctx, chatID, telegramUserID, pd, time.Now().AddDate(0, 0, -1).Format("2006-01-02"))
	case "date_skip":
		pendingDateSelection.Delete(telegramUserID)
		h.finishWithDate(ctx, chatID, telegramUserID, pd, "")
	case "date_pick":
		pd.WaitingForText = true
		pendingDateSelection.Store(telegramUserID, pd)
		h.sendText(chatID, "Введите дату в формате <b>ДД.ММ.ГГГГ</b> (например, 15.03.2025):")
	}
}

func (h *Handler) handleDateTextInput(ctx context.Context, msg *tgbotapi.Message, pd PendingDate) {
	chatID := msg.Chat.ID
	telegramUserID := fmt.Sprintf("%d", msg.From.ID)
	text := strings.TrimSpace(msg.Text)

	t, err := time.Parse("02.01.2006", text)
	if err != nil {
		h.sendText(chatID, "Неверный формат. Введите дату как <b>ДД.ММ.ГГГГ</b> (например, 15.03.2025):")
		pd.WaitingForText = true
		pendingDateSelection.Store(telegramUserID, pd)
		return
	}
	if t.After(time.Now()) {
		h.sendText(chatID, "Дата не может быть в будущем. Введите корректную дату:")
		pd.WaitingForText = true
		pendingDateSelection.Store(telegramUserID, pd)
		return
	}
	h.finishWithDate(ctx, chatID, telegramUserID, pd, t.Format("2006-01-02"))
}

func (h *Handler) finishWithDate(ctx context.Context, chatID int64, telegramUserID string, pd PendingDate, measuredAt string) {
	switch pd.Kind {
	case "criterion":
		h.saveCriterionWithDate(ctx, chatID, telegramUserID, pd, measuredAt)
	case "lab":
		h.handleLabConfirm(ctx, chatID, telegramUserID, true, measuredAt)
	}
}

func (h *Handler) saveCriterionWithDate(ctx context.Context, chatID int64, telegramUserID string, pd PendingDate, measuredAt string) {
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(chatID, "Вы не авторизованы.")
		return
	}

	_, err = h.healthClient.SetUserCriterion(ctx, &healthpb.SetUserCriterionRequest{
		UserId:      chat.UserID.String(),
		CriterionId: pd.CriterionID,
		Value:       pd.Value,
		Source:      "telegram",
		MeasuredAt:  measuredAt,
	})
	if err != nil {
		logger.Error(ctx, err, "set user criterion")
		h.sendText(chatID, "Не удалось сохранить значение. Попробуйте позже.")
		return
	}

	h.sendWithMainMenu(chatID, fmt.Sprintf("✅ <b>%s</b> сохранено!", pd.CriterionName))
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
