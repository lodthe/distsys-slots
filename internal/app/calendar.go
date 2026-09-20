package app

import (
	"net/http"
	"time"
)

// Calendar returns counts, never participant data; unlike the slot list it is not paginated.
func (s *Server) calendar(w http.ResponseWriter, r *http.Request) error {
	loc, _ := time.LoadLocation("Europe/Moscow")
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().In(loc).Format("2006-01")
	}
	from, err := time.ParseInLocation("2006-01", month, loc)
	if err != nil {
		return bad("Укажите месяц в формате ГГГГ-ММ")
	}
	items, err := objects(r.Context(), s.db, `SELECT to_char(s.starts_at AT TIME ZONE 'Europe/Moscow','YYYY-MM-DD') AS day,
 w.homework_id,'ДЗ №'||h.number AS homework_title,w.assistant_id,COALESCE(NULLIF('@'||u.username,'@'),'Ассистент') AS assistant_name,
 count(*) AS total_slots,count(*) FILTER(WHERE b.id IS NULL) AS free_slots,min(s.starts_at) AS first_at
 FROM slots s JOIN availability_windows w ON w.id=s.window_id JOIN homeworks h ON h.id=w.homework_id
 JOIN users u ON u.id=w.assistant_id LEFT JOIN bookings b ON b.slot_id=s.id AND b.status='confirmed'
 WHERE s.status='open' AND w.status='published' AND s.starts_at>clock_timestamp() AND s.starts_at>=$1 AND s.starts_at<$2
 AND ($3='' OR w.homework_id::text=$3)
 GROUP BY day,w.homework_id,h.number,w.assistant_id,u.username ORDER BY day,first_at,w.assistant_id,w.homework_id`, from, from.AddDate(0, 1, 0), r.URL.Query().Get("homework_id"))
	if err != nil {
		return err
	}
	return respond(w, 200, items)
}
