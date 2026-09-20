package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type telegramResult struct {
	OK         bool            `json:"ok"`
	Result     json.RawMessage `json:"result"`
	Code       int             `json:"error_code"`
	Parameters struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (s *Server) telegram(ctx context.Context, method string, payload any) (telegramResult, error) {
	var result telegramResult
	body, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.telegram.org/bot"+s.cfg.BotToken+"/"+method, bytes.NewReader(body))
	if err != nil {
		return result, errors.New("telegram request setup failed")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return result, errors.New("telegram unavailable")
	}
	defer res.Body.Close()
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&result); err != nil {
		return result, errors.New("invalid telegram response")
	}
	if !result.OK {
		return result, fmt.Errorf("telegram returned code %d", result.Code)
	}
	return result, nil
}
func (s *Server) ConfigureBot(ctx context.Context) error {
	if s.cfg.BotToken == "" || len(s.cfg.WebhookSecret) < 32 {
		return errors.New("bot token and webhook secret are required")
	}
	result, err := s.telegram(ctx, "getMe", map[string]any{})
	if err != nil {
		return err
	}
	var bot struct {
		Username string `json:"username"`
	}
	if err = json.Unmarshal(result.Result, &bot); err != nil {
		return err
	}
	current, err := s.telegram(ctx, "getWebhookInfo", map[string]any{})
	if err != nil {
		return err
	}
	var hook struct {
		URL string `json:"url"`
	}
	if err = json.Unmarshal(current.Result, &hook); err != nil {
		return err
	}
	desired := s.cfg.BaseURL + "/telegram/webhook"
	if hook.URL != "" && hook.URL != desired {
		return errors.New("bot already has a webhook for another URL; inspect before changing")
	}
	if _, err = s.telegram(ctx, "setWebhook", map[string]any{"url": desired, "secret_token": s.cfg.WebhookSecret, "allowed_updates": []string{"message"}, "drop_pending_updates": false}); err != nil {
		return err
	}
	if _, err = s.telegram(ctx, "setChatMenuButton", map[string]any{"menu_button": map[string]any{"type": "web_app", "text": "Защиты", "web_app": map[string]string{"url": s.cfg.BaseURL}}}); err != nil {
		return err
	}
	if _, err = s.db.Exec(ctx, `INSERT INTO app_settings(key,value) VALUES('bot_username',$1) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, bot.Username); err != nil {
		return err
	}
	slog.Info("bot configured", "username", bot.Username)
	return nil
}
func (s *Server) botUsername() string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var name string
	_ = s.db.QueryRow(ctx, `SELECT value FROM app_settings WHERE key='bot_username'`).Scan(&name)
	return name
}
func (s *Server) enqueue(ctx context.Context, tx pgx.Tx, event string, telegram int64, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO notification_outbox(event_key,telegram_id,payload) VALUES($1,$2,$3) ON CONFLICT(event_key,telegram_id) DO NOTHING`, event, telegram, raw)
	return err
}
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) error {
	if s.cfg.WebhookSecret == "" || !hmac.Equal([]byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token")), []byte(s.cfg.WebhookSecret)) {
		return &problem{403, "forbidden", "Доступ запрещён"}
	}
	var update struct {
		ID      int64 `json:"update_id"`
		Message *struct {
			Text string `json:"text"`
			Chat struct {
				ID   int64  `json:"id"`
				Type string `json:"type"`
			} `json:"chat"`
		} `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		return bad("Некорректное обновление")
	}
	err := transaction(r.Context(), s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `INSERT INTO telegram_updates(update_id) VALUES($1) ON CONFLICT DO NOTHING`, update.ID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		if update.Message == nil || update.Message.Chat.Type != "private" {
			return nil
		}
		text := update.Message.Text
		if text != "/start" && !strings.HasPrefix(text, "/start ") {
			return nil
		}
		chat := update.Message.Chat.ID
		return s.enqueue(r.Context(), tx, fmt.Sprintf("start:%d", update.ID), chat, map[string]any{"chat_id": chat, "text": "Защиты по распределённым системам\n\nОткройте расписание, выберите домашнее задание и удобное время. Ассистенты могут публиковать свои окна приёма.", "reply_markup": map[string]any{"inline_keyboard": []any{[]any{map[string]any{"text": "Открыть расписание", "web_app": map[string]string{"url": s.cfg.BaseURL}}}}}})
	})
	if err != nil {
		return err
	}
	return respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) Worker(ctx context.Context) {
	var nextReminder time.Time
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-cleanup.C:
			_, _ = s.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at<now()`)
		case <-ticker.C:
			now := time.Now()
			if !now.Before(nextReminder) {
				if err := s.enqueueReminders(ctx, now); err != nil {
					slog.Warn("reminder scheduler error", "error_type", errorClass(err))
				}
				nextReminder = now.Add(30 * time.Second)
			}
			for i := 0; i < 20; i++ {
				found, err := s.deliverOne(ctx)
				if err != nil {
					slog.Warn("notification worker error", "error_type", errorClass(err))
					break
				}
				if !found {
					break
				}
			}
		}
	}
}
func (s *Server) deliverOne(ctx context.Context) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var id int64
	var payload []byte
	var attempts int
	var active bool
	var comment string
	err = tx.QueryRow(ctx, `SELECT n.id,n.payload,n.attempts,
 (n.reminder_booking_id IS NULL OR EXISTS(
 SELECT 1 FROM bookings b JOIN slots sl ON sl.id=b.slot_id JOIN availability_windows w ON w.id=sl.window_id
 WHERE b.id=n.reminder_booking_id AND b.status='confirmed' AND sl.status='open' AND w.status='published' AND sl.starts_at>clock_timestamp()))
 AND (n.reminder_window_id IS NULL OR EXISTS(SELECT 1 FROM availability_windows w
 WHERE w.id=n.reminder_window_id AND w.status='published' AND w.starts_at>clock_timestamp())),
 COALESCE((SELECT w.comment FROM bookings b JOIN slots sl ON sl.id=b.slot_id
 JOIN availability_windows w ON w.id=sl.window_id WHERE b.id=n.reminder_booking_id),'')
 FROM notification_outbox n WHERE n.status='pending' AND n.next_attempt_at<=now() AND NOT EXISTS(SELECT 1 FROM notification_outbox earlier WHERE earlier.telegram_id=n.telegram_id AND earlier.status='pending' AND earlier.id<n.id) ORDER BY n.id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &payload, &attempts, &active, &comment)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !active {
		if _, err = tx.Exec(ctx, `UPDATE notification_outbox SET status='failed',last_error='reminder_obsolete' WHERE id=$1`, id); err != nil {
			return true, err
		}
		return true, tx.Commit(ctx)
	}
	// Append the current comment only at delivery, including retries after edits.
	if comment != "" {
		var message map[string]any
		if err = json.Unmarshal(payload, &message); err != nil {
			return true, err
		}
		text, ok := message["text"].(string)
		if !ok {
			return true, errors.New("notification text missing")
		}
		message["text"] = text + "\n\nКомментарий ассистента:\n" + comment
		if payload, err = json.Marshal(message); err != nil {
			return true, err
		}
	}
	result, sendErr := s.telegram(ctx, "sendMessage", json.RawMessage(payload))
	attempts++
	if sendErr == nil {
		_, err = tx.Exec(ctx, `UPDATE notification_outbox SET status='sent',attempts=$2,sent_at=now(),last_error='' WHERE id=$1`, id, attempts)
	} else {
		status := "pending"
		if result.Code == 400 || result.Code == 403 || attempts >= 12 {
			status = "failed"
		}
		delay := min(3600, 1<<min(attempts, 12))
		if result.Parameters.RetryAfter > delay {
			delay = result.Parameters.RetryAfter
		}
		_, err = tx.Exec(ctx, `UPDATE notification_outbox SET status=$2,attempts=$3,next_attempt_at=now()+($4 * interval '1 second'),last_error=$5 WHERE id=$1`, id, status, attempts, delay, fmt.Sprintf("telegram_code_%d", result.Code))
		slog.Warn("notification delivery deferred", "notification_id", id, "code", result.Code, "status", status)
	}
	if err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}
