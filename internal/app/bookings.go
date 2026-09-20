package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type lockedSlot struct {
	HomeworkID           int64
	StartsAt, EndsAt     time.Time
	Status, WindowStatus string
}

func lockSlot(ctx context.Context, tx pgx.Tx, id int64) (lockedSlot, error) {
	var s lockedSlot
	var windowID int64
	err := tx.QueryRow(ctx, `SELECT window_id FROM slots WHERE id=$1`, id).Scan(&windowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, missing()
	}
	if err != nil {
		return s, err
	}
	err = tx.QueryRow(ctx, `SELECT homework_id,status FROM availability_windows WHERE id=$1 FOR UPDATE`, windowID).Scan(&s.HomeworkID, &s.WindowStatus)
	if err != nil {
		return s, err
	}
	err = tx.QueryRow(ctx, `SELECT starts_at,ends_at,status FROM slots WHERE id=$1 FOR UPDATE`, id).Scan(&s.StartsAt, &s.EndsAt, &s.Status)
	return s, err
}
func dbNow(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var t time.Time
	err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&t)
	return t, err
}
func (s *Server) book(w http.ResponseWriter, r *http.Request) error {
	id, err := resourceID(r)
	if err != nil {
		return err
	}
	booking, err := s.bookSlot(r.Context(), current(r).ID, id)
	if err != nil {
		return err
	}
	return respond(w, 200, map[string]int64{"booking_id": booking})
}
func (s *Server) bookSlot(ctx context.Context, student, slot int64) (int64, error) {
	var booking int64
	err := transaction(ctx, s.db, func(tx pgx.Tx) error {
		var fullName, repositoryUsername string
		if err := tx.QueryRow(ctx, `SELECT full_name,repository_username FROM users WHERE id=$1 FOR UPDATE`, student).Scan(&fullName, &repositoryUsername); err != nil {
			return err
		}
		if strings.TrimSpace(fullName) == "" || validateRepositoryUsername(repositoryUsername) != nil {
			return bad("Сначала укажите ФИО и юзернейм репозитория в профиле")
		}
		sl, err := lockSlot(ctx, tx, slot)
		if err != nil {
			return err
		}
		now, err := dbNow(ctx, tx)
		if err != nil {
			return err
		}
		if !sl.StartsAt.After(now) || sl.Status != "open" || sl.WindowStatus != "published" {
			return conflict("Слот уже начался или отменён")
		}
		var owner int64
		err = tx.QueryRow(ctx, `SELECT id,student_id FROM bookings WHERE slot_id=$1 AND status='confirmed'`, slot).Scan(&booking, &owner)
		if err == nil {
			if owner == student {
				return nil
			}
			return conflict("Этот слот уже занят")
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var sameHomework, overlapping bool
		err = tx.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM bookings b JOIN slots sl ON sl.id=b.slot_id JOIN availability_windows w ON w.id=sl.window_id WHERE b.student_id=$1 AND b.status='confirmed' AND sl.ends_at>$2 AND w.homework_id=$3),
 EXISTS(SELECT 1 FROM bookings b JOIN slots sl ON sl.id=b.slot_id WHERE b.student_id=$1 AND b.status='confirmed' AND sl.ends_at>$2 AND sl.starts_at<$5 AND sl.ends_at>$4)`, student, now, sl.HomeworkID, sl.StartsAt, sl.EndsAt).Scan(&sameHomework, &overlapping)
		if err != nil {
			return err
		}
		if sameHomework {
			return conflict("У вас уже есть запись на это ДЗ. Чтобы выбрать другой слот, сначала отмените текущую запись в разделе «Мои записи».")
		}
		if overlapping {
			return conflict("Это время пересекается с вашей другой записью. Выберите свободное время или отмените текущую запись в разделе «Мои записи».")
		}
		if err = tx.QueryRow(ctx, `INSERT INTO bookings(slot_id,student_id) VALUES($1,$2) RETURNING id`, slot, student).Scan(&booking); err != nil {
			return err
		}
		if err = audit(ctx, tx, student, "book", "booking", booking); err != nil {
			return err
		}
		return s.bookingNotice(ctx, tx, booking, "confirmed", "")
	})
	return booking, err
}
func (s *Server) myBookings(w http.ResponseWriter, r *http.Request) error {
	limit, offset := page(r)
	items, err := objects(r.Context(), s.db, `SELECT b.id,b.status,b.cancellation_reason,b.created_at,s.id AS slot_id,s.starts_at,s.ends_at,'ДЗ №'||h.number AS homework_title,COALESCE(NULLIF('@'||u.username,'@'),'Ассистент') AS assistant_name,u.username AS assistant_username FROM bookings b JOIN slots s ON s.id=b.slot_id JOIN availability_windows w ON w.id=s.window_id JOIN homeworks h ON h.id=w.homework_id JOIN users u ON u.id=w.assistant_id WHERE b.student_id=$1 ORDER BY (s.ends_at>clock_timestamp() AND b.status='confirmed') DESC,s.starts_at DESC,b.id DESC LIMIT $2 OFFSET $3`, current(r).ID, limit, offset)
	if err != nil {
		return err
	}
	return respond(w, 200, items)
}
func (s *Server) cancelBooking(w http.ResponseWriter, r *http.Request) error {
	id, err := resourceID(r)
	if err != nil {
		return err
	}
	if err = s.cancelStudent(r.Context(), current(r).ID, id); err != nil {
		return err
	}
	return respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) cancelStudent(ctx context.Context, student, id int64) error {
	return transaction(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, student); err != nil {
			return err
		}
		var slot int64
		err := tx.QueryRow(ctx, `SELECT slot_id FROM bookings WHERE id=$1 AND student_id=$2`, id, student).Scan(&slot)
		if errors.Is(err, pgx.ErrNoRows) {
			return missing()
		}
		if err != nil {
			return err
		}
		sl, err := lockSlot(ctx, tx, slot)
		if err != nil {
			return err
		}
		var status string
		if err = tx.QueryRow(ctx, `SELECT status FROM bookings WHERE id=$1 FOR UPDATE`, id).Scan(&status); err != nil {
			return err
		}
		if status != "confirmed" {
			return nil
		}
		now, err := dbNow(ctx, tx)
		if err != nil {
			return err
		}
		if !sl.StartsAt.After(now) {
			return conflict("Нельзя отменить начавшуюся защиту")
		}
		if _, err = tx.Exec(ctx, `UPDATE bookings SET status='cancelled_by_student',cancelled_at=clock_timestamp(),cancelled_by=$2,updated_at=now() WHERE id=$1`, id, student); err != nil {
			return err
		}
		if err = audit(ctx, tx, student, "cancel_student", "booking", id); err != nil {
			return err
		}
		return s.bookingNotice(ctx, tx, id, "cancelled_by_student", "")
	})
}
func (s *Server) cancelWindow(w http.ResponseWriter, r *http.Request) error {
	return s.cancelAssistantHTTP(w, r, true)
}
func (s *Server) cancelSlot(w http.ResponseWriter, r *http.Request) error {
	return s.cancelAssistantHTTP(w, r, false)
}
func (s *Server) cancelAssistantHTTP(w http.ResponseWriter, r *http.Request, whole bool) error {
	id, err := resourceID(r)
	if err != nil {
		return err
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	if len([]rune(in.Reason)) > 2000 {
		return bad("Слишком длинная причина отмены")
	}
	if err = s.cancelAssistant(r.Context(), current(r).ID, id, whole, strings.TrimSpace(in.Reason)); err != nil {
		return err
	}
	return respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) cancelAssistant(ctx context.Context, actor, id int64, whole bool, reason string) error {
	return transaction(ctx, s.db, func(tx pgx.Tx) error {
		window := id
		if !whole {
			if err := tx.QueryRow(ctx, `SELECT window_id FROM slots WHERE id=$1`, id).Scan(&window); errors.Is(err, pgx.ErrNoRows) {
				return missing()
			} else if err != nil {
				return err
			}
		}
		var starts time.Time
		var status string
		err := tx.QueryRow(ctx, `SELECT starts_at,status FROM availability_windows WHERE id=$1 AND assistant_id=$2 FOR UPDATE`, window, actor).Scan(&starts, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return missing()
		}
		if err != nil {
			return err
		}
		if status == "cancelled" {
			return nil
		}
		now, err := dbNow(ctx, tx)
		if err != nil {
			return err
		}
		if whole && !starts.After(now) {
			return conflict("Окно уже началось. Можно отменить отдельные будущие слоты")
		}
		rows, err := tx.Query(ctx, `SELECT id,starts_at,status FROM slots WHERE window_id=$1 AND ($2 OR id=$3) ORDER BY id FOR UPDATE`, window, whole, id)
		if err != nil {
			return err
		}
		var slotIDs []int64
		for rows.Next() {
			var sid int64
			var at time.Time
			var st string
			if err = rows.Scan(&sid, &at, &st); err != nil {
				rows.Close()
				return err
			}
			if st == "cancelled" {
				continue
			}
			slotIDs = append(slotIDs, sid)
			starts = at
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if !whole && len(slotIDs) == 0 {
			return nil
		}
		now, err = dbNow(ctx, tx)
		if err != nil {
			return err
		}
		if !whole && !starts.After(now) {
			return conflict("Слот уже начался")
		}
		// Recheck the earliest affected slot after waiting for all locks.
		var tooLate bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM slots WHERE id=ANY($1) AND starts_at<=clock_timestamp())`, slotIDs).Scan(&tooLate); err != nil {
			return err
		}
		if tooLate {
			return conflict("Защита уже началась")
		}
		br, err := tx.Query(ctx, `SELECT id FROM bookings WHERE slot_id=ANY($1) AND status='confirmed' ORDER BY id FOR UPDATE`, slotIDs)
		if err != nil {
			return err
		}
		bookings, err := pgx.CollectRows(br, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE slots SET status='cancelled',cancellation_reason=$2,updated_at=now() WHERE id=ANY($1)`, slotIDs, reason); err != nil {
			return err
		}
		if whole {
			if _, err = tx.Exec(ctx, `UPDATE availability_windows SET status='cancelled',cancellation_reason=$2,updated_at=now() WHERE id=$1`, window, reason); err != nil {
				return err
			}
		}
		for _, bid := range bookings {
			if _, err = tx.Exec(ctx, `UPDATE bookings SET status='cancelled_by_assistant',cancellation_reason=$2,cancelled_by=$3,cancelled_at=clock_timestamp(),updated_at=now() WHERE id=$1`, bid, reason, actor); err != nil {
				return err
			}
			if err = s.bookingNotice(ctx, tx, bid, "cancelled_by_assistant", reason); err != nil {
				return err
			}
		}
		entity := "slot"
		if whole {
			entity = "window"
		}
		return audit(ctx, tx, actor, "cancel_assistant", entity, id)
	})
}
func (s *Server) bookingNotice(ctx context.Context, tx pgx.Tx, booking int64, event, reason string) error {
	var telegram int64
	var title, assistant string
	var at time.Time
	if err := tx.QueryRow(ctx, `SELECT u.telegram_id,'ДЗ №'||h.number,sl.starts_at,COALESCE(NULLIF('@'||a.username,'@'),'Ассистент') FROM bookings b JOIN users u ON u.id=b.student_id JOIN slots sl ON sl.id=b.slot_id JOIN availability_windows w ON w.id=sl.window_id JOIN homeworks h ON h.id=w.homework_id JOIN users a ON a.id=w.assistant_id WHERE b.id=$1`, booking).Scan(&telegram, &title, &at, &assistant); err != nil {
		return err
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	heading := "Вы записаны на защиту"
	if event != "confirmed" {
		heading = "Запись на защиту отменена"
	}
	body := fmt.Sprintf("%s\n%s · %s МСК\nАссистент: %s", heading, title, at.In(loc).Format("02.01.2006 15:04"), assistant)
	if reason != "" {
		body += "\nПричина: " + reason
	}
	return s.enqueue(ctx, tx, fmt.Sprintf("booking:%d:%s", booking, event), telegram, s.scheduleMessage(telegram, body))
}
