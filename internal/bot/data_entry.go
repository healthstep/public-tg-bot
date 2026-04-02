package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h *Handler) handleAddData(ctx context.Context, msg *tgbotapi.Message) {
	_ = ctx
	m := tgbotapi.NewMessage(msg.Chat.ID, "Выберите способ добавления данных:")
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = AddDataInlineKeyboard()
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send add data menu: %v", err)
	}
}

func (h *Handler) handleManualEntryList(ctx context.Context, chatID int64) {
	resp, err := h.healthClient.ListCriteria(ctx, &healthpb.ListCriteriaRequest{})
	if err != nil {
		log.Printf("list criteria: %v", err)
		h.sendText(chatID, "Не удалось загрузить список критериев. Попробуйте позже.")
		return
	}

	if len(resp.GetCriteria()) == 0 {
		h.sendText(chatID, "Нет доступных критериев для ввода.")
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range resp.GetCriteria() {
		if !c.GetIsActive() {
			continue
		}
		label := c.GetName()
		if c.GetUnit() != "" {
			label += fmt.Sprintf(" (%s)", c.GetUnit())
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, "criterion_"+c.GetId()),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Назад", "back_main"),
	))

	m := tgbotapi.NewMessage(chatID, "Выберите показатель для ввода:")
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send criteria list: %v", err)
	}
}

func (h *Handler) handleMarkDoneList(ctx context.Context, chatID int64) {
	resp, err := h.healthClient.ListCriteria(ctx, &healthpb.ListCriteriaRequest{})
	if err != nil {
		log.Printf("list criteria for mark done: %v", err)
		h.sendText(chatID, "Не удалось загрузить список. Попробуйте позже.")
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range resp.GetCriteria() {
		if !c.GetIsActive() {
			continue
		}
		for _, mode := range c.GetInputModes() {
			if mode == "mark_done" {
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(c.GetName(), "markdone_"+c.GetId()),
				))
				break
			}
		}
	}

	if len(rows) == 0 {
		h.sendText(chatID, "Нет доступных визитов для отметки.")
		return
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Назад", "back_main"),
	))

	m := tgbotapi.NewMessage(chatID, "Выберите визит для отметки:")
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send mark done list: %v", err)
	}
}

func (h *Handler) handleMarkDone(ctx context.Context, chatID int64, telegramUserID int64, criterionID string) {
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, strconv.FormatInt(telegramUserID, 10))
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(chatID, "Вы не авторизованы. Перейдите по ссылке из приложения ЗдравоШаг.")
		return
	}

	resp, err := h.healthClient.CreateMarkDoneEvent(ctx, &healthpb.CreateMarkDoneEventRequest{
		UserId:            chat.UserID.String(),
		HealthCriterionId: criterionID,
		OccurredAt:        timestamppb.Now(),
		Source:            "telegram",
	})
	if err != nil {
		log.Printf("create mark done event: %v", err)
		h.sendText(chatID, "Ошибка при сохранении. Попробуйте позже.")
		return
	}

	_ = resp
	h.sendWithMainMenu(chatID, "Визит отмечен! ✅")
}
