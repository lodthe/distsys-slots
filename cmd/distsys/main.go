package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"distsys-slots/internal/app"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("command failed", "error", err.Error())
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	cfg, err := app.ConfigFromEnv()
	if err != nil {
		return err
	}
	db, err := app.OpenDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("cannot connect to PostgreSQL")
	}
	defer db.Close()
	if err = app.Migrate(ctx, db); err != nil {
		return fmt.Errorf("database migration failed: %w", err)
	}
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	s := app.NewServer(db, cfg)
	switch cmd {
	case "migrate":
		slog.Info("migrations applied")
		return nil
	case "configure-bot":
		return s.ConfigureBot(ctx)
	case "grant-assistant", "grant-admin":
		if len(os.Args) != 3 {
			return errors.New("usage: distsys grant-assistant|grant-admin TELEGRAM_ID_OR_USERNAME")
		}
		arg := os.Args[2]
		role := "assistant"
		if cmd == "grant-admin" {
			role = "admin"
		}
		var telegramID int64
		var username string
		if strings.HasPrefix(arg, "@") {
			username = arg
		} else {
			telegramID, err = strconv.ParseInt(arg, 10, 64)
			if err != nil || telegramID <= 0 {
				return errors.New("invalid Telegram ID")
			}
		}
		err = s.GrantRole(ctx, telegramID, username, role)
		if err == nil {
			slog.Info("role assignment saved", "role", role)
		}
		return err
	case "serve":
		if cfg.BotToken == "" || len(cfg.WebhookSecret) < 32 {
			return errors.New("TELEGRAM_BOT_TOKEN and TELEGRAM_WEBHOOK_SECRET must be configured")
		}
		server := &http.Server{Addr: cfg.Addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
		workerDone := make(chan struct{})
		go func() { defer close(workerDone); s.Worker(ctx) }()
		done := make(chan error, 1)
		go func() { slog.Info("server started", "addr", cfg.Addr); done <- server.ListenAndServe() }()
		select {
		case err = <-done:
			cancel()
		case <-ctx.Done():
		}
		shutdown, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		_ = server.Shutdown(shutdown)
		cancel()
		<-workerDone
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	default:
		return errors.New("unknown command")
	}
}
