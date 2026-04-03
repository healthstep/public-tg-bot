package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/gorilla/mux"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	userspb "github.com/helthtech/core-users/pkg/proto/users"
	"github.com/helthtech/public-tg-bot/internal/repository"
)

type Handler struct {
	bot          *tgbotapi.BotAPI
	chatRepo     *repository.ChatRepository
	usersClient  userspb.UserServiceClient
	healthClient healthpb.HealthServiceClient
	botToken     string
	authKeys     sync.Map
}

func NewHandler(
	bot *tgbotapi.BotAPI,
	chatRepo *repository.ChatRepository,
	usersClient userspb.UserServiceClient,
	healthClient healthpb.HealthServiceClient,
	botToken string,
) *Handler {
	return &Handler{
		bot:          bot,
		chatRepo:     chatRepo,
		usersClient:  usersClient,
		healthClient: healthClient,
		botToken:     botToken,
	}
}

func (h *Handler) WebhookHTTP(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if vars["token"] != h.botToken {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var update tgbotapi.Update
	if err := json.Unmarshal(body, &update); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	h.handleUpdate(r.Context(), &update)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleUpdate(ctx context.Context, update *tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in handleUpdate: %v", r)
		}
	}()

	switch {
	case update.Message != nil:
		h.handleMessage(ctx, update.Message)
	case update.CallbackQuery != nil:
		h.handleCallback(ctx, update.CallbackQuery)
	}
}

func (h *Handler) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	if msg.Contact != nil {
		h.handlePhoneShared(ctx, msg)
		return
	}

	text := strings.TrimSpace(msg.Text)

	switch {
	case strings.HasPrefix(text, "/start"):
		args := strings.TrimSpace(strings.TrimPrefix(text, "/start"))
		if args != "" {
			h.handleStartWithKey(ctx, msg, args)
		} else {
			h.handleStartNoKey(ctx, msg)
		}
		return
	}

	switch text {
	case BtnAddData:
		h.handleAddData(ctx, msg)
	case BtnProgress:
		h.handleProgress(ctx, msg)
	case BtnChecklist:
		h.handleChecklist(ctx, msg)
	case BtnNotifications:
		h.handleNotificationsMenu(ctx, msg)
	default:
		h.sendWithMainMenu(msg.Chat.ID, "Пожалуйста, выберите действие из меню.")
	}
}

func (h *Handler) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	callback := tgbotapi.NewCallback(cb.ID, "")
	h.bot.Send(callback)

	data := cb.Data
	chatID := cb.Message.Chat.ID

	switch {
	case data == "add_upload":
		h.sendText(chatID, "Загрузите файл (PDF, фото анализов) в этот чат, и мы обработаем его автоматически.")

	case data == "add_manual":
		h.handleManualEntryList(ctx, chatID)

	case data == "add_visit":
		h.handleMarkDoneList(ctx, chatID)

	case strings.HasPrefix(data, "criterion_"):
		criterionID := strings.TrimPrefix(data, "criterion_")
		h.sendText(chatID, fmt.Sprintf("Введите значение для критерия.\nОтправьте число.\n\n(criterion_id: %s)", criterionID))

	case strings.HasPrefix(data, "markdone_"):
		criterionID := strings.TrimPrefix(data, "markdone_")
		h.handleMarkDone(ctx, chatID, cb.From.ID, criterionID)

	case data == "onboarding_next_1":
		h.sendOnboardingStep2(chatID)

	case data == "onboarding_next_2":
		h.sendOnboardingStep3(chatID)

	case data == "onboarding_done":
		h.sendMainMenu(chatID)

	case data == "back_main":
		h.sendMainMenu(chatID)
	}
}

func (h *Handler) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	if _, err := h.bot.Send(msg); err != nil {
		log.Printf("send message error: %v", err)
	}
}

func (h *Handler) sendWithMainMenu(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = MainMenuKeyboard()
	if _, err := h.bot.Send(msg); err != nil {
		log.Printf("send message error: %v", err)
	}
}
