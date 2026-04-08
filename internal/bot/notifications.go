package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
)

type NotificationPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// handleRecommendations shows all current recommendations for the user.
func (h *Handler) handleRecommendations(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := strconv.FormatInt(msg.From.ID, 10)
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(msg.Chat.ID, "Вы не авторизованы. Перейдите по ссылке из приложения ЗдравоШаг.")
		return
	}

	resp, err := h.healthClient.GetRecommendations(ctx, &healthpb.GetRecommendationsRequest{
		UserId: chat.UserID.String(),
	})
	if err != nil {
		log.Printf("get recommendations: %v", err)
		h.sendText(msg.Chat.ID, "Не удалось загрузить рекомендации. Попробуйте позже.")
		return
	}

	text := formatRecommendations(resp.GetRecommendations())

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 Рекомендации недели", "show_weekly_recs"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("« Назад", "back_main"),
		),
	)

	m := tgbotapi.NewMessage(msg.Chat.ID, text)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = kb
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send recommendations: %v", err)
	}
}

// handleWeeklyRecommendations shows the user's weekly recommendation plan.
func (h *Handler) handleWeeklyRecommendations(ctx context.Context, chatID int64, telegramUserID string) {
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(chatID, "Вы не авторизованы.")
		return
	}

	resp, err := h.healthClient.GetWeeklyRecommendations(ctx, &healthpb.GetWeeklyRecommendationsRequest{
		UserId: chat.UserID.String(),
	})
	if err != nil {
		log.Printf("get weekly recommendations: %v", err)
		h.sendText(chatID, "Не удалось загрузить рекомендации на неделю. Попробуйте позже.")
		return
	}

	text := formatWeeklyRecommendations(resp)

	m := tgbotapi.NewMessage(chatID, text)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = BackToMainInlineKeyboard()
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send weekly recommendations: %v", err)
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
		weight := item.GetWeight()
		spent := weight == 0
		prefix := ""
		if spent {
			prefix = "~~"
		}
		b.WriteString(fmt.Sprintf("%s %s<b>%s</b>%s\n", icon, prefix, item.GetTitle(), prefix))
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
		log.Printf("unmarshal notification payload: %v", err)
		return
	}

	text := FormatNotification(templateCode, &payload)

	m := tgbotapi.NewMessage(chatID, text)
	m.ParseMode = tgbotapi.ModeHTML
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send notification: %v", err)
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
