package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type WindowInput struct {
	HomeworkID    int64     `json:"homework_id"`
	StartsAt      time.Time `json:"starts_at"`
	EndsAt        time.Time `json:"ends_at"`
	SlotMinutes   int       `json:"slot_minutes"`
	ExcludedSlots []int     `json:"excluded_slot_indices,omitempty"`
	Comment       string    `json:"comment,omitempty"`
}

func (v WindowInput) validate(now time.Time) error {
	if err := validateWindowComment(v.Comment); err != nil {
		return err
	}
	if v.HomeworkID <= 0 {
		return bad("Выберите домашнее задание")
	}
	if !v.StartsAt.After(now) || !v.EndsAt.After(v.StartsAt) {
		return bad("Выберите будущий интервал с окончанием позже начала")
	}
	duration := v.EndsAt.Sub(v.StartsAt)
	if v.SlotMinutes < 1 || v.SlotMinutes > 180 || duration > 24*time.Hour || duration < time.Duration(v.SlotMinutes)*time.Minute {
		return bad("Окно должно вмещать хотя бы один слот и длиться не более суток; слот — от 1 до 180 минут")
	}
	count := int(duration / (time.Duration(v.SlotMinutes) * time.Minute))
	if len(v.ExcludedSlots) >= count {
		return bad("В окне должен остаться хотя бы один слот")
	}
	seen := make(map[int]bool, len(v.ExcludedSlots))
	for _, index := range v.ExcludedSlots {
		if index < 0 || index >= count || seen[index] {
			return bad("Некорректный список удалённых слотов")
		}
		seen[index] = true
	}
	if v.StartsAt.Second() != 0 || v.EndsAt.Second() != 0 || v.StartsAt.Nanosecond() != 0 || v.EndsAt.Nanosecond() != 0 {
		return bad("Укажите время с точностью до минуты")
	}

	return nil
}
func (s *Server) homeworks(w http.ResponseWriter, r *http.Request) error {
	items, err := objects(r.Context(), s.db, homeworkSelect+`ORDER BY h.number`)
	if err != nil {
		return err
	}
	return respond(w, 200, items)
}
func dateRange(r *http.Request) (time.Time, time.Time, error) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	from := time.Now()
	to := from.AddDate(0, 3, 0)
	var err error
	if raw := r.URL.Query().Get("date_from"); raw != "" {
		from, err = time.ParseInLocation("2006-01-02", raw, loc)
		if err != nil {
			return from, to, bad("Некорректная дата")
		}
		to = from.AddDate(0, 3, 0)
	}
	if raw := r.URL.Query().Get("date_to"); raw != "" {
		to, err = time.ParseInLocation("2006-01-02", raw, loc)
		if err != nil {
			return from, to, bad("Некорректная дата")
		}
		to = to.AddDate(0, 0, 1)
	}
	if !to.After(from) {
		return from, to, bad("Некорректный диапазон дат")
	}
	return from, to, nil
}

const slotSelect = `SELECT s.id,s.window_id,s.starts_at,s.ends_at,s.status,w.homework_id,'ДЗ №'||h.number AS homework_title,w.assistant_id,COALESCE(NULLIF('@'||u.username,'@'),'Ассистент') AS assistant_name,u.username AS assistant_username,w.status AS window_status,EXISTS(SELECT 1 FROM bookings b WHERE b.slot_id=s.id AND b.status='confirmed') AS booked FROM slots s JOIN availability_windows w ON w.id=s.window_id JOIN users u ON u.id=w.assistant_id JOIN homeworks h ON h.id=w.homework_id `

func (s *Server) listSlots(w http.ResponseWriter, r *http.Request) error {
	from, to, err := dateRange(r)
	if err != nil {
		return err
	}
	limit, offset := page(r)
	items, err := objects(r.Context(), s.db, slotSelect+`WHERE s.status='open' AND w.status='published' AND s.starts_at>clock_timestamp() AND s.starts_at>=$1 AND s.starts_at<$2 AND ($3='' OR w.homework_id::text=$3) AND NOT EXISTS(SELECT 1 FROM bookings b WHERE b.slot_id=s.id AND b.status='confirmed') ORDER BY s.starts_at,s.id LIMIT $4 OFFSET $5`, from, to, r.URL.Query().Get("homework_id"), limit, offset)
	if err != nil {
		return err
	}
	return respond(w, 200, items)
}
func (s *Server) slotDetails(w http.ResponseWriter, r *http.Request) error {
	id, err := resourceID(r)
	if err != nil {
		return err
	}
	items, err := objects(r.Context(), s.db, slotSelect+`WHERE s.id=$1`, id)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return missing()
	}
	return respond(w, 200, items[0])
}

const windowSelect = `SELECT w.id,w.assistant_id,w.homework_id,w.starts_at,w.ends_at,w.slot_minutes,w.comment,w.status,w.cancellation_reason,w.created_at,w.updated_at,'ДЗ №'||h.number AS homework_title,(SELECT count(*) FROM slots WHERE window_id=w.id) AS slot_count,(SELECT count(*) FROM bookings b JOIN slots s ON s.id=b.slot_id WHERE s.window_id=w.id AND b.status='confirmed') AS booking_count FROM availability_windows w JOIN homeworks h ON h.id=w.homework_id `

func (s *Server) listWindows(w http.ResponseWriter, r *http.Request) error {
	items, err := objects(r.Context(), s.db, windowSelect+`WHERE w.assistant_id=$1 ORDER BY w.starts_at DESC,w.id DESC`, current(r).ID)
	if err != nil {
		return err
	}
	return respond(w, 200, items)
}
func (s *Server) windowDetails(w http.ResponseWriter, r *http.Request) error {
	id, err := resourceID(r)
	if err != nil {
		return err
	}
	items, err := objects(r.Context(), s.db, windowSelect+`WHERE w.id=$1 AND w.assistant_id=$2`, id, current(r).ID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return missing()
	}
	slots, err := objects(r.Context(), s.db, `SELECT s.id,s.starts_at,s.ends_at,s.status,s.cancellation_reason,b.id AS booking_id,u.full_name,u.username,u.repository_username FROM slots s LEFT JOIN bookings b ON b.slot_id=s.id AND b.status='confirmed' LEFT JOIN users u ON u.id=b.student_id WHERE s.window_id=$1 ORDER BY s.starts_at`, id)
	if err != nil {
		return err
	}
	for _, slot := range slots {
		username, _ := slot["repository_username"].(string)
		slot["repository_url"] = repositoryURL(username)
	}
	items[0]["slots"] = slots
	return respond(w, 200, items[0])
}
func (s *Server) createWindow(w http.ResponseWriter, r *http.Request) error {
	var in WindowInput
	if err := decode(r, &in); err != nil {
		return err
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 128 {
		return bad("Отсутствует ключ публикации")
	}
	id, err := s.publishWindow(r.Context(), current(r).ID, key, in)
	if err != nil {
		return err
	}
	return respond(w, 201, map[string]int64{"id": id})
}

func validateWindowComment(comment string) error {
	if len([]rune(comment)) > 2000 {
		return bad("Комментарий — не более 2000 символов")
	}
	return nil
}

func (s *Server) updateWindowComment(w http.ResponseWriter, r *http.Request) error {
	id, err := resourceID(r)
	if err != nil {
		return err
	}
	var in struct {
		Comment *string `json:"comment"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	if in.Comment == nil {
		return bad("Передайте комментарий или пустую строку для удаления")
	}
	comment := strings.TrimSpace(*in.Comment)
	if err = validateWindowComment(comment); err != nil {
		return err
	}
	err = transaction(r.Context(), s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE availability_windows SET comment=$3,updated_at=now() WHERE id=$1 AND assistant_id=$2`, id, current(r).ID, comment)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return missing()
		}
		return audit(r.Context(), tx, current(r).ID, "update_comment", "window", id)
	})
	if err != nil {
		return err
	}
	return respond(w, 200, map[string]string{"comment": comment})
}
func (s *Server) publishWindow(ctx context.Context, actor int64, key string, in WindowInput) (int64, error) {
	raw, _ := json.Marshal(in)
	hash := digest(string(raw))
	var id int64
	err := transaction(ctx, s.db, func(tx pgx.Tx) error {
		id = 0
		if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, actor); err != nil {
			return err
		}
		var storedHash string
		var stored []byte
		err := tx.QueryRow(ctx, `SELECT request_hash,response FROM idempotency_keys WHERE user_id=$1 AND key=$2`, actor, key).Scan(&storedHash, &stored)
		if err == nil {
			if storedHash != hash {
				return conflict("Повторный ключ относится к другому запросу")
			}
			return json.Unmarshal(stored, &id)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var homeworkID int64
		if err = tx.QueryRow(ctx, `SELECT id FROM homeworks WHERE id=$1 FOR SHARE`, in.HomeworkID).Scan(&homeworkID); errors.Is(err, pgx.ErrNoRows) {
			return bad("Выберите существующее домашнее задание")
		} else if err != nil {
			return err
		}
		now, err := dbNow(ctx, tx)
		if err != nil {
			return err
		}
		if err = in.validate(now); err != nil {
			return err
		}
		var outsideDates bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM generate_series(($2::timestamptz AT TIME ZONE 'Europe/Moscow')::date::timestamp,(($3::timestamptz-interval '1 microsecond') AT TIME ZONE 'Europe/Moscow')::date::timestamp,interval '1 day') d WHERE NOT EXISTS(SELECT 1 FROM homework_defense_dates WHERE homework_id=$1 AND defense_date=d::date))`, in.HomeworkID, in.StartsAt, in.EndsAt).Scan(&outsideDates); err != nil {
			return err
		}
		if outsideDates {
			return conflict("Окно выходит за разрешённые даты защиты этого ДЗ")
		}
		if err = tx.QueryRow(ctx, `INSERT INTO availability_windows(assistant_id,homework_id,starts_at,ends_at,slot_minutes,comment) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, actor, in.HomeworkID, in.StartsAt, in.EndsAt, in.SlotMinutes, strings.TrimSpace(in.Comment)).Scan(&id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO slots(window_id,starts_at,ends_at) SELECT $1,t,t+($4 * interval '1 minute') FROM generate_series($2::timestamptz,$3::timestamptz-($4 * interval '1 minute'),$4 * interval '1 minute') WITH ORDINALITY AS generated(t,n) WHERE NOT ((n-1)::integer=ANY(COALESCE($5::integer[],'{}'::integer[])))`, id, in.StartsAt, in.EndsAt, in.SlotMinutes, in.ExcludedSlots); err != nil {
			return err
		}
		if err = audit(ctx, tx, actor, "create", "window", id); err != nil {
			return err
		}
		encoded, _ := json.Marshal(id)
		_, err = tx.Exec(ctx, `INSERT INTO idempotency_keys(user_id,key,request_hash,response) VALUES($1,$2,$3,$4)`, actor, key, hash, encoded)
		return err
	})
	return id, err
}
func audit(ctx context.Context, tx pgx.Tx, actor int64, action, entity string, id int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,entity_type,entity_id) VALUES($1,$2,$3,$4)`, actor, action, entity, id)
	return err
}
