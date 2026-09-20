const tg = window.Telegram?.WebApp;
const $ = (id) => document.getElementById(id);
function h(tag, props = {}, ...children) {
  const el = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key.startsWith('on')) el.addEventListener(key.slice(2), value);
    else if (key === 'class') el.className = value;
    else if (key === 'text') el.textContent = value;
    else if (key === 'value') el.value = value;
    else if (value !== false && value != null) el.setAttribute(key, value === true ? '' : value);
  }
  for (const child of children.flat(Infinity))
    if (child != null) el.append(child instanceof Node ? child : document.createTextNode(String(child)));
  return el;
}
const state = { user: null, csrf: '', hw: [], role: 'student', view: 'slots', homeworkID: '', draft: null };
const button = (text, fn, cls = '') =>
  h('button', { type: 'button', class: cls, onclick: (event) => action(event.currentTarget, fn) }, text);
const field = (name, input) => h('label', {}, name, input);
const empty = (title, text) =>
  h(
    'section',
    { class: 'card empty' },
    h('div', { class: 'symbol', 'aria-hidden': 'true' }, '◷'),
    h('h2', {}, title),
    h('p', {}, text),
  );
function notify(message, error = false) {
  $('notice').replaceChildren(h('div', { class: error ? 'error' : 'success' }, message));
  if (error && $('modal').open) {
    $('modal-content').querySelector('.error')?.remove();
    $('modal-content').prepend(h('div', { class: 'error', role: 'alert' }, message));
  }
}
async function action(el, fn) {
  el.disabled = true;
  try {
    await fn();
  } catch (e) {
    notify(e.message || 'Не удалось выполнить действие', true);
  } finally {
    el.disabled = false;
  }
}
async function api(path, options = {}) {
  let response;
  try {
    response = await fetch('/api/v1' + path, {
      ...options,
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': state.csrf, ...options.headers },
    });
  } catch {
    throw new Error('Нет соединения. Проверьте сеть и повторите действие.');
  }
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    const err = new Error(data.message || 'Не удалось загрузить данные');
    err.status = response.status;
    throw err;
  }
  return data;
}
const post = (path, value = {}, headers = {}) =>
  api(path, { method: 'POST', body: JSON.stringify(value), headers });
function select(options, value, change) {
  const el = h(
    'select',
    { onchange: (e) => change(e.target.value) },
    options.map(([v, text]) => h('option', { value: v }, text)),
  );
  el.value = String(value);
  return el;
}
function modal(content) {
  $('modal').setAttribute('role', 'dialog');
  $('modal').removeAttribute('aria-labelledby');
  $('modal').removeAttribute('aria-describedby');
  $('modal-content').replaceChildren(content);
  if (!$('modal').open) $('modal').showModal();
}
function closeModal() {
  $('modal').close();
}
function confirm(title, description, needsReason = false) {
  return new Promise((resolve) => {
    const input = h('textarea', { maxlength: 2000, placeholder: 'Например: изменилось расписание' });
    const finish = (value) => {
      $('modal').removeEventListener('cancel', cancel);
      closeModal();
      resolve(value);
    };
    const cancel = () => finish(null);
    $('modal').addEventListener('cancel', cancel, { once: true });
    modal(
      h(
        'div',
        {},
        h('h2', {}, title),
        h('p', { class: 'muted' }, description),
        needsReason ? field('Причина отмены (необязательно)', input) : null,
        h(
          'div',
          { class: 'actions' },
          button('Назад', cancel),
          button(
            'Подтвердить',
            () => {
              finish(input.value.trim());
            },
            'primary',
          ),
        ),
      ),
    );
  });
}
function timePicker(value, changed, label) {
  let [hh, mm] = value.split(':');
  const hours = select(
    Array.from({ length: 24 }, (_, n) => [String(n).padStart(2, '0'), String(n).padStart(2, '0')]),
    hh,
    (v) => {
      hh = v;
      changed(hh + ':' + mm);
    },
  );
  hours.setAttribute('aria-label', label + ': часы');
  const mins = select(
    Array.from({ length: 60 }, (_, n) => [String(n).padStart(2, '0'), String(n).padStart(2, '0')]),
    mm,
    (v) => {
      mm = v;
      changed(hh + ':' + mm);
    },
  );
  mins.setAttribute('aria-label', label + ': минуты');
  return h('div', { class: 'time-select' }, hours, h('span', {}, ':'), mins);
}

export {
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
  timePicker,
};
