package model

import (
	"time"

	"github.com/google/uuid"
)

type Chat struct {
	ID                uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ProvisionalUserID *uuid.UUID `gorm:"type:uuid"`
	UserID            *uuid.UUID `gorm:"type:uuid"`
	TelegramUserID    string     `gorm:"type:text;uniqueIndex"`
	ChatID            string     `gorm:"type:text"`
	Username          *string    `gorm:"type:text"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (Chat) TableName() string {
	return "tg_bot.chats"
}
