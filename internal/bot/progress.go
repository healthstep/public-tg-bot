package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"github.com/helthtech/public-tg-bot/internal/obs"
	"github.com/porebric/logger"
)

func (h *Handler) handleProgress(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := strconv.FormatInt(msg.From.ID, 10)
	h.clearLabUpload(telegramUserID)
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(msg.Chat.ID, "Вы не авторизованы. Перейдите по ссылке из приложения ЗдравоШаг.")
		return
	}

	userID := chat.UserID.String()

	// Get progress stats.
	prog, err := h.healthClient.GetProgress(ctx, &healthpb.GetProgressRequest{UserId: userID})
	if err != nil {
		logger.Error(ctx, err, "get progress")
		h.sendText(msg.Chat.ID, "Не удалось загрузить прогресс. Попробуйте позже.")
		return
	}

	// Get all criteria with user values.
	criteria, err := h.healthClient.GetUserCriteria(ctx, &healthpb.GetUserCriteriaRequest{UserId: userID})
	if err != nil {
		logger.Error(ctx, err, "get user criteria")
		h.sendText(msg.Chat.ID, "Не удалось загрузить данные. Попробуйте позже.")
		return
	}

	text := formatProgress(prog, criteria.GetEntries())

	m := tgbotapi.NewMessage(msg.Chat.ID, text)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = BackToMainInlineKeyboard()
	if _, err := h.bot.Send(m); err != nil {
		obs.BG("tg").Error(err, "send progress")
	}
}

func formatProgress(prog *healthpb.GetProgressResponse, entries []*healthpb.UserCriterionEntry) string {
	var b strings.Builder

	b.WriteString("<b>📊 Мой прогресс</b>\n\n")
	b.WriteString(fmt.Sprintf("Уровень: <b>%s</b>\n", prog.GetLevelLabel()))

	pct := prog.GetPercent()
	filled := int(pct / 10)
	empty := 10 - filled
	bar := strings.Repeat("▓", filled) + strings.Repeat("░", empty)
	b.WriteString(fmt.Sprintf("Прогресс: %s %.0f%%\n", bar, pct))
	b.WriteString(fmt.Sprintf("Заполнено: %d / %d критериев\n\n", prog.GetFilled(), prog.GetTotal()))

	if len(entries) == 0 {
		b.WriteString("Данные пока не добавлены. Нажмите «➕ Добавить данные»!")
		return b.String()
	}

	for _, e := range entries {
		icon := statusEmoji(e.GetStatus())
		b.WriteString(fmt.Sprintf("%s %s", icon, e.GetCriterionName()))
		if e.GetValue() != "" {
			b.WriteString(fmt.Sprintf(" — <b>%s</b>", e.GetValue()))
		}
		b.WriteString("\n")
	}

	return b.String()
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
