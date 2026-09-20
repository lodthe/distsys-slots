package app

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Server) adminOnly(next endpoint) endpoint {
	return s.authorized(false, func(w http.ResponseWriter, r *http.Request) error {
		if !current(r).Admin {
			return &problem{403, "forbidden", "Доступно только администратору"}
		}
		return next(w, r)
	})
}

const homeworkSelect = `SELECT h.id,h.number,'ДЗ №'||h.number AS title,
 ARRAY(SELECT to_char(defense_date,'YYYY-MM-DD') FROM homework_defense_dates WHERE homework_id=h.id ORDER BY defense_date) AS defense_dates,
 (SELECT count(*) FROM availability_windows WHERE homework_id=h.id AND status='published') AS window_count
 FROM homeworks h `

type HomeworkInput struct {
	Number int      `json:"number"`
	Dates  []string `json:"defense_dates"`
}

func (v *HomeworkInput) validate() error {
	if v.Number < 1 || v.Number > 10000 {
		return bad("Укажите номер ДЗ от 1 до 10000")
	}
	if len(v.Dates) == 0 || len(v.Dates) > 366 {
		return bad("Выберите от 1 до 366 дат защит")
	}
	seen := map[string]bool{}
	for _, d := range v.Dates {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return bad("Некорректная дата защиты")
		}
		if seen[d] {
			return bad("Дата защиты повторяется")
		}
		seen[d] = true
	}
	sort.Strings(v.Dates)
	return nil
}

func (s *Server) createHomework(w http.ResponseWriter, r *http.Request) error {
	return s.saveHomeworkHTTP(w, r, false)
}
func (s *Server) updateHomework(w http.ResponseWriter, r *http.Request) error {
	return s.saveHomeworkHTTP(w, r, true)
}
func (s *Server) saveHomeworkHTTP(w http.ResponseWriter, r *http.Request, update bool) error {
	var id int64
	var err error
	if update {
		id, err = resourceID(r)
		if err != nil {
			return err
		}
	}
	var in HomeworkInput
	if err = decode(r, &in); err != nil {
		return err
	}
	id, err = s.saveHomework(r.Context(), current(r).ID, id, in)
	if err != nil {
		return err
	}
	status := 201
	if update {
		status = 200
	}
	return respond(w, status, map[string]int64{"id": id})
}

func (s *Server) saveHomework(ctx context.Context, actor, id int64, in HomeworkInput) (int64, error) {
	if err := in.validate(); err != nil {
		return 0, err
	}
	result := id
	err := transaction(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, actor); err != nil {
			return err
		}
		if id != 0 {
			var found int64
			if err := tx.QueryRow(ctx, `SELECT id FROM homeworks WHERE id=$1 FOR UPDATE`, id).Scan(&found); errors.Is(err, pgx.ErrNoRows) {
				return missing()
			} else if err != nil {
				return err
			}
			// Dates govern new publications only. The homework lock serializes
			// changes with publication; existing windows and bookings are untouched.
			if _, err := tx.Exec(ctx, `UPDATE homeworks SET number=$2 WHERE id=$1`, id, in.Number); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM homework_defense_dates WHERE homework_id=$1`, id); err != nil {
				return err
			}
		} else {
			if err := tx.QueryRow(ctx, `INSERT INTO homeworks(number) VALUES($1) RETURNING id`, in.Number).Scan(&result); err != nil {
				return err
			}
		}
		for _, d := range in.Dates {
			if _, err := tx.Exec(ctx, `INSERT INTO homework_defense_dates(homework_id,defense_date) VALUES($1,$2::text::date)`, result, d); err != nil {
				return err
			}
		}
		return audit(ctx, tx, actor, "save_homework", "homework", result)
	})
	return result, err
}
