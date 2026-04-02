package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
)

func (h *Handler) handleProgress(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := strconv.FormatInt(msg.From.ID, 10)
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(msg.Chat.ID, "Вы не авторизованы. Перейдите по ссылке из приложения ЗдравоШаг.")
		return
	}

	dash, err := h.healthClient.GetDashboard(ctx, &healthpb.GetDashboardRequest{
		UserId: chat.UserID.String(),
	})
	if err != nil {
		log.Printf("get dashboard: %v", err)
		h.sendText(msg.Chat.ID, "Не удалось загрузить прогресс. Попробуйте позже.")
		return
	}

	text := formatDashboard(dash)

	m := tgbotapi.NewMessage(msg.Chat.ID, text)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = BackToMainInlineKeyboard()
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send progress: %v", err)
	}
}

func formatDashboard(d *healthpb.DashboardResponse) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("<b>📊 Ваш прогресс</b>\n\n"))
	b.WriteString(fmt.Sprintf("Уровень: <b>%s</b>\n", d.GetLevel()))

	pct := d.GetProgressPercent()
	filled := int(pct / 10)
	empty := 10 - filled
	bar := strings.Repeat("▓", filled) + strings.Repeat("░", empty)
	b.WriteString(fmt.Sprintf("Прогресс: %s %.0f%%\n\n", bar, pct))

	b.WriteString(fmt.Sprintf("Заполнено: %d / %d критериев\n", d.GetFilledCriteria(), d.GetTotalCriteria()))
	if d.GetOverdueCriteria() > 0 {
		b.WriteString(fmt.Sprintf("⚠️ Просрочено: %d\n", d.GetOverdueCriteria()))
	}

	if len(d.GetStates()) > 0 {
		b.WriteString("\n<b>Критерии:</b>\n")
		for _, s := range d.GetStates() {
			icon := statusIcon(s.GetStatus())
			b.WriteString(fmt.Sprintf("%s %s", icon, s.GetCriterionName()))
			if s.GetLastValueSummary() != "" {
				b.WriteString(fmt.Sprintf(" — %s", s.GetLastValueSummary()))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func statusIcon(status string) string {
	switch status {
	case "ok", "filled":
		return "✅"
	case "overdue":
		return "🔴"
	case "pending":
		return "🟡"
	default:
		return "⚪"
	}
}

func (h *Handler) handleChecklist(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := strconv.FormatInt(msg.From.ID, 10)
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(msg.Chat.ID, "Вы не авторизованы. Перейдите по ссылке из приложения ЗдравоШаг.")
		return
	}

	resp, err := h.healthClient.GetUserCriterionStates(ctx, &healthpb.GetUserCriterionStatesRequest{
		UserId: chat.UserID.String(),
	})
	if err != nil {
		log.Printf("get user criterion states: %v", err)
		h.sendText(msg.Chat.ID, "Не удалось загрузить чеклист. Попробуйте позже.")
		return
	}

	var b strings.Builder
	b.WriteString("<b>✅ Чеклист здоровья</b>\n\n")

	if len(resp.GetStates()) == 0 {
		b.WriteString("Нет данных. Начните добавлять показатели здоровья!")
	} else {
		for _, s := range resp.GetStates() {
			icon := statusIcon(s.GetStatus())
			b.WriteString(fmt.Sprintf("%s <b>%s</b>\n", icon, s.GetCriterionName()))
			if s.GetLastValueSummary() != "" {
				b.WriteString(fmt.Sprintf("   Последнее: %s\n", s.GetLastValueSummary()))
			}
			if s.GetRecommendation() != "" {
				b.WriteString(fmt.Sprintf("   💡 %s\n", s.GetRecommendation()))
			}
			b.WriteString("\n")
		}
	}

	m := tgbotapi.NewMessage(msg.Chat.ID, b.String())
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = BackToMainInlineKeyboard()
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send checklist: %v", err)
	}
}
