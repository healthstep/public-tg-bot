package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

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

// handlePasswordCommand lets a registered user change their password.
// Usage: /password <newpassword>
func (h *Handler) handlePasswordCommand(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := strconv.FormatInt(msg.From.ID, 10)
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(msg.Chat.ID, "Вы не авторизованы. Пройдите регистрацию через ссылку из приложения.")
		return
	}

	text := strings.TrimSpace(msg.Text)
	parts := strings.SplitN(text, " ", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		h.sendText(msg.Chat.ID,
			"Чтобы задать новый пароль, отправьте:\n\n<code>/password НовыйПароль</code>\n\nМинимум 8 символов.")
		return
	}

	newPassword := strings.TrimSpace(parts[1])
	if len(newPassword) < 8 {
		h.sendText(msg.Chat.ID, "Пароль должен содержать минимум 8 символов.")
		return
	}

	_, err = h.usersClient.ChangePassword(ctx, &userspb.ChangePasswordRequest{
		UserId:      chat.UserID.String(),
		NewPassword: newPassword,
	})
	if err != nil {
		log.Printf("change password for %s: %v", chat.UserID, err)
		h.sendText(msg.Chat.ID, "Не удалось изменить пароль. Попробуйте позже.")
		return
	}

	h.sendText(msg.Chat.ID, "✅ Пароль успешно изменён! Используйте его для входа на сайт.")
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

	text := "✅ Регистрация завершена! Добро пожаловать в ЗОШ."

	if resp.GetInitialPassword() != "" {
		text += fmt.Sprintf("\n\n🔑 <b>Ваш пароль для входа на сайт:</b> <code>%s</code>\n\nСохраните его! Изменить пароль: /password", resp.GetInitialPassword())
	}

	if h.siteURL != "" && resp.GetToken() != "" {
		loginURL := h.siteURL + "/auth?token=" + resp.GetToken()
		text += fmt.Sprintf("\n\n🌐 <a href=\"%s\">Войти на сайт одним нажатием</a>", loginURL)
	} else if h.siteURL != "" {
		text += "\n\n🌐 " + h.siteURL
	}

	// Send welcome with password first, then start onboarding for new accounts.
	h.sendText(msg.Chat.ID, text)
	if resp.GetInitialPassword() != "" {
		// New account — ask for gender and date of birth before showing the menu.
		h.sendOnboardingStep1(msg.Chat.ID)
	} else {
		h.sendWithMainMenu(msg.Chat.ID, "")
	}
}
