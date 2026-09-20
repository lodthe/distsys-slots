package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Select overdue reminders too, so restarts and late bookings do not lose them.
// The event key makes repeated or concurrent scheduler runs idempotent.
func (s *Server) enqueueReminders(ctx context.Context, now time.Time) error {
	return transaction(ctx, s.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH due AS (
 SELECT b.id AS booking, NULL::bigint AS window_id, 'student' AS kind, student.telegram_id,
 sl.starts_at, h.number, COALESCE(NULLIF('@'||assistant.username,'@'),'Ассистент') AS assistant
 FROM bookings b
 JOIN slots sl ON sl.id=b.slot_id
 JOIN availability_windows w ON w.id=sl.window_id
 JOIN homeworks h ON h.id=w.homework_id
 JOIN users student ON student.id=b.student_id
 JOIN users assistant ON assistant.id=w.assistant_id
 WHERE b.status='confirmed' AND sl.status='open' AND w.status='published'
 AND sl.starts_at>$1 AND sl.starts_at<=$1+interval '5 minutes'
 UNION ALL
 SELECT NULL::bigint, w.id, 'window', assistant.telegram_id, w.starts_at, h.number, ''
 FROM availability_windows w
 JOIN homeworks h ON h.id=w.homework_id
 JOIN users assistant ON assistant.id=w.assistant_id
 WHERE w.status='published' AND w.starts_at>$1 AND w.starts_at<=$1+interval '30 minutes'
 )
 SELECT booking, window_id, kind, telegram_id, starts_at, number, assistant FROM due d
 WHERE NOT EXISTS (SELECT 1 FROM notification_outbox n
 WHERE n.event_key='reminder:'||d.kind||':'||COALESCE(d.booking,d.window_id) AND n.telegram_id=d.telegram_id)
 ORDER BY starts_at,kind,COALESCE(booking,window_id)`, now)
		if err != nil {
			return err
		}
		type reminder struct {
			booking, window *int64
			kind            string
			telegram        int64
			at              time.Time
			homework        int
			assistant       string
		}
		items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (reminder, error) {
			var item reminder
			err := row.Scan(&item.booking, &item.window, &item.kind, &item.telegram, &item.at, &item.homework, &item.assistant)
			return item, err
		})
		if err != nil {
			return err
		}
		loc, _ := time.LoadLocation("Europe/Moscow")
		for _, item := range items {
			heading := "Скоро ваша защита"
			if item.kind == "window" {
				heading = "Напоминание об окне защиты"
			}
			body := fmt.Sprintf("%s\nДЗ №%d · %s МСК", heading, item.homework, item.at.In(loc).Format("02.01.2006 15:04"))
			var event string
			if item.booking != nil {
				body += "\nАссистент: " + item.assistant
				event = fmt.Sprintf("reminder:student:%d", *item.booking)
			} else {
				event = fmt.Sprintf("reminder:window:%d", *item.window)
			}
			payload, err := json.Marshal(s.scheduleMessage(item.telegram, body))
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO notification_outbox(event_key,telegram_id,payload,reminder_booking_id,reminder_window_id)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT(event_key,telegram_id) DO NOTHING`, event, item.telegram, payload, item.booking, item.window); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Server) scheduleMessage(telegram int64, text string) map[string]any {
	return map[string]any{"chat_id": telegram, "text": text, "reply_markup": map[string]any{"inline_keyboard": []any{[]any{map[string]any{"text": "Открыть расписание", "web_app": map[string]string{"url": s.cfg.BaseURL}}}}}}
}
