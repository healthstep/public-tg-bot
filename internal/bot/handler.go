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

// pendingNumericInput stores users waiting to type a criterion value.
// key: telegramUserID (string), value: PendingInput
var pendingNumericInput sync.Map

// criterionNames caches criterionID -> criterionName to avoid embedding names in
// callback data (Telegram limits callback_data to 64 bytes).
var criterionNames sync.Map

// criterionInputTypes caches criterionID -> inputType ("numeric" or "check").
var criterionInputTypes sync.Map

type PendingInput struct {
	CriterionID   string
	CriterionName string
	InputType     string // "numeric" or "check"
}

type Handler struct {
	bot          *tgbotapi.BotAPI
	chatRepo     *repository.ChatRepository
	usersClient  userspb.UserServiceClient
	healthClient healthpb.HealthServiceClient
	botToken     string
	siteURL      string
	authKeys     sync.Map
}

func NewHandler(
	bot *tgbotapi.BotAPI,
	chatRepo *repository.ChatRepository,
	usersClient userspb.UserServiceClient,
	healthClient healthpb.HealthServiceClient,
	botToken string,
	siteURL string,
) *Handler {
	return &Handler{
		bot:          bot,
		chatRepo:     chatRepo,
		usersClient:  usersClient,
		healthClient: healthClient,
		botToken:     botToken,
		siteURL:      siteURL,
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

	telegramUserID := fmt.Sprintf("%d", msg.From.ID)
	text := strings.TrimSpace(msg.Text)

	// "cancel" resets all criteria.
	if strings.EqualFold(text, "отмена") || strings.EqualFold(text, "cancel") {
		pendingNumericInput.Delete(telegramUserID)
		h.handleCancelAll(ctx, msg)
		return
	}

	// Check if user is waiting to type a criterion value.
	if val, ok := pendingNumericInput.LoadAndDelete(telegramUserID); ok {
		pending := val.(PendingInput)
		h.handleUserInput(ctx, msg, pending)
		return
	}

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
	case BtnRecommendations:
		h.handleRecommendations(ctx, msg)
	default:
		h.sendWithMainMenu(msg.Chat.ID, "Пожалуйста, выберите действие из меню.")
	}
}

func (h *Handler) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	callback := tgbotapi.NewCallback(cb.ID, "")
	h.bot.Send(callback)

	data := cb.Data
	chatID := cb.Message.Chat.ID
	telegramUserID := fmt.Sprintf("%d", cb.From.ID)

	switch {
	// User selects a criterion from the list.
	case strings.HasPrefix(data, "criterion_select_"):
		criterionID := strings.TrimPrefix(data, "criterion_select_")
		h.handleCriterionSelect(ctx, chatID, telegramUserID, criterionID)

	case data == "onboarding_next_1":
		h.sendOnboardingStep2(chatID)
	case data == "onboarding_next_2":
		h.sendOnboardingStep3(chatID)
	case data == "onboarding_done":
		h.sendMainMenu(chatID)
	case data == "back_main":
		h.sendMainMenu(chatID)
	case data == "back_criteria":
		h.handleAddData(ctx, &tgbotapi.Message{
			From: &tgbotapi.User{ID: cb.From.ID},
			Chat: &tgbotapi.Chat{ID: chatID},
		})
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
