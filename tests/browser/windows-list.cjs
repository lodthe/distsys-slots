const {launch, mockApp} = require('./fixture.cjs');
const assert = require('node:assert/strict');
(async () => {
  const browser = await launch();
  const context = await browser.newContext({viewport: {width: 390, height: 844}});
  const rows = Array.from({length: 102}, (_, i) => ({id: i + 1, homework_id: 1, homework_title: 'ДЗ №1',
    starts_at: new Date(Date.now() + (14 - i) * 86400000).toISOString(),
    ends_at: new Date(Date.now() + (14 - i) * 86400000 + 3600000).toISOString(),
    status: i === 101 ? 'cancelled' : 'published', booking_count: 0, slot_count: 7, slots: []}));
  let requests = 0, failSave = true;
  await mockApp(context, 'https://windows.test', async route => {
    const url = new URL(route.request().url());
    let body = [];
    if (url.pathname === '/api/v1/me') body = {user: {id: 1, assistant: true}, csrf_token: 'test'};
    if (url.pathname === '/api/v1/assistant/windows') {
      requests++;
      assert.equal(url.search, '', 'all windows must be loaded in one request');
      body = rows;
    }
    if (/\/assistant\/windows\/\d+$/.test(url.pathname)) body = rows.find(v => v.id === Number(url.pathname.split('/').at(-1)));
    if (/\/assistant\/windows\/\d+\/comment$/.test(url.pathname)) {
      assert.equal(route.request().method(), 'PATCH');
      if (failSave) { failSave = false; return route.fulfill({status: 500, json: {message: 'Не удалось сохранить комментарий'}}); }
      const row = rows.find(v => v.id === Number(url.pathname.split('/').at(-2)));
      row.comment = route.request().postDataJSON().comment.trim();
      body = {comment: row.comment};
    }
    await route.fulfill({json: body});
  });
  const page = await context.newPage(), errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.goto('https://windows.test', {waitUntil: 'domcontentloaded'});
  await page.locator('.window-list-item').last().waitFor();
  assert.equal(requests, 1);
  assert.equal(await page.locator('.month-calendar').count(), 0);
  assert.deepEqual(await page.locator('.window-list-item').evaluateAll(els => els.map(el => Number(el.dataset.windowId))), rows.map(v => v.id));
  await page.locator('.window-list-item').last().click();
  await page.locator('dialog').waitFor();
  const comment = page.locator('dialog').getByLabel('Комментарий к защите (необязательно)', {exact: true});
  await comment.fill('https://meet.example/defense');
  await page.getByRole('button', {name: 'Сохранить комментарий', exact: true}).click();
  await page.getByRole('alert').getByText('Не удалось сохранить комментарий', {exact: true}).waitFor();
  assert.equal(await comment.inputValue(), 'https://meet.example/defense');
  await page.getByRole('button', {name: 'Сохранить комментарий', exact: true}).click();
  await page.getByRole('status').getByText('Комментарий сохранён', {exact: true}).waitFor();
  await page.getByRole('button', {name: 'Закрыть', exact: true}).click();
  await page.locator('.window-list-item').last().click();
  assert.equal(await comment.inputValue(), 'https://meet.example/defense');
  await comment.fill('');
  await page.getByRole('button', {name: 'Сохранить комментарий', exact: true}).click();
  await page.getByRole('status').getByText('Комментарий сохранён', {exact: true}).waitFor();
  assert.equal(rows.at(-1).comment, '');
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
  assert.deepEqual(errors, []);
  await browser.close();
  console.log('PASS: all 102 windows in one request, newest first, including past/cancelled windows; no calendar.');
})().catch(e => {console.error(e); process.exit(1);});
