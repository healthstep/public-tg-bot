package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"github.com/helthtech/public-tg-bot/internal/obs"
	"github.com/porebric/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var pendingAnalysisPick sync.Map

func progressReplyMarkup() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 Как получить", "progress_how"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("« Назад в меню", "back_main"),
		),
	)
}

func (h *Handler) clearAnalysisPick(telegramUserID string) {
	pendingAnalysisPick.Delete(telegramUserID)
}

func (h *Handler) handleProgressHowTo(ctx context.Context, chatID int64, telegramUserID string) {
	resp, err := h.healthClient.ListAnalyses(ctx, &healthpb.ListAnalysesRequest{})
	if err != nil {
		logger.Error(ctx, err, "list analyses for how-to")
		h.sendText(chatID, "Не удалось загрузить список анализов. Попробуйте позже.")
		return
	}
	list := resp.GetAnalyses()
	if len(list) == 0 {
		h.sendText(chatID, "В системе пока нет ни одного анализа. Обратитесь к администратору.")
		return
	}
	var b strings.Builder
	b.WriteString("<b>Введите число — id анализа</b>, по которому нужна инструкция (из списка ниже).\n\n")
	for _, a := range list {
		b.WriteString(fmt.Sprintf("%d — %s\n", a.GetId(), escapeHTML(a.GetName())))
	}
	pendingAnalysisPick.Store(telegramUserID, struct{}{})
	m := tgbotapi.NewMessage(chatID, b.String())
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("« Назад в меню", "back_main"),
		),
	)
	if _, err := h.bot.Send(m); err != nil {
		obs.BG("tg").Error(err, "send how-to analyses list", "chat_id", chatID)
	}
}

func (h *Handler) handleAnalysisPickReply(ctx context.Context, msg *tgbotapi.Message, telegramUserID string) bool {
	if _, ok := pendingAnalysisPick.Load(telegramUserID); !ok {
		return false
	}
	text := strings.TrimSpace(msg.Text)
	if strings.HasPrefix(text, "/") {
		return false
	}
	pendingAnalysisPick.Delete(telegramUserID)
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id <= 0 {
		h.sendWithMainMenu(msg.Chat.ID, "Неверный номер. Возвращаем вас в главное меню.")
		return true
	}
	ar, err := h.healthClient.GetAnalysis(ctx, &healthpb.GetAnalysisRequest{Id: id})
	if err != nil {
		st, _ := status.FromError(err)
		if st.Code() == codes.NotFound {
			h.sendWithMainMenu(msg.Chat.ID, "Такого id нет в списке. Возвращаем вас в главное меню.")
			return true
		}
		logger.Error(ctx, err, "get analysis by id", "id", id)
		h.sendWithMainMenu(msg.Chat.ID, "Ошибка загрузки. Возвращаем вас в главное меню.")
		return true
	}
	a := ar.GetAnalysis()
	instr := strings.TrimSpace(a.GetInstruction())
	var body string
	if instr == "" {
		body = fmt.Sprintf("<b>%s</b>\n\nИнструкция пока не заполнена.", escapeHTML(a.GetName()))
	} else {
		body = fmt.Sprintf("<b>%s</b>\n\n%s", escapeHTML(a.GetName()), escapeHTML(instr))
	}
	h.sendWithMainMenu(msg.Chat.ID, body)
	return true
}
