import { h, button } from './core.js';
import { fmt, time, date, isoDay, clock } from './dates.js';
import { layoutTimedEvents } from './time-layout.js';

// Shared month grid: Monday first, Moscow dates, keyboard-focusable day buttons.
function scheduleCalendar({
  month,
  selected = [],
  events = [],
  onMonth,
  onDay,
  disabledBefore = '',
  compact = false,
  allowedDays = null,
}) {
  const first = new Date(month + '-01T12:00:00+03:00');
  const shift = (n) => {
    const next = new Date(first);
    next.setUTCMonth(next.getUTCMonth() + n);
    return onMonth(isoDay(next).slice(0, 7));
  };
  const prev = button('‹', () => shift(-1), 'icon');
  prev.setAttribute('aria-label', 'Предыдущий месяц');
  const next = button('›', () => shift(1), 'icon');
  next.setAttribute('aria-label', 'Следующий месяц');
  const grid = h('div', { class: 'month-grid' });
  const start = new Date(first);
  start.setUTCDate(1 - ((first.getUTCDay() + 6) % 7));
  for (let i = 0; i < 42; i++) {
    const at = new Date(start);
    at.setUTCDate(start.getUTCDate() + i);
    const day = isoDay(at);
    const list = events.filter((v) => v.day === day),
      available = list.reduce((n, v) => n + Number(v.free_slots || 0), 0);
    const selectedDay = selected.includes(day);
    const cell = button(
      '',
      () => onDay(day),
      'month-day' +
        (day.slice(0, 7) !== month ? ' outside' : '') +
        (day === isoDay() ? ' today' : '') +
        (list.length ? (available > 0 ? ' available' : ' occupied') : '') +
        (selectedDay ? ' picked' : ''),
    );
    cell.dataset.day = day;
    cell.setAttribute('aria-pressed', String(selectedDay));
    cell.setAttribute('aria-label', date(at) + (list.length ? ' · свободных слотов: ' + available : ''));
    cell.disabled =
      (!!disabledBefore && day < disabledBefore) || (allowedDays !== null && !allowedDays.includes(day));
    cell.append(h('span', { class: 'day-number' }, String(at.getUTCDate())));
    if (list.length) {
      cell.append(h('span', { class: 'day-count' }, available ? available + ' своб.' : 'Нет мест'));
      for (const item of list.slice(0, 2))
        cell.append(
          h(
            'span',
            { class: 'calendar-event tone-' + (Number(item.assistant_id) % 4) },
            time(item.first_at) + ' ' + item.assistant_name,
          ),
        );
      if (list.length > 2) cell.append(h('span', { class: 'event-more' }, '+' + (list.length - 2)));
    }
    grid.append(cell);
  }
  return h(
    'section',
    { class: 'month-calendar' + (compact ? ' compact' : ''), 'aria-label': 'Календарь' },
    h(
      'div',
      { class: 'row between calendar-toolbar' },
      h('h3', {}, fmt(first, { month: 'long', year: 'numeric' })),
      h(
        'div',
        { class: 'row' },
        button('Сегодня', () => onMonth(isoDay().slice(0, 7))),
        prev,
        next,
      ),
    ),
    h(
      'div',
      { class: 'weekday-labels' },
      ...['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс'].map((v) => h('span', {}, v)),
    ),
    grid,
  );
}
function dayTimeline(day, items, onSlot) {
  const midnight = new Date(day + 'T00:00:00+03:00').getTime();
  const placed = layoutTimedEvents(
    items.map((item) => ({
      ...item,
      start: Math.max(0, (new Date(item.starts_at).getTime() - midnight) / 60000),
      end: Math.min(1440, (new Date(item.ends_at).getTime() - midnight) / 60000),
    })),
  );
  if (!placed.length) return h('div');
  const from = Math.floor(Math.min(...placed.map((v) => v.start)) / 30) * 30;
  const to = Math.min(1440, Math.ceil(Math.max(...placed.map((v) => v.end)) / 30) * 30);
  // Keep every slot tappable without distorting its time position.
  const shortest = Math.min(...placed.map((v) => v.end - v.start));
  const scale = Math.max(4, 54 / shortest);
  const columns = Math.max(...placed.map((v) => v.columns));
  const height = (to - from) * scale + 24;
  const axis = h('div', { class: 'time-axis', 'aria-hidden': 'true', style: 'height:' + height + 'px' });
  const canvas = h('div', {
    class: 'time-canvas',
    style: 'height:' + height + 'px;min-width:' + Math.max(0, columns * 120) + 'px',
  });
  for (let minute = from; minute <= to; minute += 30) {
    const top = (minute - from) * scale + 12;
    axis.append(
      h(
        'span',
        { class: 'time-tick', style: 'top:' + top + 'px' },
        minute === 1440 ? '24:00' : clock(minute),
      ),
    );
    canvas.append(h('div', { class: 'time-rule', style: 'top:' + top + 'px', 'aria-hidden': 'true' }));
  }
  for (const item of placed) {
    const title = item.assistant_name || 'Ассистент';
    const details = item.homework_title;
    const b = button(
      '',
      () => onSlot(item.id),
      'timed-event tone-' + (Number(item.assistant_id || item.homework_id) % 4),
    );
    b.dataset.eventId = item.id;
    b.dataset.column = item.column;
    b.dataset.columns = item.columns;
    b.style.top = 12 + (item.start - from) * scale + 'px';
    b.style.height = (item.end - item.start) * scale - 2 + 'px';
    b.style.left = (100 * item.column) / item.columns + '%';
    b.style.width = 'calc(' + 100 / item.columns + '% - 4px)';
    const label = time(item.starts_at) + '–' + time(item.ends_at) + ' · ' + title + ' · ' + details;
    b.setAttribute('aria-label', label);
    b.title = label;
    b.append(
      h('strong', { class: 'event-time' }, time(item.starts_at) + '–' + time(item.ends_at)),
      h('span', { class: 'event-title' }, title),
      h('span', { class: 'event-detail' }, details),
    );
    canvas.append(b);
  }
  const scroll = h(
    'div',
    {
      class: 'time-scroll',
      tabindex: 0,
      role: 'region',
      'aria-label': 'Сетка времени на ' + date(day + 'T12:00:00+03:00'),
    },
    h('div', { class: 'time-board' }, axis, canvas),
  );
  return h('section', { class: 'day-timeline' }, scroll);
}

export { scheduleCalendar, dayTimeline };
