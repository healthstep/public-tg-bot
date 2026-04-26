package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"github.com/helthtech/public-tg-bot/internal/obs"
	"github.com/porebric/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxLabFiles = 5

// pendingLabCollect: telegram user id -> *labCollectState
var pendingLabCollect sync.Map

// pendingLabImport blocks concurrent imports per user.
var pendingLabImport sync.Map

// labConfirmPendingID: telegram user id -> pending import id (short-lived, for inline buttons)
var labConfirmPendingID sync.Map

type labCollectState struct {
	ChatID int64
	Files  []rawLabFile
}

type rawLabFile struct {
	Name string
	Data []byte
}

func (h *Handler) clearLabUpload(telegramUserID string) {
	pendingLabCollect.Delete(telegramUserID)
	labConfirmPendingID.Delete(telegramUserID)
}

func (h *Handler) startLabUploadFromMenu(ctx context.Context, chatID int64, telegramUserID string) {
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(chatID, "Сначала зарегистрируйтесь через <b>/start</b>.")
		return
	}
	h.clearLabUpload(telegramUserID)
	pendingLabCollect.Store(telegramUserID, &labCollectState{ChatID: chatID, Files: nil})
	text := "📄 <b>Загрузка анализов (PDF)</b>\n\n" +
		"Отправьте в чат до " + fmt.Sprintf("%d", maxLabFiles) + " PDF-файлов (по одному в сообщении или по очереди).\n\n" +
		"Когда закончите — нажмите <b>«Готово»</b> или напишите «готово»."
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Готово", "lab_done"),
			tgbotapi.NewInlineKeyboardButtonData("◀️ Отмена", "lab_cancel"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = kb
	if _, err := h.bot.Send(msg); err != nil {
		obs.BG("tg").Error(err, "startLabUpload", "chat_id", chatID)
	}
}

func (h *Handler) handleLabDocumentMessage(ctx context.Context, msg *tgbotapi.Message) bool {
	telegramUserID := fmt.Sprintf("%d", msg.From.ID)
	if msg.Document == nil {
		return false
	}
	v, ok := pendingLabCollect.Load(telegramUserID)
	if !ok {
		return false
	}
	st := v.(*labCollectState)
	if st.ChatID != msg.Chat.ID {
		return false
	}
	if len(st.Files) >= maxLabFiles {
		h.sendText(msg.Chat.ID, fmt.Sprintf("Уже получено максимум %d файлов. Нажмите «Готово» или «Отмена».", maxLabFiles))
		return true
	}
	mime := strings.ToLower(msg.Document.MimeType)
	name := msg.Document.FileName
	if name == "" {
		name = "file.pdf"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") && mime != "application/pdf" && !strings.HasPrefix(mime, "application/x-pdf") {
		h.sendText(msg.Chat.ID, "Нужен файл в формате PDF. Отправьте PDF или нажмите «Отмена».")
		return true
	}
	if _, busy := pendingLabImport.Load(telegramUserID); busy {
		h.sendText(msg.Chat.ID, "Дождитесь окончания обработки предыдущей загрузки.")
		return true
	}
	data, err := h.downloadTelegramFile(ctx, msg.Document.FileID)
	if err != nil {
		logger.Error(ctx, err, "tg download lab pdf")
		h.sendText(msg.Chat.ID, "Не удалось скачать файл. Попробуйте ещё раз.")
		return true
	}
	st.Files = append(st.Files, rawLabFile{Name: name, Data: data})
	pendingLabCollect.Store(telegramUserID, st)
	n := len(st.Files)
	h.sendText(msg.Chat.ID, fmt.Sprintf("Принято: <b>%s</b> (%d/%d)", tgbotapi.EscapeText(tgbotapi.ModeHTML, name), n, maxLabFiles))
	return true
}

func (h *Handler) downloadTelegramFile(ctx context.Context, fileID string) ([]byte, error) {
	f, err := h.bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.Link(h.botToken), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// handleLabUploadDone run import (also used when user typed «готово»).
func (h *Handler) handleLabUploadDone(ctx context.Context, chatID int64, telegramUserID string) {
	v, ok := pendingLabCollect.Load(telegramUserID)
	if !ok {
		return
	}
	st := v.(*labCollectState)
	if len(st.Files) == 0 {
		h.sendText(chatID, "Вы ещё не отправили ни одного PDF. Пришлите файл и нажмите «Готово».")
		return
	}
	if !tryStartLabImport(telegramUserID) {
		h.sendText(chatID, "Подождите, идёт обработка предыдущей загрузки…")
		return
	}
	files := st.Files
	pendingLabCollect.Delete(telegramUserID)
	h.sendText(chatID, "Подождите, идёт обработка файла…")
	go h.runTelegramLabImport(ctx, chatID, telegramUserID, files)
}

func tryStartLabImport(telegramUserID string) bool {
	if _, loaded := pendingLabImport.LoadOrStore(telegramUserID, struct{}{}); loaded {
		return false
	}
	return true
}

func doneLabImport(telegramUserID string) { pendingLabImport.Delete(telegramUserID) }

func (h *Handler) runTelegramLabImport(ctx context.Context, chatID int64, telegramUserID string, files []rawLabFile) {
	defer doneLabImport(telegramUserID)
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(chatID, "Ошибка: пользователь не найден.")
		return
	}
	userID := chat.UserID.String()
	userSex := h.getUserSex(ctx, telegramUserID)

	resp, err := h.callImportCriteriaFromPdf(ctx, userID, userSex, files)
	if err != nil {
		logger.Error(ctx, err, "import lab pdfs")
		if st, ok := status.FromError(err); ok && st.Code() == codes.Unavailable {
			h.sendText(chatID, "Сервис разбора анализов временно недоступен. Попробуйте позже.")
			return
		}
		h.sendText(chatID, "Не удалось обработать файлы. Попробуйте ещё раз.")
		return
	}
	pendingID := resp.GetPendingImportId()
	if pendingID == "" {
		rows := resp.GetUserCriteria()
		if len(rows) == 0 {
			h.sendText(chatID, "Из PDF не удалось извлечь показатели.")
			return
		}
	}
	text := formatLabExtractionText(resp)
	labConfirmPendingID.Store(telegramUserID, pendingID)
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Сохранить в показатели", "lab_yes"),
			tgbotapi.NewInlineKeyboardButtonData("✖️ Отклонить", "lab_no"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = kb
	if _, err := h.bot.Send(msg); err != nil {
		obs.BG("tg").Error(err, "send lab result", "chat_id", chatID)
	}
}

func (h *Handler) handleLabConfirm(ctx context.Context, chatID int64, telegramUserID string, accept bool) {
	pidVal, ok := labConfirmPendingID.Load(telegramUserID)
	if !ok {
		h.sendText(chatID, "Нет данных для подтверждения. Сначала загрузите анализ.")
		return
	}
	pendingID := pidVal.(string)
	if pendingID == "" {
		labConfirmPendingID.Delete(telegramUserID)
		h.sendText(chatID, "Нечего применять.")
		return
	}
	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(chatID, "Пользователь не найден.")
		return
	}
	userID := chat.UserID.String()
	userSex := h.getUserSex(ctx, telegramUserID)
	out, err := h.healthClient.ConfirmPendingImport(ctx, &healthpb.ConfirmPendingImportRequest{
		UserId:    userID,
		PendingId: pendingID,
		Accept:    accept,
		UserSex:   userSex,
	})
	labConfirmPendingID.Delete(telegramUserID)
	if err != nil {
		logger.Error(ctx, err, "confirm lab import")
		h.sendWithMainMenu(chatID, "Не удалось применить: попробуйте снова в личном кабинете на сайте.")
		return
	}
	if !out.GetSuccess() {
		msg := out.GetErrorMessage()
		if msg == "" {
			msg = "ошибка"
		}
		h.sendWithMainMenu(chatID, "Ошибка: "+tgbotapi.EscapeText(tgbotapi.ModeHTML, msg))
		return
	}
	if accept {
		n := out.GetApplied()
		h.sendWithMainMenu(chatID, fmt.Sprintf("Готово: в профиль записано обновлений: <b>%d</b>.", n))
	} else {
		h.sendWithMainMenu(chatID, "Черновик отклонён, показатели на сайте не менялись.")
	}
}

func (h *Handler) callImportCriteriaFromPdf(ctx context.Context, userID, userSex string, files []rawLabFile) (*healthpb.ImportCriteriaFromPdfResponse, error) {
	stream, err := h.healthClient.ImportCriteriaFromPdf(ctx)
	if err != nil {
		return nil, err
	}
	if err := stream.Send(&healthpb.ImportCriteriaFromPdfRequest{UserId: userID, UserSex: userSex}); err != nil {
		_ = stream.CloseSend()
		return nil, err
	}
	const chunkSize = 64 * 1024
	for _, f := range files {
		if err := stream.Send(&healthpb.ImportCriteriaFromPdfRequest{Filename: f.Name}); err != nil {
			_ = stream.CloseSend()
			return nil, err
		}
		buf := make([]byte, chunkSize)
		r := f.Data
		for i := 0; i < len(r); {
			n := copy(buf, r[i:])
			if err := stream.Send(&healthpb.ImportCriteriaFromPdfRequest{Chunk: buf[:n]}); err != nil {
				_ = stream.CloseSend()
				return nil, err
			}
			i += n
		}
	}
	return stream.CloseAndRecv()
}

func formatLabExtractionText(resp *healthpb.ImportCriteriaFromPdfResponse) string {
	var b strings.Builder
	b.WriteString("📄 <b>Результат по файлу</b>\n\n")
	if note := strings.TrimSpace(resp.GetModelNote()); note != "" {
		b.WriteString("ℹ️ ")
		b.WriteString(tgbotapi.EscapeText(tgbotapi.ModeHTML, note))
		b.WriteString("\n\n")
	}
	rows := resp.GetUserCriteria()
	if len(rows) == 0 {
		b.WriteString("Показатели не распознаны.")
		return b.String()
	}
	b.WriteString("Найдено:\n")
	for _, r := range rows {
		name := tgbotapi.EscapeText(tgbotapi.ModeHTML, r.GetCriterionName())
		val := tgbotapi.EscapeText(tgbotapi.ModeHTML, r.GetValue())
		b.WriteString("• <b>" + name + ":</b> " + val + "\n")
	}
	b.WriteString("\nСохранить в ваши показатели?")
	return b.String()
}
