import { isoDay, clock } from './dates.js';

function newDraft(homeworks, p = {}) {
  const homework = homeworks.find((v) => v.defense_dates.some((day) => day >= isoDay()));
  const duration = Number(p.duration);
  return {
    day: '',
    comment: '',
    month: (homework?.defense_dates.find((v) => v >= isoDay()) || isoDay()).slice(0, 7),
    start: '18:00',
    end: clock(18 * 60 + (duration > 0 && duration <= 359 ? duration : 90)),
    homework_id: homework?.id || '',
    slot_minutes: Number(p.slot_minutes) || 8,
    removedSlots: {},
    key: crypto.randomUUID(),
  };
}
function draftWindow(d) {
  if (!d.day) return null;
  const start = new Date(d.day + 'T' + d.start + ':00+03:00');
  const end = new Date(d.day + 'T' + d.end + ':00+03:00');
  if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime())) return null;
  return {
    homework_id: Number(d.homework_id),
    starts_at: start.toISOString(),
    ends_at: end.toISOString(),
    slot_minutes: Number(d.slot_minutes),
    comment: (d.comment || '').trim(),
  };
}
// Deletions are tied to an exact interval, not an index that could move after editing.
function previewSlots(window, removed = {}) {
  const start = new Date(window.starts_at).getTime(),
    end = new Date(window.ends_at).getTime();
  const step = Number(window.slot_minutes) * 60000;
  if (
    !Number.isFinite(start) ||
    !Number.isFinite(end) ||
    !Number.isInteger(window.slot_minutes) ||
    step < 60000 ||
    step > 180 * 60000 ||
    end <= start ||
    end - start > 86400000
  )
    return [];
  const count = Math.floor((end - start) / step);
  return Array.from({ length: count }, (_, index) => {
    const starts_at = new Date(start + index * step).toISOString(),
      ends_at = new Date(start + (index + 1) * step).toISOString();
    const key = starts_at + '/' + ends_at;
    return { index, key, starts_at, ends_at, removed: !!removed[key] };
  });
}
function publicationWindow(item, removed = {}) {
  if (!item) return null;
  const rows = previewSlots(item, removed);
  if (!rows.length || rows.every((row) => row.removed)) return null;
  return { ...item, excluded_slot_indices: rows.filter((row) => row.removed).map((row) => row.index) };
}

export { newDraft, draftWindow, previewSlots, publicationWindow };
