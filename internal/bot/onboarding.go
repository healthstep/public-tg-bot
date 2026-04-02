package bot

import (
	"context"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (h *Handler) handleStartNoKey(ctx context.Context, msg *tgbotapi.Message) {
	_ = ctx
	h.sendOnboardingStep1(msg.Chat.ID)
}

func (h *Handler) sendOnboardingStep1(chatID int64) {
	text := "<b>Добро пожаловать в ЗдравоШаг!</b>\n\n" +
		"ЗдравоШаг — ваш персональный помощник по управлению здоровьем.\n\n" +
		"Мы поможем вам:\n" +
		"• Отслеживать показатели здоровья\n" +
		"• Не пропускать важные обследования\n" +
		"• Контролировать ваш прогресс"

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Далее →", "onboarding_next_1"),
		),
	)

	m := tgbotapi.NewMessage(chatID, text)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = kb
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send onboarding step 1: %v", err)
	}
}

func (h *Handler) sendOnboardingStep2(chatID int64) {
	text := "<b>Как это работает?</b>\n\n" +
		"1️⃣ <b>Добавляйте данные</b> — загружайте результаты анализов, " +
		"вводите показатели вручную или отмечайте визиты к врачу.\n\n" +
		"2️⃣ <b>Следите за прогрессом</b> — мы покажем ваш уровень здоровья " +
		"и напомним, что пора обновить данные.\n\n" +
		"3️⃣ <b>Получайте уведомления</b> — своевременные напоминания " +
		"о важных обследованиях."

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Далее →", "onboarding_next_2"),
		),
	)

	m := tgbotapi.NewMessage(chatID, text)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = kb
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send onboarding step 2: %v", err)
	}
}

func (h *Handler) sendOnboardingStep3(chatID int64) {
	text := "<b>Начнём!</b>\n\n" +
		"Для полноценной работы авторизуйтесь через приложение ЗдравоШаг.\n\n" +
		"Если у вас уже есть аккаунт — перейдите по ссылке авторизации " +
		"из приложения (она содержит /start с ключом).\n\n" +
		"А пока вы можете ознакомиться с меню бота."

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Открыть меню ✅", "onboarding_done"),
		),
	)

	m := tgbotapi.NewMessage(chatID, text)
	m.ParseMode = tgbotapi.ModeHTML
	m.ReplyMarkup = kb
	if _, err := h.bot.Send(m); err != nil {
		log.Printf("send onboarding step 3: %v", err)
	}
}
