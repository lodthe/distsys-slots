package app

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

type Config struct{ Addr, DatabaseURL, BaseURL, BotToken, WebhookSecret, AdminUsername string }

func ConfigFromEnv() (Config, error) {
	c := Config{Addr: os.Getenv("HTTP_ADDR"), DatabaseURL: os.Getenv("DATABASE_URL"), BaseURL: strings.TrimRight(os.Getenv("APP_BASE_URL"), "/"), BotToken: os.Getenv("TELEGRAM_BOT_TOKEN"), WebhookSecret: os.Getenv("TELEGRAM_WEBHOOK_SECRET")}
	c.AdminUsername = strings.TrimPrefix(strings.TrimSpace(os.Getenv("ADMIN_TELEGRAM_USERNAME")), "@")
	if c.Addr == "" {
		c.Addr = "127.0.0.1:8083"
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if c.BaseURL == "" {
		return c, errors.New("APP_BASE_URL is required")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return c, errors.New("APP_BASE_URL must be an HTTPS origin")
	}
	return c, nil
}
