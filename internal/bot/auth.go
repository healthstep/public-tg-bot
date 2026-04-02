package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	userspb "github.com/helthtech/core-users/pkg/proto/users"
	"github.com/helthtech/public-tg-bot/internal/model"
)

func (h *Handler) handleStartWithKey(ctx context.Context, msg *tgbotapi.Message, key string) {
	resp, err := h.usersClient.ResolveAuthKey(ctx, &userspb.ResolveAuthKeyRequest{Key: key})
	if err != nil || !resp.GetValid() {
		h.sendText(msg.Chat.ID, "Ссылка недействительна или устарела. Попробуйте получить новую ссылку в приложении.")
		return
	}

	provID, err := uuid.Parse(resp.GetProvisionalUserId())
	if err != nil {
		log.Printf("parse provisional user id: %v", err)
		h.sendText(msg.Chat.ID, "Произошла ошибка. Попробуйте позже.")
		return
	}

	telegramUserID := strconv.FormatInt(msg.From.ID, 10)
	chatIDStr := strconv.FormatInt(msg.Chat.ID, 10)
	username := &msg.From.UserName

	chat := &model.Chat{
		ProvisionalUserID: &provID,
		TelegramUserID:    telegramUserID,
		ChatID:            chatIDStr,
		Username:          username,
	}

	if err := h.chatRepo.Upsert(ctx, chat); err != nil {
		log.Printf("upsert chat: %v", err)
		h.sendText(msg.Chat.ID, "Произошла ошибка. Попробуйте позже.")
		return
	}

	h.requestPhone(msg.Chat.ID, key)
}

func (h *Handler) requestPhone(chatID int64, _ string) {
	text := "Для завершения авторизации поделитесь номером телефона.\n\n" +
		"Нажмите кнопку ниже:"

	contactBtn := tgbotapi.NewKeyboardButtonContact("📱 Поделиться номером")
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(contactBtn),
	)
	kb.OneTimeKeyboard = true
	kb.ResizeKeyboard = true

	m := tgbotapi.NewMessage(chatID, text)
	m.ReplyMarkup = kb
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send phone request: %v", err)
	}
}

func (h *Handler) handlePhoneShared(ctx context.Context, msg *tgbotapi.Message) {
	if msg.Contact == nil {
		return
	}

	phone := msg.Contact.PhoneNumber
	if phone == "" {
		h.sendText(msg.Chat.ID, "Не удалось получить номер телефона.")
		return
	}

	if phone[0] != '+' {
		phone = "+" + phone
	}

	telegramUserID := strconv.FormatInt(msg.From.ID, 10)
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil {
		h.sendText(msg.Chat.ID, "Сначала перейдите по ссылке авторизации из приложения.")
		return
	}

	if chat.ProvisionalUserID == nil {
		h.sendWithMainMenu(msg.Chat.ID, "Вы уже авторизованы!")
		return
	}

	resp, err := h.usersClient.VerifyPhone(ctx, &userspb.VerifyPhoneRequest{
		PhoneE164:         phone,
		ProvisionalUserId: chat.ProvisionalUserID.String(),
		Platform:          "telegram",
	})
	if err != nil {
		log.Printf("verify phone: %v", err)
		h.sendText(msg.Chat.ID, "Ошибка верификации. Попробуйте позже.")
		return
	}

	userID, err := uuid.Parse(resp.GetUserId())
	if err != nil {
		log.Printf("parse user id: %v", err)
		h.sendText(msg.Chat.ID, "Произошла ошибка. Попробуйте позже.")
		return
	}

	if err := h.chatRepo.UpdateUserID(ctx, telegramUserID, userID); err != nil {
		log.Printf("update user id: %v", err)
	}

	h.sendWithMainMenu(msg.Chat.ID,
		fmt.Sprintf("Авторизация успешна! Добро пожаловать в ЗдравоШаг."),
	)
}
