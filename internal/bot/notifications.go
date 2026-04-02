package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type NotificationPayload struct {
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	CriterionName string   `json:"criterion_name,omitempty"`
	Actions       []string `json:"actions,omitempty"`
}

func (h *Handler) handleNotificationsMenu(ctx context.Context, msg *tgbotapi.Message) {
	_ = ctx
	text := "<b>🔔 Уведомления</b>\n\n" +
		"Бот будет присылать вам напоминания о:\n" +
		"• Просроченных обследованиях\n" +
		"• Плановых визитах к врачу\n" +
		"• Важных показателях здоровья\n\n" +
		"Уведомления приходят автоматически."

	m := tgbotapi.NewMessage(msg.Chat.ID, text)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = BackToMainInlineKeyboard()
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send notifications menu: %v", err)
	}
}

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
	case "overdue_reminder":
		b.WriteString("🔴 <b>Напоминание</b>\n\n")
		if p.CriterionName != "" {
			b.WriteString(fmt.Sprintf("Показатель <b>%s</b> просрочен.\n", p.CriterionName))
		}
		if p.Body != "" {
			b.WriteString(p.Body + "\n")
		}
		b.WriteString("\nИспользуйте меню «➕ Добавить данные» для обновления.")

	case "visit_reminder":
		b.WriteString("📅 <b>Напоминание о визите</b>\n\n")
		if p.Title != "" {
			b.WriteString(p.Title + "\n")
		}
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
