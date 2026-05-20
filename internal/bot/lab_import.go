package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	"github.com/helthtech/public-tg-bot/internal/obs"
	"github.com/porebric/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxLabFiles = 5
const tgLabDebounce = 2 * time.Second

var labConfirmPendingID sync.Map

var pendingLabImport sync.Map

var tgLabBatches sync.Map

type tgLabBatchState struct {
	mu     sync.Mutex
	files  []rawLabFile
	chatID int64
	timer  *time.Timer
}

type rawLabFile struct {
	Name string
	Data []byte
}

func (h *Handler) clearLabUpload(telegramUserID string) {
	labConfirmPendingID.Delete(telegramUserID)
	cancelTgLabBatch(telegramUserID)
}

func cancelTgLabBatch(telegramUserID string) {
	v, ok := tgLabBatches.Load(telegramUserID)
	if !ok {
		return
	}
	st := v.(*tgLabBatchState)
	st.mu.Lock()
	if st.timer != nil {
		st.timer.Stop()
	}
	st.mu.Unlock()
	tgLabBatches.Delete(telegramUserID)
}

func (h *Handler) handleAnyLabDocument(ctx context.Context, msg *tgbotapi.Message) bool {
	if msg == nil || msg.Document == nil {
		return false
	}
	telegramUserID := fmt.Sprintf("%d", msg.From.ID)

	chat, err := h.chatRepo.FindByTelegramUserID(ctx, telegramUserID)
	if err != nil || chat == nil || chat.UserID == nil {
		h.sendText(msg.Chat.ID, "Сначала зарегистрируйтесь через <b>/start</b>.")
		return true
	}
	_ = chat

	mime := strings.ToLower(msg.Document.MimeType)
	name := msg.Document.FileName
	if name == "" {
		name = "file.pdf"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") && mime != "application/pdf" && !strings.HasPrefix(mime, "application/x-pdf") {
		h.sendText(msg.Chat.ID, "Нужен файл в формате <b>PDF</b> (анализ).")
		return true
	}
	if _, busy := pendingLabImport.Load(telegramUserID); busy {
		h.sendText(msg.Chat.ID, "Дождитесь окончания предыдущей обработки.")
		return true
	}

	data, err := h.downloadTelegramFile(ctx, msg.Document.FileID)
	if err != nil {
		logger.Error(ctx, err, "tg download lab pdf")
		h.sendText(msg.Chat.ID, "Не удалось скачать файл. Попробуйте ещё раз.")
		return true
	}
	f := rawLabFile{Name: name, Data: data}

	v, _ := tgLabBatches.LoadOrStore(telegramUserID, &tgLabBatchState{})
	st := v.(*tgLabBatchState)
	st.mu.Lock()
	if st.chatID == 0 {
		st.chatID = msg.Chat.ID
	}
	if len(st.files) >= maxLabFiles {
		st.mu.Unlock()
		h.sendText(msg.Chat.ID, fmt.Sprintf("Максимум <b>%d</b> PDF за раз. Сейчас идёт накопление — дождитесь обработки или обработайте пачку, затем отправьте снова.", maxLabFiles))
		return true
	}
	wasEmpty := len(st.files) == 0
	st.files = append(st.files, f)
	if st.timer != nil {
		st.timer.Stop()
	}
	uid := telegramUserID
	chatID := st.chatID
	if wasEmpty {
		h.sendText(msg.Chat.ID, "📄 <b>PDF получен.</b> Можно за "+fmt.Sprintf("%d", int(tgLabDebounce.Seconds()))+" с прислать ещё (до 5 вместе) — тогда обработаем одной пачкой; иначе обработаем сразу по истечении таймера.")
	}
	st.timer = time.AfterFunc(tgLabDebounce, func() {
		h.flushTgLabBatch(context.Background(), uid, chatID)
	})
	st.mu.Unlock()
	return true
}

func (h *Handler) flushTgLabBatch(ctx context.Context, telegramUserID string, chatID int64) {
	v, ok := tgLabBatches.Load(telegramUserID)
	if !ok {
		return
	}
	st := v.(*tgLabBatchState)
	st.mu.Lock()
	if len(st.files) == 0 {
		st.mu.Unlock()
		return
	}
	files := make([]rawLabFile, len(st.files))
	copy(files, st.files)
	st.files = nil
	if st.timer != nil {
		st.timer.Stop()
		st.timer = nil
	}
	st.mu.Unlock()
	tgLabBatches.Delete(telegramUserID)

	if !tryStartLabImport(telegramUserID) {
		h.sendText(chatID, "Подождите, идёт обработка предыдущей загрузки…")
		return
	}
	h.sendText(chatID, "Подождите, идёт обработка файла…")
	go h.runTelegramLabImport(ctx, chatID, telegramUserID, files)
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
	m := tgbotapi.NewMessage(chatID, text)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = kb
	if _, err := h.bot.Send(m); err != nil {
		obs.BG("tg").Error(err, "send lab result", "chat_id", chatID)
	}
}


func (h *Handler) handleLabConfirm(ctx context.Context, chatID int64, telegramUserID string, accept bool, measuredAt string) {
	pidVal, ok := labConfirmPendingID.Load(telegramUserID)
	if !ok {
		h.sendText(chatID, "Нет данных для подтверждения. Сначала пришлите PDF с анализом.")
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
		labConfirmPendingID.Delete(telegramUserID)
		h.sendText(chatID, "Пользователь не найден.")
		return
	}
	userID := chat.UserID.String()
	userSex := h.getUserSex(ctx, telegramUserID)
	out, err := h.healthClient.ConfirmPendingImport(ctx, &healthpb.ConfirmPendingImportRequest{
		UserId:     userID,
		PendingId:  pendingID,
		Accept:     accept,
		UserSex:    userSex,
		MeasuredAt: measuredAt,
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
