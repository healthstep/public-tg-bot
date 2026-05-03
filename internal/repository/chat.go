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
	existing := &model.Chat{}
	err := r.db.WithContext(ctx).Where("telegram_user_id = ?", chat.TelegramUserID).First(existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.WithContext(ctx).Create(chat).Error
	}
	if err != nil {
		return err
	}

	return r.db.WithContext(ctx).
		Model(existing).
		Updates(map[string]any{
			"chat_id":             chat.ChatID,
			"username":            chat.Username,
			"provisional_user_id": chat.ProvisionalUserID,
			"user_id":             nil,
		}).Error
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
