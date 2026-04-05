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

// pendingNumericInput stores users waiting to type a numeric criterion value.
// key: telegramUserID (string), value: PendingInput
var pendingNumericInput sync.Map

// pendingAnalysis tracks the last selected analysis per user (for cancel command).
// key: telegramUserID (string), value: analysisID (string)
var pendingAnalysis sync.Map

type PendingInput struct {
	CriterionID   string
	CriterionName string
	AnalysisID    string
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

	// "cancel" resets all criteria for the pending analysis.
	if strings.EqualFold(text, "отмена") || strings.EqualFold(text, "cancel") {
		pendingNumericInput.Delete(telegramUserID)
		if aID, ok := pendingAnalysis.LoadAndDelete(telegramUserID); ok {
			h.handleCancelAnalysis(ctx, msg, aID.(string))
		} else {
			h.sendWithMainMenu(msg.Chat.ID, "Нечего отменять.")
		}
		return
	}

	// Check if user is waiting to type a numeric value.
	if val, ok := pendingNumericInput.LoadAndDelete(telegramUserID); ok {
		pending := val.(PendingInput)
		h.handleNumericInput(ctx, msg, pending)
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
	// Step 1: user clicked "Add Data" → show analysis list
	// (handled as a button, not callback; the callback is for analysis selection)

	// Step 2: user selects an analysis → show criteria + input options
	case strings.HasPrefix(data, "analysis_"):
		analysisID := strings.TrimPrefix(data, "analysis_")
		pendingAnalysis.Store(telegramUserID, analysisID)
		h.showCriteriaForAnalysis(ctx, chatID, analysisID)

	// Step 3a: user chose "enter manually" for a criterion
	case strings.HasPrefix(data, "criterion_manual_"):
		// format: criterion_manual_<criterionID>:<criterionName>:<analysisID>
		parts := strings.SplitN(strings.TrimPrefix(data, "criterion_manual_"), ":", 3)
		if len(parts) >= 2 {
			analysisID := ""
			if len(parts) == 3 {
				analysisID = parts[2]
			}
			pendingNumericInput.Store(telegramUserID, PendingInput{
				CriterionID:   parts[0],
				CriterionName: parts[1],
				AnalysisID:    analysisID,
			})
			h.sendText(chatID, fmt.Sprintf(
				"Введите числовое значение для <b>%s</b>:\n\n<i>Отправьте «отмена» чтобы сбросить все данные этого анализа.</i>",
				parts[1],
			))
		}

	// Step 3b: user chose "mark done" for a criterion
	// format: criterion_done_<criterionID>:<analysisID>
	case strings.HasPrefix(data, "criterion_done_"):
		parts := strings.SplitN(strings.TrimPrefix(data, "criterion_done_"), ":", 2)
		criterionID := parts[0]
		if len(parts) == 2 {
			pendingAnalysis.Store(telegramUserID, parts[1])
		}
		h.handleMarkDone(ctx, chatID, cb.From.ID, criterionID)

	// Step 3c: upload file instruction
	// format: criterion_upload_<criterionID>:<analysisID>
	case strings.HasPrefix(data, "criterion_upload_"):
		parts := strings.SplitN(strings.TrimPrefix(data, "criterion_upload_"), ":", 2)
		criterionID := parts[0]
		if len(parts) == 2 {
			pendingAnalysis.Store(telegramUserID, parts[1])
		}
		h.sendText(chatID, fmt.Sprintf(
			"Загрузите файл (PDF, фото анализов) в этот чат.\n\n(ID критерия: <code>%s</code>)\n\nОтправьте «отмена» чтобы сбросить все данные этого анализа.",
			criterionID,
		))

	case data == "onboarding_next_1":
		h.sendOnboardingStep2(chatID)
	case data == "onboarding_next_2":
		h.sendOnboardingStep3(chatID)
	case data == "onboarding_done":
		h.sendMainMenu(chatID)
	case data == "back_main":
		h.sendMainMenu(chatID)
	case data == "back_analysis":
		h.handleAddData(ctx, &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}})
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
