package migration

import (
	"github.com/helthtech/public-tg-bot/internal/model"
	"gorm.io/gorm"
)

func Run(db *gorm.DB) error {
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS tg_bot").Error; err != nil {
		return err
	}

	if err := db.AutoMigrate(&model.Chat{}); err != nil {
		return err
	}

	db.Exec(`
		DO $$ BEGIN
			ALTER TABLE tg_bot.chats
				ADD CONSTRAINT chk_user_xor_provisional
				CHECK (
					(provisional_user_id IS NOT NULL AND user_id IS NULL) OR
					(provisional_user_id IS NULL AND user_id IS NOT NULL)
				);
		EXCEPTION WHEN duplicate_object THEN NULL;
		END $$
	`)

	return nil
}
