'use strict';
// Half-open intervals: an event ending at 18:08 does not overlap one starting then.
// Each connected overlap group shares its maximum concurrent column count.
function layoutTimedEvents(items) {
  const sorted = items
    .map((item) => ({ ...item }))
    .filter((item) => Number.isFinite(item.start) && Number.isFinite(item.end) && item.end > item.start)
    .sort((a, b) => a.start - b.start || b.end - a.end || String(a.id).localeCompare(String(b.id)));
  let group = [],
    ends = [],
    groupEnd = -Infinity;
  const finish = () => {
    for (const item of group) item.columns = ends.length;
    group = [];
    ends = [];
  };
  for (const item of sorted) {
    if (item.start >= groupEnd) {
      finish();
      groupEnd = -Infinity;
    }
    let column = ends.findIndex((end) => end <= item.start);
    if (column === -1) column = ends.length;
    ends[column] = item.end;
    item.column = column;
    group.push(item);
    groupEnd = Math.max(groupEnd, item.end);
  }
  finish();
  return sorted;
}
export { layoutTimedEvents };
