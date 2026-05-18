package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/gorilla/mux"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	userspb "github.com/helthtech/core-users/pkg/proto/users"
	"github.com/helthtech/public-tg-bot/internal/obs"
	"github.com/helthtech/public-tg-bot/internal/repository"
	"github.com/porebric/logger"
)

var pendingNumericInput sync.Map

var criterionNames sync.Map

var criterionInputTypes sync.Map

var criterionGroups sync.Map

var pendingDateSelection sync.Map

type PendingInput struct {
	CriterionID   string
	CriterionName string
	InputType     string
}

type PendingDate struct {
	Kind            string
	CriterionID     string
	CriterionName   string
	Value           string
	PendingImportID string
	WaitingForText  bool
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
	ctx := obs.WithTrace(r.Context())
	if vars["token"] != h.botToken {
		logger.Warn(ctx, "tg webhook: forbidden token")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error(ctx, err, "tg webhook: read body")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var update tgbotapi.Update
	if err := json.Unmarshal(body, &update); err != nil {
		logger.Error(ctx, err, "tg webhook: unmarshal", "body_len", len(body))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	logger.Info(ctx, "tg webhook: update", "update_id", update.UpdateID, "body_len", len(body),
		"has_message", update.Message != nil, "has_callback", update.CallbackQuery != nil)

	h.handleUpdate(ctx, &update)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleUpdate(ctx context.Context, update *tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(ctx, fmt.Errorf("%v", r), "tg handleUpdate panic", "recovered", r)
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

	if text == BtnAddData || text == BtnProgress || text == BtnWeeklyRecs {
		h.clearLabUpload(telegramUserID)
		h.clearAnalysisPick(telegramUserID)
		pendingDateSelection.Delete(telegramUserID)
	}

	if strings.EqualFold(text, "отмена") || strings.EqualFold(text, "cancel") {
		pendingNumericInput.Delete(telegramUserID)
		pendingDateSelection.Delete(telegramUserID)
		h.clearLabUpload(telegramUserID)
		h.clearAnalysisPick(telegramUserID)
		h.handleCancelAll(ctx, msg)
		return
	}

	if val, ok := pendingDateSelection.Load(telegramUserID); ok {
		pd := val.(PendingDate)
		if pd.WaitingForText {
			pendingDateSelection.Delete(telegramUserID)
			h.handleDateTextInput(ctx, msg, pd)
			return
		}
	}

	if val, ok := pendingNumericInput.LoadAndDelete(telegramUserID); ok {
		pending := val.(PendingInput)
		h.handleUserInput(ctx, msg, pending)
		return
	}

	if h.handleAnyLabDocument(ctx, msg) {
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
	case strings.HasPrefix(text, "/password"):
		h.handlePasswordCommand(ctx, msg)
		return
	}

	if h.handleAnalysisPickReply(ctx, msg, telegramUserID) {
		return
	}

	switch text {
	case BtnAddData:
		h.handleAddData(ctx, msg)
	case BtnProgress:
		h.handleProgress(ctx, msg)
	case BtnWeeklyRecs:
		telegramUserIDStr := fmt.Sprintf("%d", msg.From.ID)
		h.handleWeeklyRecommendations(ctx, msg.Chat.ID, telegramUserIDStr)
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
	case strings.HasPrefix(data, "group_"):
		groupID := strings.TrimPrefix(data, "group_")
		h.handleGroupSelect(ctx, chatID, telegramUserID, groupID)

	case strings.HasPrefix(data, "criterion_select_"):
		criterionID := strings.TrimPrefix(data, "criterion_select_")
		h.handleCriterionSelect(ctx, chatID, telegramUserID, criterionID)

	case data == "show_weekly_recs":
		h.handleWeeklyRecommendations(ctx, cb.Message.Chat.ID, telegramUserID)

	case data == "progress_how":
		h.handleProgressHowTo(ctx, chatID, telegramUserID)

	case data == "onboarding_next_1":
		h.sendOnboardingStep2(chatID)
	case data == "onboarding_next_2":
		h.sendOnboardingStep3(chatID)
	case data == "onboarding_done":
		h.sendMainMenu(chatID)
	case data == "back_main":
		h.clearAnalysisPick(telegramUserID)
		h.sendMainMenu(chatID)
	case data == "back_criteria":
		h.handleAddData(ctx, &tgbotapi.Message{
			From: &tgbotapi.User{ID: cb.From.ID},
			Chat: &tgbotapi.Chat{ID: chatID},
		})
	case data == "lab_yes":
		h.handleLabYesShowDate(ctx, chatID, telegramUserID)
	case data == "lab_no":
		h.handleLabConfirm(ctx, chatID, telegramUserID, false, "")
	case data == "date_today" || data == "date_yesterday" || data == "date_skip" || data == "date_pick":
		h.handleDateCallback(ctx, chatID, telegramUserID, data)
	}
}

func (h *Handler) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	if _, err := h.bot.Send(msg); err != nil {
		obs.BG("tg").Error(err, "tg sendText", "chat_id", chatID)
	}
}

func (h *Handler) sendWithMainMenu(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = MainMenuKeyboard()
	if _, err := h.bot.Send(msg); err != nil {
		obs.BG("tg").Error(err, "tg sendText", "chat_id", chatID)
	}
}
