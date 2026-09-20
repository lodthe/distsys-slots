package app

import (
	"testing"
	"time"
)

func TestExcludedSlotValidation(t *testing.T) {
	now := time.Date(2027, 2, 15, 12, 0, 0, 0, time.UTC)
	in := WindowInput{HomeworkID: 1, StartsAt: now.Add(time.Hour), EndsAt: now.Add(150 * time.Minute), SlotMinutes: 8}
	for _, indices := range [][]int{nil, {}, {0}, {1, 4, 10}} {
		in.ExcludedSlots = indices
		if err := in.validate(now); err != nil {
			t.Fatal("valid exclusions rejected", indices, err)
		}
	}
	for _, indices := range [][]int{{-1}, {11}, {2, 2}, {0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}} {
		in.ExcludedSlots = indices
		if err := in.validate(now); err == nil {
			t.Fatal("invalid exclusions accepted", indices)
		}
	}
}

func TestPublicationOmitsDeletedSlotsAtomically(t *testing.T) {
	f := setup(t)
	in := f.window(f.start)
	in.ExcludedSlots = []int{1, 4, 10}
	key := randomToken()
	id, err := f.publishWindow(f.ctx, f.assistant, key, in)
	if err != nil {
		t.Fatal(err)
	}
	var count, excluded int
	err = f.db.QueryRow(f.ctx, `SELECT count(*),count(*) FILTER(WHERE starts_at=ANY($2::timestamptz[])) FROM slots WHERE window_id=$1`, id, []time.Time{f.start.Add(8 * time.Minute), f.start.Add(32 * time.Minute), f.start.Add(80 * time.Minute)}).Scan(&count, &excluded)
	if err != nil || count != 8 || excluded != 0 {
		t.Fatal("deleted slots were published", count, excluded, err)
	}
	again, err := f.publishWindow(f.ctx, f.assistant, key, in)
	if err != nil || again != id {
		t.Fatal("idempotency", again, err)
	}
	in.ExcludedSlots = []int{2}
	_, err = f.publishWindow(f.ctx, f.assistant, key, in)
	assertConflict(t, err)
	invalid := f.window(f.start.AddDate(0, 0, 2))
	invalid.ExcludedSlots = []int{11}
	if _, err = f.publishWindow(f.ctx, f.assistant, randomToken(), invalid); err == nil {
		t.Fatal("invalid window accepted")
	}
	if err = f.db.QueryRow(f.ctx, `SELECT count(*) FROM availability_windows`).Scan(&count); err != nil || count != 1 {
		t.Fatal("invalid publication changed windows", count, err)
	}
}
