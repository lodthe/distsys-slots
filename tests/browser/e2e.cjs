const {chromium} = require('playwright');
const crypto = require('node:crypto');
const assert = require('node:assert/strict');
const backendURL = process.env.E2E_BACKEND_URL || 'http://127.0.0.1:8084';

function initData(id, firstName) {
  const values = new URLSearchParams({auth_date: String(Math.floor(Date.now() / 1000)), user: JSON.stringify({id, first_name: firstName, username: 'testuser' + id}), query_id: 'isolated-e2e-' + id});
  const data = [...values].sort(([a], [b]) => a.localeCompare(b)).map(([k, v]) => k + '=' + v).join('\n');
  const secret = crypto.createHmac('sha256', 'WebAppData').update('e2e-test-token').digest();
  values.set('hash', crypto.createHmac('sha256', secret).update(data).digest('hex')); return values.toString();
}
(async () => {
  const browser = await chromium.launch({executablePath: '/usr/bin/chromium-browser', args: ['--no-sandbox', '--disable-dev-shm-usage']});
  const errors = [];
  async function user(id, name) {
    const context = await browser.newContext({viewport: {width: 390, height: 844}});
    await context.addInitScript(data => {window.Telegram = {WebApp: {initData: data, ready() {}, expand() {}}};}, initData(id, name));
    await context.route('https://telegram.org/**', route => route.fulfill({status: 200, contentType: 'text/javascript', body: ''}));
    await context.route('https://distsys-e2e.local/**', async route => {
      const incoming = new URL(route.request().url());
      const response = await route.fetch({url: backendURL + incoming.pathname + incoming.search, headers: {...route.request().headers(), origin: 'https://distsys-e2e.local'}});
      await route.fulfill({response});
    });
    const page = await context.newPage(); page.on('pageerror', error => errors.push(error.message));
    await page.goto('https://distsys-e2e.local/', {waitUntil: 'domcontentloaded'});
    const staff = id === 303 || id === 101;
    if (staff) {
      await (id === 303 ? page.getByRole('heading', {name: 'ДЗ и даты защит', exact: true}) : page.locator('.window-list')).waitFor();
      assert.equal(await page.getByLabel('Ваше имя', {exact: true}).count(), 0);
      assert.equal(await page.getByRole('button', {name: 'Профиль', exact: true}).count(), 0);
    } else {
      await page.getByLabel('ФИО', {exact: true}).fill(name); await page.getByLabel('Юзернейм репозитория', {exact: true}).fill('student_' + id);
      await page.getByRole('button', {name: 'Перейти к слотам', exact: true}).click();
      await page.getByRole('heading', {name: 'Расписание защит', exact: true}).waitFor();
    }
    await page.reload({waitUntil: 'domcontentloaded'});
    await (id === 101 ? page.locator('.window-list') : page.getByRole('heading', {name: id === 303 ? 'ДЗ и даты защит' : 'Расписание защит', exact: true})).waitFor();
    return page;
  }
  const today = new Intl.DateTimeFormat('en-CA', {timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit', day: '2-digit'}).format(new Date());
  const tomorrow = new Date(today + 'T12:00:00+03:00'); tomorrow.setUTCDate(tomorrow.getUTCDate() + 1);
  const day = new Intl.DateTimeFormat('en-CA', {timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit', day: '2-digit'}).format(tomorrow);
  const admin = await user(303, 'Администратор Проверочный');
  assert.deepEqual(await admin.getByRole('tab').allTextContents(), ['Админ', 'Ассистент', 'Студент']);
  await admin.getByRole('tab', {name: 'Ассистент', exact: true}).click();
  await admin.locator('.window-list').waitFor();
  await admin.getByRole('tab', {name: 'Студент', exact: true}).click();
  await admin.getByRole('heading', {name: 'Как вас записать?', exact: true}).waitFor();
  await admin.getByRole('tab', {name: 'Студент', exact: true}).press('Home');
  await admin.getByRole('heading', {name: 'ДЗ и даты защит', exact: true}).waitFor();
  assert.equal(await admin.getByRole('tab', {name: 'Админ', exact: true}).getAttribute('aria-selected'), 'true');
  await admin.getByRole('button', {name: '+ Добавить ДЗ', exact: true}).click();
  await admin.getByLabel('Номер ДЗ', {exact: true}).fill('1');
  await admin.locator('dialog [data-day="' + day + '"]').click();
  await admin.getByRole('button', {name: 'Сохранить ДЗ', exact: true}).click();
  await admin.getByText('ДЗ сохранено', {exact: true}).waitFor();
  const assistant = await user(101, 'Ассистент Проверочный');
  assert.deepEqual(await assistant.getByRole('tab').allTextContents(), ['Ассистент', 'Студент']);
  await assistant.getByRole('tab', {name: 'Ассистент', exact: true}).click();
  await assistant.locator('nav').getByRole('button', {name: 'Добавить окно', exact: true}).click();
  await assistant.locator('.month-calendar [data-day="' + day + '"]').click();
  await assistant.getByLabel('Начало: минуты', {exact: true}).selectOption('07');
  assert.equal(await assistant.getByLabel('Конец: минуты', {exact: true}).inputValue(), '30', 'changing start must preserve end');
  await assistant.getByLabel('Конец: минуты', {exact: true}).selectOption('37');
  await assistant.getByLabel('Комментарий к защите (необязательно)', {exact: true}).fill('https://meet.example/defense');
  assert.equal(await assistant.locator('.review-table tbody tr').count(), 11);
  await assistant.getByRole('button', {name: 'Удалить слот 18:15–18:23', exact: true}).click();
  assert.equal(await assistant.locator('.review-table tbody tr').count(), 10);
  await assistant.getByRole('button', {name: 'Опубликовать', exact: true}).click();
  await assistant.getByText('Окно опубликовано', {exact: true}).waitFor();
  const student = await user(202, 'Студент Проверочный');
  assert.deepEqual(await student.getByRole('tab').allTextContents(), []);
  assert.equal(await student.locator('#role-switch').isVisible(), false);
  async function calendarCount(count) {
    await student.locator('nav').getByRole('button', {name: 'Расписание', exact: true}).click();
    // Tomorrow may be in the following month.
    if (today.slice(0, 7) !== day.slice(0, 7)) await student.getByRole('button', {name: 'Следующий месяц', exact: true}).click();
    await student.locator('[data-day="' + day + '"].available').waitFor();
    await student.locator('[data-day="' + day + '"] .day-count').getByText(count + ' своб.', {exact: true}).waitFor();
  }
  await calendarCount(10);
  assert.equal(await student.getByRole('button', {name: /^18:15–18:23/}).count(), 0);
  assert(await student.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'calendar overflows mobile width');
  await student.getByRole('button', {name: 'Следующий месяц', exact: true}).click();
  await student.getByText('В этот день нет свободных слотов', {exact: true}).waitFor();
  await student.getByRole('button', {name: 'Предыдущий месяц', exact: true}).click();
  await student.locator('[data-day="' + day + '"].available').waitFor();
  async function book() {
    await student.locator('nav').getByRole('button', {name: 'Расписание', exact: true}).click();
    await student.getByRole('button', {name: /^18:07–18:15/}).click();
    await student.getByRole('button', {name: 'Записаться', exact: true}).click();
    await student.getByText('Вы записаны на защиту', {exact: true}).waitFor();
  }
  await book();
  await student.locator('nav').getByRole('button', {name: 'Расписание', exact: true}).click();
  await student.getByRole('button', {name: /^18:23–18:31/}).click();
  await student.getByRole('button', {name: 'Записаться', exact: true}).click();
  await student.getByRole('alertdialog', {name: 'Не удалось записаться', exact: true}).waitFor();
  await student.getByText('Новая запись не создана. Ваши существующие записи не изменились.', {exact: true}).waitFor();
  await student.getByRole('alertdialog').getByRole('button', {name: 'Мои записи', exact: true}).click();
  await student.locator('#content').getByText('Вы записаны', {exact: true}).waitFor();
  assert.equal(await student.locator('#content .card').count(), 1);
  assert(!/МСК|по Москве/i.test(await student.locator('body').innerText()), 'timezone label in my bookings');
  await calendarCount(9);
  // Changing allowed dates must leave already published windows and bookings intact.
  const nextDate = new Date(tomorrow); nextDate.setUTCDate(nextDate.getUTCDate() + 1);
  const replacementDay = new Intl.DateTimeFormat('en-CA', {timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit', day: '2-digit'}).format(nextDate);
  await admin.getByRole('button', {name: 'Изменить ДЗ', exact: true}).click();
  await admin.locator('dialog [data-day="' + day + '"]').click();
  if (replacementDay.slice(0,7) !== day.slice(0,7)) await admin.locator('dialog').getByRole('button', {name: 'Следующий месяц', exact: true}).click();
  await admin.locator('dialog [data-day="' + replacementDay + '"]').click();
  await admin.getByRole('button', {name: 'Сохранить ДЗ', exact: true}).click();
  await admin.getByText('ДЗ сохранено', {exact: true}).waitFor();
  await calendarCount(9);
  await student.locator('nav').getByRole('button', {name: 'Мои записи', exact: true}).click();
  await student.getByRole('button', {name: 'Отменить запись', exact: true}).click();
  await student.getByRole('button', {name: 'Подтвердить', exact: true}).click();
  await student.getByText('Вы отменили запись', {exact: true}).waitFor();
  await calendarCount(10);
  await book();
  await assistant.getByRole('button', {name: 'Обновить расписание', exact: true}).click();
  await assistant.locator('.window-list-item').first().click();
  await assistant.getByText('Студент Проверочный', {exact: true}).waitFor();
  await assistant.getByRole('link', {name: '@testuser202', exact: true}).waitFor();
  assert.equal(await assistant.getByRole('link', {name: 'Репозиторий: student_202', exact: true}).getAttribute('href'), 'https://distsys.ru/hse-2026/student_202');
  await assistant.getByRole('button', {name: 'Отменить всё окно', exact: true}).click();
  await assistant.getByLabel('Причина отмены (необязательно)', {exact: true}).fill('Проверка отмены окна');
  await assistant.getByRole('button', {name: 'Подтвердить', exact: true}).click();
  await assistant.getByText('Окно отменено', {exact: true}).waitFor();
  await student.getByRole('button', {name: 'Обновить расписание', exact: true}).click();
  await student.getByText('Отменено ассистентом', {exact: true}).waitFor();
  await student.getByText('Причина: Проверка отмены окна', {exact: true}).waitFor();
  await student.locator('nav').getByRole('button', {name: 'Расписание', exact: true}).click();
  await student.getByText('В этот день нет свободных слотов', {exact: true}).waitFor();
  assert.equal(await student.locator('.month-day.available').count(), 0);
  assert.deepEqual(errors, []);
  console.log('PASS: isolated real Go/PostgreSQL + Chromium: three roles, one homework number and dates, monthly calendar on mobile, accurate availability after booking/cancellation, assistant role assignment, name and repository profiles, assistant publication at 18:07, student booking/cancellation/rebooking, participant details, assistant cancellation and student history. No production data used.');
  await browser.close();
})().catch(error => {console.error(error); process.exit(1);});
