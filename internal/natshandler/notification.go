package natshandler

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/google/uuid"
	"github.com/helthtech/public-tg-bot/internal/bot"
	"github.com/helthtech/public-tg-bot/internal/obs"
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
			obs.BG("nats").Error(err, "nats notification unmarshal")
			return
		}

		userID, err := uuid.Parse(n.UserID)
		if err != nil {
			obs.BG("nats").Error(err, "nats notification invalid user_id")
			return
		}

		chat, err := h.chatRepo.FindByUserID(context.Background(), userID)
		if err != nil || chat == nil {
			obs.BG("nats").Warn("nats notification: chat not found", "user_id", n.UserID)
			return
		}

		chatID, err := strconv.ParseInt(chat.ChatID, 10, 64)
		if err != nil {
			obs.BG("nats").Warn("nats notification: invalid chat_id", "chat_id", chat.ChatID)
			return
		}

		h.bot.SendNotification(chatID, n.TemplateCode, n.PayloadJSON)
	})
	return err
}
