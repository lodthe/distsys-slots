function checkSlotPreview(previewSlots, publicationWindow) {
  const window = {homework_id: 1, starts_at: '2027-02-15T15:00:00Z', ends_at: '2027-02-15T16:30:00Z', slot_minutes: 8};
  const assert = (ok, text) => {if (!ok) throw new Error(text);};
  const rows = previewSlots(window);
  assert(rows.length === 11, '90/8 must produce 11 full slots');
  const removed = {[rows[4].key]: true};
  const remaining = previewSlots(window, removed).filter(row => !row.removed);
  assert(remaining.length === 10 && remaining[4].starts_at === rows[5].starts_at, 'deletion must leave a gap without shifting slots');
  let payload = publicationWindow(window, removed);
  assert(payload && payload.excluded_slot_indices.join() === '4', 'publication must exclude the deleted slot');
  assert(previewSlots({...window, starts_at: '2027-02-16T15:00:00Z', ends_at: '2027-02-16T16:30:00Z'}, removed).every(row => !row.removed), 'deletion must not affect another window');
  assert(previewSlots({...window, slot_minutes: 10}, removed).every(row => !row.removed), 'do not delete different intervals after duration changes');
  for (const row of rows) removed[row.key] = true;
  assert(publicationWindow(window, removed) === null, 'all removed slots must omit the empty window');
  assert(publicationWindow(window, {}).excluded_slot_indices.length === 0, 'restore all');
  assert(previewSlots({...window, slot_minutes: 0}).length === 0, 'invalid duration');
  assert(previewSlots({...window, slot_minutes: 1.5}).length === 0, 'fractional duration');
  assert(previewSlots({...window, starts_at: '2027-02-15T20:56:00Z', ends_at: '2027-02-15T21:12:00Z'}).length === 2, 'Moscow midnight');
  return 'PASS: slot preview, 90/8 remainder, deletion gap, exact publication exclusions, restore, isolation, edits and midnight.';
}
import('../../internal/app/web/draft.js').then(({previewSlots, publicationWindow}) => console.log(checkSlotPreview(previewSlots, publicationWindow))).catch(e => {console.error(e); process.exit(1);});
