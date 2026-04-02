package natshandler

import (
	"context"
	"encoding/json"
	"log"
	"strconv"

	"github.com/google/uuid"
	"github.com/helthtech/public-tg-bot/internal/bot"
	"github.com/helthtech/public-tg-bot/internal/repository"
	"github.com/nats-io/nats.go"
)

type TelegramNotification struct {
	UserID       string `json:"user_id"`
	Channel      string `json:"channel"`
	TemplateCode string `json:"template_code"`
	PayloadJSON  string `json:"payload_json"`
}

type NotificationHandler struct {
	chatRepo *repository.ChatRepository
	bot      *bot.Handler
}

func NewNotificationHandler(chatRepo *repository.ChatRepository, bot *bot.Handler) *NotificationHandler {
	return &NotificationHandler{
		chatRepo: chatRepo,
		bot:      bot,
	}
}

func (h *NotificationHandler) Subscribe(nc *nats.Conn) error {
	_, err := nc.Subscribe("notification.telegram", func(msg *nats.Msg) {
		var n TelegramNotification
		if err := json.Unmarshal(msg.Data, &n); err != nil {
			log.Printf("nats notification unmarshal error: %v", err)
			return
		}

		userID, err := uuid.Parse(n.UserID)
		if err != nil {
			log.Printf("nats notification invalid user_id: %v", err)
			return
		}

		chat, err := h.chatRepo.FindByUserID(context.Background(), userID)
		if err != nil || chat == nil {
			log.Printf("nats notification: chat not found for user %s", n.UserID)
			return
		}

		chatID, err := strconv.ParseInt(chat.ChatID, 10, 64)
		if err != nil {
			log.Printf("nats notification: invalid chat_id %s", chat.ChatID)
			return
		}

		h.bot.SendNotification(chatID, n.TemplateCode, n.PayloadJSON)
	})
	return err
}
