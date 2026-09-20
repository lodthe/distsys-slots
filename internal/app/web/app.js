import {
  tg,
  $,
  h,
  state,
  button,
  field,
  empty,
  notify,
  action,
  api,
  post,
  select,
  modal,
  closeModal,
  confirm,
} from './core.js';
import { time, date, isoDay } from './dates.js';
import { scheduleCalendar, dayTimeline } from './calendar.js';
import { createWindowEditor, windowCommentField } from './window-editor.js';

const renderWindowEditor = createWindowEditor(async () => {
  await navigate('windows');
  notify('Окно опубликовано');
});

function navigation() {
  const roles = [
    ...(state.user?.admin ? [['admin', 'Админ']] : []),
    ...(state.user?.assistant || state.user?.admin ? [['assistant', 'Ассистент']] : []),
    ['student', 'Студент'],
  ];
  if (!roles.some(([key]) => key === state.role)) {
    state.role = 'student';
    state.view = needsName() ? 'profile' : homeView();
  }
  const onboarding = state.view === 'profile' && needsName();
  $('nav').hidden = onboarding || state.role === 'admin';
  $('refresh').hidden = state.view === 'profile';
  document.querySelector('footer').textContent =
    'Курс «Распределённые системы»' + (state.view === 'bookings' ? '' : ' · Время по Москве');
  const tabs =
    state.role === 'admin'
      ? []
      : state.role === 'assistant'
        ? [
            ['windows', 'Мои окна'],
            ['create', 'Добавить окно'],
          ]
        : [
            ['slots', 'Расписание'],
            ['bookings', 'Мои записи'],
            ['profile', 'Профиль'],
          ];
  $('nav').replaceChildren(
    ...tabs.map(([key, label]) => {
      const tab = button(label, () => navigate(key), state.view === key ? 'selected' : '');
      if (state.view === key) tab.setAttribute('aria-current', 'page');
      return tab;
    }),
  );
  $('role-switch').hidden = roles.length === 1;
  const activate = async (key) => {
    state.role = key;
    await navigate(needsName() ? 'profile' : homeView());
    $('interface-' + key)?.focus();
  };
  const roleTabs = roles.map(([key, label], index) => {
    const tab = button(label, () => activate(key), state.role === key ? 'selected' : '');
    tab.id = 'interface-' + key;
    tab.setAttribute('role', 'tab');
    tab.setAttribute('aria-selected', String(state.role === key));
    tab.setAttribute('aria-controls', 'content');
    tab.tabIndex = state.role === key ? 0 : -1;
    tab.addEventListener('keydown', (event) => {
      let next;
      if (event.key === 'ArrowRight') next = (index + 1) % roles.length;
      else if (event.key === 'ArrowLeft') next = (index + roles.length - 1) % roles.length;
      else if (event.key === 'Home') next = 0;
      else if (event.key === 'End') next = roles.length - 1;
      else return;
      event.preventDefault();
      action(tab, () => activate(roles[next][0]));
    });
    return tab;
  });
  $('role-switch').replaceChildren(
    h('div', { class: 'interface-tabs', role: 'tablist', 'aria-label': 'Доступные интерфейсы' }, ...roleTabs),
  );
  if (roles.length > 1) {
    $('content').setAttribute('role', 'tabpanel');
    $('content').setAttribute('aria-labelledby', 'interface-' + state.role);
  } else {
    $('content').removeAttribute('role');
    $('content').removeAttribute('aria-labelledby');
  }
}
function initialRole() {
  return state.user.admin ? 'admin' : state.user.assistant ? 'assistant' : 'student';
}
function needsName() {
  return state.role === 'student' && (!state.user.full_name?.trim() || !state.user.repository_username);
}
function homeView() {
  return state.role === 'admin' ? 'homework_admin' : state.role === 'assistant' ? 'windows' : 'slots';
}
async function loadHomeworks() {
  state.hw = await api('/homeworks');
}
async function navigate(view) {
  if (view === 'profile' && state.role !== 'student') view = homeView();
  if (needsName()) view = 'profile';
  state.view = view;
  $('notice').replaceChildren();
  navigation();
  if (view === 'create') await loadHomeworks();
  await render();
}
async function render() {
  const view = state.view;
  if (!state.user) return;
  if (view === 'profile') return profile();
  if (view === 'create') return renderWindowEditor();
  if (view === 'homework_admin') return adminHomeworks();
  $('content').classList.add('loading');
  try {
    if (view === 'slots') await slotsView();
    else if (view === 'bookings') await bookingsView();
    else if (view === 'windows') await windowsView();
  } finally {
    $('content').classList.remove('loading');
  }
}
function profile() {
  const onboarding = needsName();
  const name = h('input', {
    value: state.user.full_name || '',
    placeholder: 'ФИО',
    autocomplete: 'name',
    maxlength: 200,
    required: true,
  });
  const repositoryError = h('span', { id: 'repository-error', class: 'danger small', 'aria-live': 'polite' });
  const repository = h('input', {
    value: state.user.repository_username || '',
    'aria-label': 'Юзернейм репозитория',
    'aria-describedby': 'repository-prefix repository-error',
    placeholder: 'ivanov_ivan_i',
    autocapitalize: 'none',
    spellcheck: 'false',
    required: true,
  });
  const validateRepository = () => {
    const message = /[\p{White_Space}\uFEFF/\\]/u.test(repository.value)
      ? 'В юзернейме нельзя использовать пробелы и слеши'
      : '';
    repository.setCustomValidity(message);
    repositoryError.textContent = message;
    return !message;
  };
  repository.addEventListener('input', validateRepository);
  const form = h(
    'form',
    {
      onsubmit: (e) => {
        e.preventDefault();
        action(form.querySelector('button'), async () => {
          const payload = { full_name: name.value.trim(), repository_username: repository.value };
          if (Object.values(payload).some((value) => !value))
            throw new Error('Укажите ФИО и юзернейм репозитория');
          if (!validateRepository()) {
            repository.reportValidity();
            return;
          }
          await api('/me', { method: 'PATCH', body: JSON.stringify(payload) });
          state.user = (await api('/me')).user;
          await navigate(homeView());
          if (!onboarding) notify('Профиль сохранён');
        });
      },
    },
    h('h2', {}, onboarding ? 'Как вас записать?' : 'Ваш профиль'),
    field('ФИО', name),
    field(
      'Репозиторий',
      h(
        'div',
        { class: 'repository-input' },
        h('span', { id: 'repository-prefix' }, 'https://distsys.ru/hse-2026/'),
        repository,
      ),
    ),
    repositoryError,
    h(
      'div',
      { class: 'actions' },
      h(
        'button',
        { class: 'primary', type: 'submit' },
        onboarding ? 'Перейти к слотам' : 'Сохранить профиль',
      ),
      onboarding ? null : button('Назад', () => navigate(homeView())),
    ),
  );
  $('content').replaceChildren(h('section', { class: 'card profile-card' }, form));
}
async function adminHomeworks() {
  await loadHomeworks();
  if (state.view !== 'homework_admin') return;
  $('content').replaceChildren(
    h(
      'div',
      { class: 'row between' },
      h('h2', {}, 'ДЗ и даты защит'),
      button('+ Добавить ДЗ', () => homeworkEditor(), 'primary'),
    ),
    h(
      'p',
      { class: 'muted' },
      'Одна настройка: номер ДЗ и даты, в которые ассистенты могут принимать защиты.',
    ),
  );
  if (!state.hw.length)
    $('content').append(
      empty('Расписание начинается здесь', 'Добавьте номер ДЗ и отметьте разрешённые даты в календаре.'),
    );
  for (const item of state.hw)
    $('content').append(
      h(
        'section',
        { class: 'card' },
        h('h2', {}, item.title),
        h(
          'div',
          { class: 'chips' },
          ...item.defense_dates.map((v) => h('span', { class: 'tag' }, date(v + 'T12:00:00+03:00'))),
        ),
        h('p', { class: 'muted small' }, 'Опубликовано окон: ' + item.window_count),
        button('Изменить ДЗ', () => homeworkEditor(item)),
      ),
    );
}
function homeworkEditor(item = null) {
  let dates = [...(item?.defense_dates || [])],
    month = (dates.find((v) => v >= isoDay()) || isoDay()).slice(0, 7);
  const number = h('input', {
    type: 'number',
    min: 1,
    max: 10000,
    step: 1,
    required: true,
    value: item?.number || Math.max(0, ...state.hw.map((v) => v.number)) + 1,
  });
  const calendar = h('div'),
    chips = h('div', { class: 'chips' });
  const show = () => {
    calendar.replaceChildren(
      scheduleCalendar({
        month,
        selected: dates,
        onMonth: (value) => {
          month = value;
          show();
        },
        onDay: (value) => {
          dates = dates.includes(value) ? dates.filter((d) => d !== value) : [...dates, value].sort();
          show();
        },
        disabledBefore: isoDay(),
        compact: true,
      }),
    );
    chips.replaceChildren(
      ...dates.map((value) =>
        button(date(value + 'T12:00:00+03:00') + ' ×', () => {
          dates = dates.filter((v) => v !== value);
          show();
        }),
      ),
    );
  };
  show();
  modal(
    h(
      'div',
      {},
      h('h2', {}, item ? 'Изменить ДЗ' : 'Новое ДЗ'),
      field('Номер ДЗ', number),
      h('p', { class: 'muted' }, 'Отметьте даты защит — можно выбрать несколько дней.'),
      calendar,
      chips,
      h(
        'p',
        { class: 'muted small' },
        'Даты разрешают создавать новые окна. Удаление даты не отменяет уже опубликованные окна и записи студентов.',
      ),
      h(
        'div',
        { class: 'actions' },
        button('Назад', closeModal),
        button(
          'Сохранить ДЗ',
          async () => {
            const data = { number: Number(number.value), defense_dates: dates };
            if (!number.checkValidity() || !Number.isInteger(data.number))
              throw new Error('Укажите целый номер ДЗ от 1 до 10000');
            if (!dates.length) throw new Error('Выберите хотя бы одну дату защиты');
            if (item)
              await api('/admin/homeworks/' + item.id, { method: 'PATCH', body: JSON.stringify(data) });
            else await post('/admin/homeworks', data);
            closeModal();
            await adminHomeworks();
            notify('ДЗ сохранено');
          },
          'primary',
        ),
      ),
    ),
  );
}
let scheduleRequest = 0;
async function slotsView() {
  const request = ++scheduleRequest;
  const homework = state.hw.find((v) => String(v.id) === state.homeworkID) || state.hw[0];
  if (!homework) {
    state.homeworkID = '';
    $('content').replaceChildren(
      h('h2', {}, 'Расписание защит'),
      empty('ДЗ пока не добавлены', 'Расписание появится, когда администратор добавит домашнее задание.'),
    );
    return;
  }
  const homeworkID = String(homework.id);
  if (state.homeworkID !== homeworkID) state.slotDay = '';
  state.homeworkID = homeworkID;
  const month = (state.slotMonth ||= isoDay().slice(0, 7));
  const calendarParams = new URLSearchParams({ month, homework_id: homeworkID });
  const events = await api('/calendar?' + calendarParams);
  if (state.view !== 'slots' || request !== scheduleRequest) return;
  const availableDays = events
    .filter((v) => Number(v.free_slots) > 0)
    .map((v) => v.day)
    .sort();
  if (!state.slotDay || state.slotDay.slice(0, 7) !== month)
    state.slotDay = availableDays[0] || (month === isoDay().slice(0, 7) ? isoDay() : month + '-01');
  const selectedDay = state.slotDay;
  const params = new URLSearchParams({
    homework_id: homeworkID,
    date_from: selectedDay,
    date_to: selectedDay,
  });
  const list = [];
  // Lay out the entire day, including overlaps crossing a pagination boundary.
  for (let offset = 0; offset <= 100000; offset += 100) {
    params.set('offset', offset);
    const next = await api('/slots?' + params);
    list.push(...next);
    if (state.view !== 'slots' || request !== scheduleRequest) return;
    if (next.length < 100) break;
    if (offset === 100000) throw new Error('Слишком много слотов в этот день.');
  }
  if (state.view !== 'slots' || request !== scheduleRequest) return;
  const hw = select(
    state.hw.map((v) => [v.id, v.title]),
    homeworkID,
    async (value) => {
      state.homeworkID = value;
      state.slotDay = '';
      await safeRender();
    },
  );
  const container = h('div');
  const calendar = scheduleCalendar({
    month,
    selected: [selectedDay],
    events,
    onMonth: (value) => {
      state.slotMonth = value;
      state.slotDay = '';
      return safeRender();
    },
    onDay: (value) => {
      state.slotDay = value;
      state.slotMonth = value.slice(0, 7);
      return safeRender();
    },
  });
  $('content').replaceChildren(
    h(
      'div',
      { class: 'row between' },
      h('h2', {}, 'Расписание защит'),
      h('span', { class: 'muted small' }, 'Время по Москве'),
    ),
    field('Домашнее задание', hw),
    calendar,
    h('h3', { class: 'date-heading', 'aria-live': 'polite' }, date(selectedDay + 'T12:00:00+03:00')),
    container,
  );
  if (list.length)
    container.append(
      dayTimeline(selectedDay, list, slotModal),
      button(
        'К выбору дня',
        () => {
          calendar.querySelector('[aria-pressed="true"]')?.focus({ preventScroll: true });
          calendar.scrollIntoView({
            behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth',
            block: 'start',
          });
        },
        'wide',
      ),
    );
  else
    container.append(
      empty(
        'В этот день нет свободных слотов',
        events.length
          ? 'Выберите другой день в календаре или другое ДЗ.'
          : 'В этом месяце пока нет доступного времени. Переключите месяц или выберите другое ДЗ.',
      ),
    );
}
function bookingFailure(error) {
  const rejected = [400, 403, 404, 409, 422].includes(error.status);
  const dismiss = button(
    'Понятно',
    async () => {
      closeModal();
      await safeRender();
    },
    'primary',
  );
  modal(
    h(
      'div',
      { class: 'booking-error' },
      h(
        'h2',
        { id: 'booking-error-title' },
        rejected ? 'Не удалось записаться' : 'Не удалось подтвердить запись',
      ),
      h('p', { id: 'booking-error-reason', class: 'error' }, error.message),
      h(
        'p',
        { class: 'muted' },
        rejected
          ? 'Новая запись не создана. Ваши существующие записи не изменились.'
          : 'Проверьте «Мои записи» перед повторной попыткой: запрос мог успеть выполниться.',
      ),
      h(
        'div',
        { class: 'actions' },
        button('Мои записи', async () => {
          closeModal();
          await navigate('bookings');
        }),
        dismiss,
      ),
    ),
  );
  $('modal').setAttribute('role', 'alertdialog');
  $('modal').setAttribute('aria-labelledby', 'booking-error-title');
  $('modal').setAttribute('aria-describedby', 'booking-error-reason');
  dismiss.focus();
  tg?.HapticFeedback?.notificationOccurred?.('error');
}
async function slotModal(id) {
  const item = await api('/slots/' + id);
  const available =
    item.status === 'open' &&
    item.window_status === 'published' &&
    !item.booked &&
    new Date(item.starts_at) > new Date();
  modal(
    h(
      'div',
      {},
      h('span', { class: 'tag' }, item.homework_title),
      h('h2', { class: 'slot-title', tabindex: -1, autofocus: true }, date(item.starts_at)),
      h('p', { class: 'time' }, time(item.starts_at) + '–' + time(item.ends_at) + ' МСК'),
      h(
        'p',
        {},
        item.assistant_username
          ? h(
              'a',
              {
                href: 'https://t.me/' + item.assistant_username,
                target: '_blank',
                rel: 'noopener noreferrer',
              },
              item.assistant_name,
            )
          : item.assistant_name,
      ),
      h(
        'div',
        { class: 'actions' },
        button('Закрыть', closeModal),
        available
          ? button(
              'Записаться',
              async () => {
                try {
                  await post('/slots/' + id + '/bookings');
                } catch (e) {
                  bookingFailure(e);
                  return;
                }
                closeModal();
                await navigate('bookings');
                notify('Вы записаны на защиту');
              },
              'primary',
            )
          : h('span', { class: 'muted' }, 'Слот уже недоступен'),
      ),
    ),
  );
  if (!available && state.view === 'slots') await safeRender();
}
async function bookingsView() {
  const list = await api('/me/bookings');
  if (state.view !== 'bookings') return;
  const container = h('div');
  $('content').replaceChildren(container);
  const status = {
    confirmed: 'Вы записаны',
    cancelled_by_student: 'Вы отменили запись',
    cancelled_by_assistant: 'Отменено ассистентом',
  };
  function append(items) {
    for (const item of items) {
      const future = new Date(item.starts_at) > new Date();
      container.append(
        h(
          'section',
          { class: 'card' },
          h(
            'div',
            { class: 'row between' },
            h(
              'span',
              { class: 'tag' + (item.status === 'confirmed' ? '' : ' cancelled') },
              item.status === 'confirmed' && !future ? 'Прошедшая запись' : status[item.status],
            ),
            h('span', { class: 'muted small' }, item.homework_title),
          ),
          h('h2', {}, date(item.starts_at)),
          h('div', { class: 'time' }, time(item.starts_at) + '–' + time(item.ends_at)),
          h('p', { class: 'muted' }, item.assistant_name),
          item.cancellation_reason
            ? h('p', { class: 'muted' }, 'Причина: ' + item.cancellation_reason)
            : null,
          item.status === 'confirmed' && future
            ? h(
                'div',
                { class: 'actions' },
                button(
                  'Отменить запись',
                  async () => {
                    if (
                      (await confirm(
                        'Отменить запись?',
                        'Слот станет доступен другим студентам. Новое время нужно будет выбрать отдельно.',
                      )) === null
                    )
                      return;
                    await post('/bookings/' + item.id + '/cancel');
                    await render();
                    notify('Запись отменена');
                  },
                  'danger',
                ),
              )
            : null,
        ),
      );
    }
  }
  append(list);
  if (!list.length) {
    const prompt = empty('Вы пока не записаны', 'Выберите удобное время для защиты.');
    prompt.append(button('Выбрать слот', () => navigate('slots'), 'primary'));
    container.append(prompt);
  }
  let offset = list.length;
  const more = button(
    'Показать историю дальше',
    async () => {
      const next = await api('/me/bookings?offset=' + offset);
      append(next);
      offset += next.length;
      more.hidden = next.length < 100;
    },
    'wide',
  );
  more.hidden = list.length < 100;
  container.append(more);
}
async function windowsView() {
  const request = ++scheduleRequest;
  const windows = await api('/assistant/windows');
  if (state.view !== 'windows' || request !== scheduleRequest) return;
  const list = h('section', { class: 'card window-list' });
  for (const item of windows) {
    const entry = button('', () => windowModal(item.id), 'wide window-list-item');
    entry.dataset.windowId = item.id;
    entry.append(
      h('strong', {}, date(item.starts_at) + ' · ' + time(item.starts_at) + '–' + time(item.ends_at)),
      h(
        'span',
        { class: 'muted small' },
        item.homework_title +
          ' · ' +
          (item.status === 'cancelled' ? 'Отменено' : item.booking_count + '/' + item.slot_count + ' занято'),
      ),
    );
    list.append(entry);
  }
  if (!windows.length) list.append(h('p', { class: 'muted' }, 'Нет созданных окон'));
  $('content').replaceChildren(list);
}
async function windowModal(id) {
  const item = await api('/assistant/windows/' + id);
  const comment = h('textarea', { value: item.comment || '' });
  const commentStatus = h('p', { class: 'muted small', role: 'status' });
  const saveComment = button('Сохранить комментарий', async () => {
    const saved = await api('/assistant/windows/' + id + '/comment', {
      method: 'PATCH',
      body: JSON.stringify({ comment: comment.value }),
    });
    $('modal-content').querySelector('.error')?.remove();
    comment.value = saved.comment;
    commentStatus.textContent = 'Комментарий сохранён';
  });
  comment.addEventListener('input', () => {
    commentStatus.textContent = '';
  });
  const entries = item.slots.map((slot) =>
    h(
      'div',
      { class: 'subcard slot-card' },
      h(
        'div',
        { class: 'row between' },
        h('strong', {}, time(slot.starts_at) + '–' + time(slot.ends_at)),
        h(
          'span',
          { class: 'tag' + (slot.status === 'cancelled' ? ' cancelled' : '') },
          slot.status === 'cancelled' ? 'Отменён' : slot.booking_id ? 'Занят' : 'Свободен',
        ),
      ),
      slot.full_name ? h('p', { class: 'slot-name' }, slot.full_name) : null,
      h(
        'div',
        { class: 'slot-actions' },
        slot.repository_url
          ? h(
              'a',
              {
                class: 'tag repository-link',
                title: slot.repository_username,
                'aria-label': 'Репозиторий: ' + slot.repository_username,
                href: slot.repository_url,
                target: '_blank',
                rel: 'noopener noreferrer',
              },
              'Репозиторий',
            )
          : null,
        slot.username
          ? h(
              'a',
              {
                class: 'tag',
                href: 'https://t.me/' + slot.username,
                target: '_blank',
                rel: 'noopener noreferrer',
                title: '@' + slot.username,
                'aria-label': '@' + slot.username,
              },
              'Telegram',
            )
          : null,
        slot.status === 'open' && new Date(slot.starts_at) > new Date()
          ? button(
              'Отменить',
              async () => {
                const reason = await confirm(
                  'Отменить слот?',
                  slot.booking_id
                    ? 'Записанный студент получит уведомление.'
                    : 'Студенты больше не смогут выбрать это время.',
                  true,
                );
                if (reason === null) return;
                await post('/assistant/slots/' + slot.id + '/cancel', { reason });
                await windowModal(id);
              },
              'tag danger',
            )
          : null,
      ),
    ),
  );
  modal(
    h(
      'div',
      { class: 'window-details' },
      h('h2', {}, date(item.starts_at)),
      h(
        'p',
        { class: 'muted' },
        item.homework_title + ' · ' + time(item.starts_at) + '–' + time(item.ends_at) + ' МСК',
      ),
      windowCommentField(comment),
      saveComment,
      commentStatus,
      ...entries,
      h(
        'div',
        { class: 'actions' },
        button('Закрыть', () => {
          closeModal();
          return render();
        }),
        item.status === 'published' && new Date(item.starts_at) > new Date()
          ? button(
              'Отменить всё окно',
              async () => {
                const reason = await confirm(
                  'Отменить всё окно?',
                  'Будет отменено записей: ' + item.booking_count + '.',
                  true,
                );
                if (reason === null) return;
                await post('/assistant/windows/' + id + '/cancel', { reason });
                closeModal();
                await render();
                notify('Окно отменено');
              },
              'danger',
            )
          : null,
      ),
    ),
  );
}
async function safeRender() {
  try {
    await render();
  } catch (e) {
    notify(e.message, true);
  }
}
async function boot() {
  tg?.ready();
  tg?.expand();
  try {
    let me;
    if (tg?.initData) {
      const session = await post('/auth/telegram', { init_data: tg.initData });
      state.csrf = session.csrf_token;
    }
    try {
      me = await api('/me');
    } catch (e) {
      if (e.status !== 401) throw e;
      if (!tg?.initData) return landing();
      const session = await post('/auth/telegram', { init_data: tg.initData });
      state.csrf = session.csrf_token;
      me = await api('/me');
    }
    state.user = me.user;
    state.csrf = me.csrf_token;
    await loadHomeworks();
    state.role = initialRole();
    await navigate(needsName() ? 'profile' : homeView());
  } catch (e) {
    $('content').replaceChildren(
      empty('Не удалось открыть расписание', e.message),
      button('Попробовать снова', boot, 'primary'),
    );
  }
}
async function landing() {
  const config = await api('/public/config');
  const intro = h(
    'section',
    { class: 'card' },
    h('div', { class: 'step' }, 'Защиты домашних заданий'),
    h('h2', {}, 'Запишитесь через Telegram'),
    h(
      'p',
      { class: 'muted' },
      'Откройте приложение из бота: мы узнаем вас и покажем свободные слоты. Отдельный пароль не нужен.',
    ),
    config.bot_username
      ? h(
          'a',
          {
            href: 'https://t.me/' + config.bot_username + '?start=app',
            class: 'primary',
            style: 'display:inline-block;padding:13px 20px;border-radius:12px;text-decoration:none',
          },
          'Открыть бота ↗',
        )
      : h('p', { class: 'muted' }, 'Бот готовится к запуску. Ссылка скоро появится здесь.'),
  );
  $('content').replaceChildren(
    intro,
    h(
      'div',
      { class: 'landing-grid' },
      h(
        'section',
        { class: 'card' },
        h('span', { class: 'tag' }, 'Студентам'),
        h('h2', {}, 'Выбрать. Записаться.'),
        h(
          'p',
          {},
          'Все свободные часы ассистентов в одном расписании. Ваша запись закрепляется за вами, а планы можно изменить.',
        ),
      ),
      h(
        'section',
        { class: 'card' },
        h('span', { class: 'tag' }, 'Ассистентам'),
        h('h2', {}, 'Ваш день, ваше время.'),
        h(
          'p',
          {},
          'Выберите день, укажите начало и конец окна и опубликуйте. Приложение само нарежет время на слоты для защит.',
        ),
      ),
    ),
  );
}
$('refresh').addEventListener('click', (e) =>
  action(e.currentTarget, async () => {
    if (!state.user) return boot();
    const me = await api('/me');
    state.user = me.user;
    state.csrf = me.csrf_token;
    await loadHomeworks();
    navigation();
    await render();
  }),
);
document.addEventListener('visibilitychange', () => {
  if (!document.hidden && state.user && !['create', 'profile'].includes(state.view)) safeRender();
});
boot();
