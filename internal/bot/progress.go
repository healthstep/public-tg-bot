package bot

import (
	"context"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"github.com/helthtech/public-tg-bot/internal/obs"
	"github.com/porebric/logger"
)

func (h *Handler) handleProgress(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := strconv.FormatInt(msg.From.ID, 10)
	h.clearLabUpload(telegramUserID)
	h.clearAnalysisPick(telegramUserID)
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(msg.Chat.ID, "Вы не авторизованы. Перейдите по ссылке из приложения ЗдравоШаг.")
		return
	}

	userID := chat.UserID.String()

	prog, err := h.healthClient.GetProgress(ctx, &healthpb.GetProgressRequest{UserId: userID})
	if err != nil {
		logger.Error(ctx, err, "get progress")
		h.sendText(msg.Chat.ID, "Не удалось загрузить прогресс. Попробуйте позже.")
		return
	}

	criteria, err := h.healthClient.GetUserCriteria(ctx, &healthpb.GetUserCriteriaRequest{UserId: userID})
	if err != nil {
		logger.Error(ctx, err, "get user criteria")
		h.sendText(msg.Chat.ID, "Не удалось загрузить данные. Попробуйте позже.")
		return
	}

	groupsResp, err := h.healthClient.ListGroups(ctx, &healthpb.ListGroupsRequest{})
	if err != nil {
		logger.Error(ctx, err, "list groups for progress")
		h.sendText(msg.Chat.ID, "Не удалось загрузить группы. Попробуйте позже.")
		return
	}

	text := formatProgressGroupedHTML(prog, criteria.GetEntries(), groupsResp.GetGroups())

	m := tgbotapi.NewMessage(msg.Chat.ID, text)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = progressReplyMarkup()
	if _, err := h.bot.Send(m); err != nil {
		obs.BG("tg").Error(err, "send progress")
	}
}

func statusEmoji(status string) string {
	switch status {
	case "ok":
		return "✅"
	case "warning":
		return "⚠️"
	case "critical":
		return "🔴"
	case "empty", "":
		return "⚪"
	default:
		return "⚪"
	}
}
