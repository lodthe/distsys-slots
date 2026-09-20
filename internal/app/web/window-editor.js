import { $, h, state, button, field, empty, post, select, timePicker } from './core.js';
import { time, date, isoDay, minutes } from './dates.js';
import { scheduleCalendar } from './calendar.js';
import { newDraft, draftWindow, previewSlots, publicationWindow } from './draft.js';

export function windowCommentField(input) {
  input.maxLength = 2000;
  input.rows = 3;
  input.placeholder = 'Например, ссылка на встречу для защиты';
  return h(
    'div',
    {},
    field('Комментарий к защите (необязательно)', input),
    h(
      'p',
      { class: 'muted small' },
      'Студент получит комментарий за 5 минут до своего слота. Например, добавьте ссылку на встречу для защиты. Не забудьте включить комнату ожидания для входа.',
    ),
  );
}

export function createWindowEditor(onPublished) {
  function readPrefs() {
    try {
      return JSON.parse(localStorage.getItem('distsys-prefs:' + state.user.id) || '{}');
    } catch {
      return {};
    }
  }
  function builder() {
    const homeworks = state.hw.filter((v) => v.defense_dates.some((day) => day >= isoDay()));
    if (!homeworks.length) {
      $('content').replaceChildren(
        empty(
          'Даты защит ещё не открыты',
          'Администратор добавит ДЗ и даты, после чего вы сможете указать своё время.',
        ),
      );
      return;
    }
    const d = (state.draft ||= newDraft(state.hw, readPrefs()));
    let homework = homeworks.find((v) => String(v.id) === String(d.homework_id));
    if (!homework) {
      homework = homeworks[0];
      d.homework_id = homework.id;
      d.day = '';
      d.month = (homework.defense_dates.find((v) => v >= isoDay()) || isoDay()).slice(0, 7);
    }
    const allowed = homework.defense_dates;
    if (!allowed.includes(d.day)) d.day = '';
    const update = () => {
      d.key = crypto.randomUUID();
      builder();
    };
    const selectDate = (value) => {
      if (d.day === value) return;
      d.day = value;
      update();
    };
    const calendar = scheduleCalendar({
      month: d.month,
      selected: d.day ? [d.day] : [],
      allowedDays: allowed,
      disabledBefore: isoDay(),
      compact: true,
      onMonth: (value) => {
        d.month = value;
        builder();
      },
      onDay: selectDate,
    });
    const dates = h(
      'section',
      { class: 'card' },
      h('div', { class: 'step' }, '01 / Выберите день'),
      h('h2', {}, 'Когда принимаете?'),
      h('p', { class: 'muted small' }, 'Доступны даты, открытые администратором для выбранного ДЗ.'),
      calendar,
    );
    const timing = h(
      'section',
      { class: 'card' },
      h('div', { class: 'step' }, '02 / Укажите время'),
      h('h2', {}, 'Окно приёма'),
      h(
        'div',
        { class: 'fields' },
        field(
          'Начало',
          timePicker(
            d.start,
            (value) => {
              d.start = value;
              update();
            },
            'Начало',
          ),
        ),
        field(
          'Конец',
          timePicker(
            d.end,
            (value) => {
              d.end = value;
              update();
            },
            'Конец',
          ),
        ),
      ),
      minutes(d.end) <= minutes(d.start)
        ? h('p', { class: 'danger small', role: 'alert' }, 'Окончание должно быть позже начала')
        : null,
    );
    timing.append(
      field(
        'Длительность одного слота, минут',
        h('input', {
          type: 'number',
          min: 1,
          max: 180,
          value: d.slot_minutes,
          onchange: (e) => {
            d.slot_minutes = Number(e.target.value);
            update();
          },
        }),
      ),
    );
    timing.append(
      windowCommentField(
        h('textarea', {
          value: d.comment,
          oninput: (e) => {
            d.comment = e.target.value;
            d.key = crypto.randomUUID();
          },
        }),
      ),
    );
    const preview = h(
      'section',
      { class: 'card' },
      h('div', { class: 'step' }, '03 / Проверьте слоты'),
      h('h2', {}, 'Ревью слотов'),
    );
    const item = draftWindow(d);
    let count = 0,
      hasErrors = false;
    if (!item) preview.append(h('p', { class: 'muted' }, 'Выберите день — здесь появится список слотов.'));
    else {
      const duration = (new Date(item.ends_at) - new Date(item.starts_at)) / 60000;
      const rows = previewSlots(item, d.removedSlots);
      const visible = rows.filter((row) => !row.removed);
      const n = rows.length;
      count = visible.length;
      const outsideDates =
        !allowed.includes(isoDay(new Date(item.starts_at))) ||
        !allowed.includes(isoDay(new Date(new Date(item.ends_at).getTime() - 1)));
      const bad = outsideDates || n < 1 || new Date(item.starts_at) <= new Date();
      hasErrors = bad;
      const body = h('tbody');
      for (const slot of visible) {
        const remove = button(
          'Удалить',
          () => {
            d.removedSlots[slot.key] = true;
            d.key = crypto.randomUUID();
            builder();
          },
          'danger',
        );
        remove.setAttribute('aria-label', 'Удалить слот ' + time(slot.starts_at) + '–' + time(slot.ends_at));
        body.append(
          h(
            'tr',
            { 'data-slot-start': slot.starts_at },
            h('td', {}, time(slot.starts_at)),
            h('td', {}, time(slot.ends_at)),
            h('td', {}, remove),
          ),
        );
      }
      const table = visible.length
        ? h(
            'div',
            { class: 'review-table-scroll' },
            h(
              'table',
              { class: 'review-table' },
              h('caption', {}, 'Слоты · ' + date(item.starts_at)),
              h(
                'thead',
                {},
                h(
                  'tr',
                  {},
                  h('th', { scope: 'col' }, 'Начало'),
                  h('th', { scope: 'col' }, 'Конец'),
                  h('th', { scope: 'col' }, 'Действие'),
                ),
              ),
              body,
            ),
          )
        : h(
            'p',
            { class: 'muted' },
            n ? 'Все слоты удалены. Это окно не будет опубликовано.' : 'Проверьте длительность окна и слота.',
          );
      preview.append(
        h(
          'div',
          { class: 'subcard' + (bad ? ' conflict' : '') },
          h('strong', {}, date(item.starts_at) + ' · ' + time(item.starts_at) + '–' + time(item.ends_at)),
          h(
            'p',
            { class: 'muted small' },
            visible.length +
              ' слотов · остаток ' +
              (duration % d.slot_minutes) +
              ' мин' +
              (n > visible.length ? ' · удалено: ' + (n - visible.length) : ''),
          ),
          bad
            ? h(
                'p',
                { class: 'danger' },
                outsideDates ? 'Время выходит за разрешённые даты защиты' : 'Проверьте дату и длительность',
              )
            : null,
          table,
          n > visible.length
            ? button('Вернуть удалённые слоты', () => {
                for (const row of rows) delete d.removedSlots[row.key];
                update();
              })
            : null,
        ),
      );
    }
    const publish = button(
      'Опубликовать',
      async () => {
        const clean = publicationWindow(draftWindow(d), d.removedSlots);
        if (!clean) throw new Error('Укажите время окна и оставьте хотя бы один слот');
        await post('/assistant/windows', clean, { 'Idempotency-Key': d.key });
        try {
          localStorage.setItem(
            'distsys-prefs:' + state.user.id,
            JSON.stringify({ duration: minutes(d.end) - minutes(d.start), slot_minutes: d.slot_minutes }),
          );
        } catch {}
        state.draft = null;
        await onPublished();
      },
      'primary',
    );
    publish.disabled = !count || hasErrors;
    preview.append(
      h(
        'div',
        { class: 'summary' },
        h(
          'div',
          {},
          h('strong', {}, count + ' слотов'),
          h('div', { class: 'muted small' }, 'После публикации студенты смогут записаться'),
        ),
        publish,
      ),
    );
    $('content').replaceChildren(
      h(
        'section',
        { class: 'card' },
        field(
          'Домашнее задание',
          select(
            homeworks.map((v) => [v.id, v.title]),
            d.homework_id,
            (value) => {
              d.homework_id = Number(value);
              const next = homeworks.find((v) => v.id === d.homework_id);
              d.day = '';
              d.month = (next.defense_dates.find((v) => v >= isoDay()) || isoDay()).slice(0, 7);
              update();
            },
          ),
        ),
      ),
      h('div', { class: 'split' }, dates, timing),
      preview,
    );
  }
  return builder;
}
