package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"github.com/helthtech/public-tg-bot/internal/obs"
	"github.com/porebric/logger"
)

type NotificationPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}


// handleWeeklyRecommendations shows the user's weekly recommendation plan.
func (h *Handler) handleWeeklyRecommendations(ctx context.Context, chatID int64, telegramUserID string) {
	h.clearLabUpload(telegramUserID)
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(chatID, "Вы не авторизованы.")
		return
	}

	resp, err := h.healthClient.GetWeeklyRecommendations(ctx, &healthpb.GetWeeklyRecommendationsRequest{
		UserId: chat.UserID.String(),
	})
	if err != nil {
		logger.Error(ctx, err, "get weekly recommendations")
		h.sendText(chatID, "Не удалось загрузить рекомендации на неделю. Попробуйте позже.")
		return
	}

	text := formatWeeklyRecommendations(resp)

	m := tgbotapi.NewMessage(chatID, text)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = BackToMainInlineKeyboard()
	if _, err := h.bot.Send(m); err != nil {
		obs.BG("tg").Error(err, "send weekly recommendations")
	}
}

func formatRecommendations(recs []*healthpb.Recommendation) string {
	var b strings.Builder
	b.WriteString("<b>💡 Рекомендации</b>\n\n")

	if len(recs) == 0 {
		b.WriteString("🎉 Всё отлично! Все показатели заполнены и в норме.")
		return b.String()
	}

	for _, r := range recs {
		icon := severityEmoji(r.GetSeverity())
		b.WriteString(fmt.Sprintf("%s <b>%s</b>\n", icon, r.GetCriterionName()))
		if r.GetText() != "" {
			b.WriteString(fmt.Sprintf("   %s\n", r.GetText()))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func severityEmoji(severity string) string {
	switch severity {
	case "critical":
		return "🔴"
	case "warning":
		return "⚠️"
	case "ok":
		return "✅"
	default:
		return "💡"
	}
}

func formatWeeklyRecommendations(resp *healthpb.GetWeeklyRecommendationsResponse) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>📅 Рекомендации на неделю</b> (с %s)\n\n", resp.GetWeekStart()))

	items := resp.GetItems()
	if len(items) == 0 {
		b.WriteString("🎉 На эту неделю рекомендаций нет — все показатели в норме!")
		return b.String()
	}

	for _, item := range items {
		icon := recTypeIcon(item.GetType())
		spent := item.GetWeight() == 0
		if spent {
			b.WriteString(fmt.Sprintf("%s <s>%s</s>\n", icon, item.GetTitle()))
		} else {
			b.WriteString(fmt.Sprintf("%s <b>%s</b>\n", icon, item.GetTitle()))
		}
		if item.GetCriterionName() != "" {
			b.WriteString(fmt.Sprintf("   <i>%s</i>\n", item.GetCriterionName()))
		}
	}

	return b.String()
}

func recTypeIcon(t string) string {
	switch t {
	case "reminder":
		return "🔔"
	case "alarm":
		return "🚨"
	case "expiration_reminder":
		return "⏰"
	default:
		return "💡"
	}
}

// SendNotification sends a bot notification message to the chat.
func (h *Handler) SendNotification(chatID int64, templateCode string, payloadJSON string) {
	var payload NotificationPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		obs.BG("notification").Error(err, "unmarshal notification payload")
		return
	}

	text := FormatNotification(templateCode, &payload)

	m := tgbotapi.NewMessage(chatID, text)
	m.ParseMode = tgbotapi.ModeHTML
	if _, err := h.bot.Send(m); err != nil {
		obs.BG("notification").Error(err, "send notification", "chat_id", chatID, "template", templateCode)
	} else {
		obs.BG("notification").Info("send notification", "chat_id", chatID, "template", templateCode)
	}
}

func FormatNotification(templateCode string, p *NotificationPayload) string {
	var b strings.Builder

	switch templateCode {
	case "daily_rec":
		b.WriteString("💡 <b>Рекомендация дня</b>\n\n")
		if p.Body != "" {
			b.WriteString(p.Body + "\n")
		}

	default:
		b.WriteString("🔔 <b>Уведомление</b>\n\n")
		if p.Title != "" {
			b.WriteString(fmt.Sprintf("<b>%s</b>\n", p.Title))
		}
		if p.Body != "" {
			b.WriteString(p.Body + "\n")
		}
	}

	return b.String()
}
