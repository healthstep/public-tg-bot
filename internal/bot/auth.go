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

	telegramUserID := strconv.FormatInt(msg.From.ID, 10)

	existing, _ := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if existing != nil && existing.UserID != nil {
		h.handleLogin(ctx, msg, existing, key)
		return
	}

	h.handleRegistration(ctx, msg, resp.GetProvisionalUserId(), key)
}

func (h *Handler) handleLogin(ctx context.Context, msg *tgbotapi.Message, chat *model.Chat, authKey string) {
	userResp, err := h.usersClient.GetUser(ctx, &userspb.GetUserRequest{UserId: chat.UserID.String()})
	if err != nil {
		log.Printf("get user for login: %v", err)
		h.sendText(msg.Chat.ID, "Произошла ошибка. Попробуйте позже.")
		return
	}

	verifyResp, err := h.usersClient.VerifyPhone(ctx, &userspb.VerifyPhoneRequest{
		PhoneE164: userResp.GetPhoneE164(),
		AuthKey:   authKey,
		Platform:  "telegram",
	})
	if err != nil {
		log.Printf("login verify phone: %v", err)
		h.sendText(msg.Chat.ID, "Произошла ошибка. Попробуйте позже.")
		return
	}

	text := "С возвращением! Вы успешно авторизованы."
	if h.siteURL != "" && verifyResp.GetToken() != "" {
		loginURL := h.siteURL + "/auth?token=" + verifyResp.GetToken()
		text += fmt.Sprintf("\n\n🌐 <a href=\"%s\">Войти на сайт одним нажатием</a>", loginURL)
	}
	h.sendWithMainMenu(msg.Chat.ID, text)
}

func (h *Handler) handleRegistration(ctx context.Context, msg *tgbotapi.Message, provisionalUserID string, authKey string) {
	provID, err := uuid.Parse(provisionalUserID)
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

	h.authKeys.Store(telegramUserID, authKey)
	h.requestPhone(msg.Chat.ID)
}

func (h *Handler) requestPhone(chatID int64) {
	text := "Для завершения регистрации поделитесь номером телефона.\n\n" +
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
		h.sendText(msg.Chat.ID, "Сначала перейдите по ссылке авторизации из приложении.")
		return
	}

	if chat.ProvisionalUserID == nil {
		h.sendWithMainMenu(msg.Chat.ID, "Вы уже авторизованы!")
		return
	}

	var authKey string
	if v, ok := h.authKeys.LoadAndDelete(telegramUserID); ok {
		authKey = v.(string)
	}

	resp, err := h.usersClient.VerifyPhone(ctx, &userspb.VerifyPhoneRequest{
		PhoneE164:         phone,
		ProvisionalUserId: chat.ProvisionalUserID.String(),
		AuthKey:           authKey,
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

	text := "Регистрация завершена! Добро пожаловать в ЗдравоШаг. 🎉"
	if h.siteURL != "" && resp.GetToken() != "" {
		loginURL := h.siteURL + "/auth?token=" + resp.GetToken()
		text += fmt.Sprintf("\n\n🌐 <a href=\"%s\">Войти на сайт одним нажатием</a>", loginURL)
	}
	h.sendWithMainMenu(msg.Chat.ID, text)
}
