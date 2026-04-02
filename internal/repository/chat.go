package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/helthtech/public-tg-bot/internal/model"
	"gorm.io/gorm"
)

type ChatRepository struct {
	db *gorm.DB
}

func NewChatRepository(db *gorm.DB) *ChatRepository {
	return &ChatRepository{db: db}
}

func (r *ChatRepository) Upsert(ctx context.Context, chat *model.Chat) error {
	return r.db.WithContext(ctx).
		Where("telegram_user_id = ?", chat.TelegramUserID).
		Assign(model.Chat{
			ChatID:            chat.ChatID,
			Username:          chat.Username,
			ProvisionalUserID: chat.ProvisionalUserID,
			UserID:            chat.UserID,
		}).
		FirstOrCreate(chat).Error
}

func (r *ChatRepository) FindByTelegramUserID(ctx context.Context, telegramUserID string) (*model.Chat, error) {
	var chat model.Chat
	err := r.db.WithContext(ctx).Where("telegram_user_id = ?", telegramUserID).First(&chat).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &chat, err
}

func (r *ChatRepository) FindByUserID(ctx context.Context, userID uuid.UUID) (*model.Chat, error) {
	var chat model.Chat
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&chat).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &chat, err
}

func (r *ChatRepository) UpdateUserID(ctx context.Context, telegramUserID string, userID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Model(&model.Chat{}).
		Where("telegram_user_id = ?", telegramUserID).
		Updates(map[string]any{
			"user_id":             userID,
			"provisional_user_id": nil,
		}).Error
}
