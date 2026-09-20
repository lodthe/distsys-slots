(async () => {
const assert = require('node:assert/strict');
const {layoutTimedEvents} = await import('../../internal/app/web/time-layout.js');
const events = ranges => ranges.map(([start, end], id) => ({id, start, end}));
function check(ranges, columns) {
  const input = events(ranges), before = JSON.stringify(input), result = layoutTimedEvents(input);
  assert.equal(JSON.stringify(input), before, 'must not mutate inputs');
  assert.deepEqual(result.map(v => v.columns), columns);
  for (const a of result) for (const b of result) {
    if (a.id !== b.id && a.start < b.end && a.end > b.start) assert.notEqual(a.column, b.column, 'overlapping events share a column');
    assert(a.column >= 0 && a.column < a.columns);
  }
  return result;
}
check([], []);
check([[0, 8], [8, 16]], [1, 1]);
check([[0, 8], [0, 8]], [2, 2]);
check([[0, 8], [0, 8], [0, 8]], [3, 3, 3]);
check([[0, 90], [8, 16], [16, 24], [90, 98]], [2, 2, 2, 1]);
check([[0, 8], [4, 12], [8, 16]], [2, 2, 2]);
check([[0, 30], [2, 12], [3, 8], [12, 20], [40, 48]], [3, 3, 3, 3, 1]);
assert.equal(layoutTimedEvents([{start: 2, end: 1}, {start: NaN, end: 8}]).length, 0);
// More than one page and many simultaneous assistants.
const many = Array.from({length: 180}, (_, n) => [Math.floor(n / 3) * 8, Math.floor(n / 3) * 8 + 8]);
check(many, Array(180).fill(3));
console.log('PASS: timed calendar layout: touching, partial, nested, chained, 2/3 columns, reuse, >100 events.');

})().catch(e => {console.error(e); process.exit(1);});
