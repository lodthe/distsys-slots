const {launch, mockApp} = require('./fixture.cjs');
const assert = require('node:assert/strict');
(async () => {
  const browser = await launch();
  const iso = d => new Intl.DateTimeFormat('en-CA', {timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit', day: '2-digit'}).format(d);
  const at = new Date(iso(new Date()) + 'T12:00:00+03:00'); at.setUTCDate(at.getUTCDate()+1); const day = iso(at);
  const slot = {id: 1, homework_id: 1, homework_title: 'ДЗ №1', assistant_name: '@assistant', starts_at: day+'T15:00:00Z', ends_at: day+'T15:08:00Z', status: 'open', window_status: 'published', booked: false};
  for (const scenario of [
    {status: 409, reason: 'Этот слот уже занят'},
    {status: 503, reason: 'Сервис временно недоступен'},
    {status: 0, reason: 'Нет соединения. Проверьте сеть и повторите действие.'},
  ]) {
    let attempts = 0;
    const context = await browser.newContext({viewport: {width: 390, height: 844}}), errors = [];
    await mockApp(context, 'https://feedback.test', async route => {
      const request = route.request(), url = new URL(request.url());
      let body = [];
      if (url.pathname.endsWith('/me')) body = {user: {id: 1, full_name: 'Иван Иванов', repository_username: 'ivanov'}, csrf_token: 'test'};
      if (url.pathname.endsWith('/homeworks')) body = [{id: 1, number: 1, title: 'ДЗ №1', defense_dates: [day]}];
      if (url.pathname.endsWith('/calendar')) body = [{day, homework_id: 1, assistant_id: 1, assistant_name: '@assistant', total_slots: 1, free_slots: 1, first_at: slot.starts_at}];
      if (url.pathname.endsWith('/slots')) body = [slot];
      if (url.pathname.endsWith('/slots/1')) body = slot;
      if (url.pathname.endsWith('/slots/1/bookings')) {
        attempts++;
        if (!scenario.status) return route.abort('failed');
        return route.fulfill({status: scenario.status, json: {message: scenario.reason}});
      }
      if (url.pathname.endsWith('/me/bookings')) body = [{...slot, id: 9, status: 'confirmed'}];
      await route.fulfill({json: body});
    });
    const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message));
    await page.goto('https://feedback.test', {waitUntil: 'domcontentloaded'});
    if (day.slice(0,7)!==iso(new Date()).slice(0,7)) await page.getByRole('button', {name:'Следующий месяц',exact:true}).click();
    await page.getByRole('button', {name: /^18:00–18:08/}).click();
    await page.getByRole('button', {name: 'Записаться', exact: true}).click();
    const rejected = scenario.status===409;
    const popup = page.getByRole('alertdialog', {name: rejected ? 'Не удалось записаться' : 'Не удалось подтвердить запись', exact: true});
    await popup.waitFor();
    await popup.getByText(scenario.reason, {exact: true}).waitFor();
    assert.equal(await popup.getByRole('button', {name: 'Понятно', exact: true}).evaluate(el => el === document.activeElement), true);
    assert.equal(await page.getByText('Вы записаны на защиту', {exact: true}).count(), 0);
    if (!rejected) assert.equal(await popup.getByText('Новая запись не создана.', {exact: false}).count(), 0);
    assert.equal(attempts, 1);
    await popup.getByRole('button', {name: 'Понятно', exact: true}).click();
    assert.equal(await page.locator('dialog').isVisible(), false);
    await page.getByRole('button', {name: /^18:00–18:08/}).click();
    await page.getByRole('dialog').waitFor();
    assert.equal(await page.getByRole('dialog').count(), 1, 'error dialog role must reset');
    await page.getByRole('button', {name: 'Записаться', exact: true}).click();
    await page.getByRole('alertdialog').getByRole('button', {name: 'Мои записи', exact: true}).click();
    await page.locator('#content').getByText('Вы записаны', {exact: true}).waitFor();
    await page.getByText('@assistant', {exact: true}).waitFor();
    assert(!/МСК|по Москве/i.test(await page.locator('body').innerText()));
    assert.deepEqual(errors, []);
    await context.close();
  }
  // A slot already booked when opened must refresh the grid while its card stays open.
  {
    let unavailable = false, calendarReads = 0, slotReads = 0;
    const context = await browser.newContext({viewport: {width: 390, height: 844}}), errors = [];
    await mockApp(context, 'https://stale-slot.test', async route => {
      assert.equal(route.request().method(), 'GET', 'opening an unavailable slot must not try to book');
      const path = new URL(route.request().url()).pathname;
      let body = [];
      if (path.endsWith('/me')) body = {user: {id: 1, full_name: 'Иван Иванов', repository_username: 'ivanov'}, csrf_token: 'test'};
      if (path.endsWith('/homeworks')) body = [{id: 1, number: 1, title: 'ДЗ №1', defense_dates: [day]}];
      if (path.endsWith('/calendar')) {
        calendarReads++;
        body = [{day, homework_id: 1, assistant_id: 1, assistant_name: '@assistant', total_slots: 1, free_slots: unavailable ? 0 : 1, first_at: slot.starts_at}];
      }
      if (path.endsWith('/slots')) {slotReads++; body = unavailable ? [] : [slot];}
      if (path.endsWith('/slots/1')) {unavailable = true; body = {...slot, booked: true};}
      await route.fulfill({json: body});
    });
    const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message));
    await page.goto('https://stale-slot.test', {waitUntil: 'domcontentloaded'});
    if (day.slice(0,7)!==iso(new Date()).slice(0,7)) await page.getByRole('button', {name:'Следующий месяц',exact:true}).click();
    await page.getByRole('button', {name: /^18:00–18:08/}).waitFor();
    const before = {calendarReads, slotReads};
    await page.getByRole('button', {name: /^18:00–18:08/}).click();
    await page.getByRole('dialog').getByText('Слот уже недоступен', {exact: true}).waitFor();
    await page.locator('#content').getByRole('heading', {name: 'В этот день нет свободных слотов', exact: true}).waitFor();
    assert.equal(await page.getByRole('dialog').getByRole('button', {name: 'Записаться', exact: true}).count(), 0);
    assert.equal(await page.locator('.timed-event').count(), 0);
    assert(calendarReads > before.calendarReads && slotReads > before.slotReads, 'calendar and slots must both refresh');
    await page.locator('[data-day="' + day + '"] .day-count').getByText('Нет мест', {exact: true}).waitFor();
    await page.getByRole('dialog').getByRole('button', {name: 'Закрыть', exact: true}).click();
    assert.equal(await page.locator('.timed-event').count(), 0);
    assert.deepEqual(errors, []);
    await context.close();
  }
  await browser.close();
  console.log('PASS: booking conflicts and uncertain errors show accessible popup, explicit outcome/reason, focus, dismissal, my bookings and no timezone label.');
})().catch(e => {console.error(e); process.exit(1);});
