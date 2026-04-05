package boot

import (
	"context"
	"fmt"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	healthpb "github.com/helthtech/core-health/pkg/proto/health"
	userspb "github.com/helthtech/core-users/pkg/proto/users"
	"github.com/helthtech/public-tg-bot/internal/bot"
	"github.com/helthtech/public-tg-bot/internal/migration"
	"github.com/helthtech/public-tg-bot/internal/natshandler"
	"github.com/helthtech/public-tg-bot/internal/repository"
	"github.com/nats-io/nats.go"
	"github.com/porebric/configs"
	"github.com/porebric/logger"
	"github.com/porebric/resty"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/driver/postgres"
	gormlib "gorm.io/gorm"
)

func Run(ctx context.Context) error {
	db, err := gormlib.Open(postgres.Open(configs.Value(ctx, "db_dsn").String()), &gormlib.Config{})
	if err != nil {
		return fmt.Errorf("db open: %w", err)
	}
	if err := migration.Run(db); err != nil {
		return fmt.Errorf("migration: %w", err)
	}

	usersConn, err := grpc.NewClient(
		configs.Value(ctx, "grpc_core_users").String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("grpc core-users: %w", err)
	}

	healthConn, err := grpc.NewClient(
		configs.Value(ctx, "grpc_core_health").String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("grpc core-health: %w", err)
	}

	usersClient := userspb.NewUserServiceClient(usersConn)
	healthClient := healthpb.NewHealthServiceClient(healthConn)

	nc, err := nats.Connect(configs.Value(ctx, "nats_url").String())
	if err != nil {
		return fmt.Errorf("nats: %w", err)
	}

	botToken := configs.Value(ctx, "tg_bot_token").String()
	tgBot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return fmt.Errorf("telegram bot: %w", err)
	}
	log.Printf("authorized on telegram as %s", tgBot.Self.UserName)

	webhookHost := configs.Value(ctx, "tg_webhook_host").String()
	webhookURL := fmt.Sprintf("%s/tg/%s", webhookHost, botToken)
	wh, err := tgbotapi.NewWebhook(webhookURL)
	if err != nil {
		return fmt.Errorf("telegram webhook: %w", err)
	}
	if _, err := tgBot.Request(wh); err != nil {
		return fmt.Errorf("set webhook: %w", err)
	}
	log.Printf("webhook set to %s/tg/***", webhookHost)

	siteURL := configs.Value(ctx, "site_url").String()
	chatRepo := repository.NewChatRepository(db)

	handler := bot.NewHandler(tgBot, chatRepo, usersClient, healthClient, botToken, siteURL)

	notifHandler := natshandler.NewNotificationHandler(chatRepo, handler)
	if err := notifHandler.Subscribe(nc); err != nil {
		return fmt.Errorf("nats subscribe: %w", err)
	}

	l := logger.New(logger.InfoLevel)
	router := resty.NewRouter(func() *logger.Logger { return l }, nil)
	router.MuxRouter().HandleFunc("/tg/{token}", handler.WebhookHTTP).Methods("POST")

	log.Println("public-tg-bot starting")
	resty.RunServer(ctx, router, func(ctx context.Context) error {
		usersConn.Close()
		healthConn.Close()
		nc.Close()
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
		return nil
	})

	return nil
}
