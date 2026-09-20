const {launch, mockApp} = require('./fixture.cjs');
const assert = require('node:assert/strict');

(async () => {
  const browser = await launch();
  const context = await browser.newContext({viewport: {width: 390, height: 844}, hasTouch: true});
  const base = 'https://calendar.test';
  const page = await context.newPage(), errors = [];
  page.on('pageerror', e => errors.push(e.message));
  const isoDay = value => new Intl.DateTimeFormat('en-CA', {timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit', day: '2-digit'}).format(value);
  const date = new Date(isoDay(new Date()) + 'T12:00:00+03:00'); date.setUTCDate(date.getUTCDate() + 1);
  const day = isoDay(date), start = new Date(day + 'T08:00:00+03:00').getTime();
  let slots = [], offsets = [], noHomeworks = false, calendarRequests = 0;
  function fixture(ranges) {
    return ranges.map(([a, b], index) => ({id: index + 1, window_id: index + 1, homework_id: 1, homework_title: 'ДЗ №1', assistant_id: index % 3 + 1,
      assistant_name: ['Иван Иванов', 'Мария Петрова', 'Анна Смирнова'][index % 3], starts_at: new Date(start + a * 60000).toISOString(), ends_at: new Date(start + b * 60000).toISOString(), status: 'open'}));
  }
  await mockApp(context, base, async route => {
    const url = new URL(route.request().url()), path = url.pathname.replace('/api/v1', '');
    assert.equal(url.searchParams.has('assistant_id'), false, 'assistant filter still sent');
    let body;
    if (path === '/me') body = {user: {id: 500, full_name: 'Проверочный Студент', repository_username: 'test_student'}, csrf_token: 'test'};
    else if (path === '/homeworks') body = noHomeworks ? [] : [{id: 1, number: 1, title: 'ДЗ №1', defense_dates: [day]}, {id: 2, number: 2, title: 'ДЗ №2', defense_dates: [day]}];
    else if (path === '/calendar') body = [{day, homework_id: 1, assistant_id: 1, assistant_name: 'Иван Иванов', total_slots: slots.length, free_slots: slots.length, first_at: slots[0]?.starts_at}];
    else if (path === '/slots') {const offset = Number(url.searchParams.get('offset') || 0); offsets.push(offset); body = slots.slice(offset, offset + 100);}
    else if (/^\/slots\/\d+$/.test(path)) body = {...slots.find(v => v.id === Number(path.split('/')[2])), window_status: 'published', booked: false};
    else throw new Error('Unexpected API ' + path);
    if (path === '/calendar' || path === '/slots') {
      assert(['1', '2'].includes(url.searchParams.get('homework_id')), 'schedule request requires a concrete homework');
      if (path === '/calendar') calendarRequests++;
      if (url.searchParams.get('homework_id') === '2') body = [];
    }
    await route.fulfill({status: 200, contentType: 'application/json', body: JSON.stringify(body)});
  });
  async function show(ranges) {
    slots = fixture(ranges); offsets = [];
    await page.goto(base, {waitUntil: 'domcontentloaded'});
    if (isoDay(new Date()).slice(0, 7) !== day.slice(0, 7)) await page.getByRole('button', {name: 'Следующий месяц', exact: true}).click();
    await page.locator('.timed-event').last().waitFor();
    assert.equal(await page.locator('.timed-event').count(), slots.length);
    assert.equal(await page.getByLabel('Ассистент', {exact: true}).count(), 0);
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'page overflow');
    const rects = await page.locator('.timed-event').evaluateAll(els => els.map(el => {
      const r = el.getBoundingClientRect(); return {id: Number(el.dataset.eventId), columns: Number(el.dataset.columns), column: Number(el.dataset.column), x: r.x, y: r.y, right: r.right, bottom: r.bottom, width: r.width, height: r.height};
    }));
    for (const a of rects) {
      assert(a.width >= 110 && a.height >= 44, 'slot is not readable/tappable');
      for (const b of rects) if (a.id !== b.id) assert(!(a.x < b.right && a.right > b.x && a.y < b.bottom && a.bottom > b.y), 'event rectangles overlap');
    }
    return rects;
  }
  let rects = await show([[0, 12], [2, 10], [4, 8], [12, 20]]);
  assert.deepEqual(rects.map(v => v.columns), [3, 3, 3, 1]);
  await page.setViewportSize({width: 1100, height: 1000});
  await page.emulateMedia({colorScheme: 'dark'});
  await page.locator('.timed-event').first().click();
  await page.getByRole('button', {name: 'Записаться', exact: true}).waitFor();
  await page.getByRole('button', {name: 'Закрыть', exact: true}).click();
  rects = await show(Array.from({length: 102}, (_, i) => [Math.floor(i / 3) * 8, Math.floor(i / 3) * 8 + 8]));
  assert(offsets.includes(100), 'second page not loaded');
  assert(rects.every(v => v.columns === 3));
  await page.setViewportSize({width: 390, height: 844});
  await page.evaluate(() => {
    const grid = document.querySelector('.time-scroll');
    window.scrollTo(0, grid.getBoundingClientRect().top + scrollY - 80);
  });
  // Swipe directly over the slots, without clicking or focusing the grid first.
  const beforeSwipe = await page.evaluate(() => scrollY);
  const touch = await context.newCDPSession(page);
  await touch.send('Input.dispatchTouchEvent', {type: 'touchStart', touchPoints: [{x: 200, y: 620}]});
  for (let y = 600; y >= 220; y -= 20) {
    await touch.send('Input.dispatchTouchEvent', {type: 'touchMove', touchPoints: [{x: 200, y}]});
    await page.waitForTimeout(16);
  }
  await touch.send('Input.dispatchTouchEvent', {type: 'touchEnd', touchPoints: []});
  await page.waitForFunction(before => scrollY > before + 100, beforeSwipe);
  assert.equal(await page.locator('dialog[open]').count(), 0, 'swiping must not open a slot');
  assert.equal(await page.locator('.time-scroll').evaluate(el => el.scrollHeight - el.clientHeight), 0, 'slots must not have a nested vertical scrollbar');
  const beforeHorizontalSwipe = await page.evaluate(() => scrollY);
  await touch.send('Input.dispatchTouchEvent', {type: 'touchStart', touchPoints: [{x: 300, y: 420}]});
  for (let x = 280; x >= 100; x -= 20) {
    await touch.send('Input.dispatchTouchEvent', {type: 'touchMove', touchPoints: [{x, y: 420}]});
    await page.waitForTimeout(16);
  }
  await touch.send('Input.dispatchTouchEvent', {type: 'touchEnd', touchPoints: []});
  await page.waitForFunction(() => document.querySelector('.time-scroll').scrollLeft > 0);
  assert(Math.abs(await page.evaluate(() => scrollY) - beforeHorizontalSwipe) < 50, 'horizontal swipe must stay in the grid');
  await touch.detach();
  const backToDay = page.getByRole('button', {name: 'К выбору дня', exact: true});
  const backBounds = await backToDay.boundingBox();
  const gridBounds = await page.locator('.time-scroll').boundingBox();
  assert(backBounds.y >= gridBounds.y + gridBounds.height, 'day navigation must stay below the grid without covering slots');
  const requestsBeforeReturn = calendarRequests;
  await backToDay.click();
  await page.waitForFunction(() => {
    const bounds = document.querySelector('.month-calendar').getBoundingClientRect();
    return bounds.top >= -1 && bounds.bottom <= innerHeight;
  });
  assert.equal(await page.locator('.month-grid [aria-pressed="true"]').getAttribute('data-day'), day);
  assert.equal(calendarRequests, requestsBeforeReturn, 'returning to the calendar should preserve the loaded schedule');
  const homework = page.getByRole('combobox');
  assert.deepEqual(await homework.locator('option').allTextContents(), ['ДЗ №1', 'ДЗ №2']);
  assert.equal(await homework.inputValue(), '1');
  await homework.selectOption('2');
  await page.getByRole('heading', {name: 'В этот день нет свободных слотов', exact: true}).waitFor();
  assert.equal(await page.locator('.timed-event').count(), 0);
  assert.equal(await homework.inputValue(), '2');
  await Promise.all([
    page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/calendar'),
    page.getByRole('button', {name: 'Следующий месяц', exact: true}).click(),
  ]);
  await page.locator('#content.loading').waitFor({state: 'hidden'});
  await page.getByRole('heading', {name: 'В этот день нет свободных слотов', exact: true}).waitFor();
  assert.equal(await homework.inputValue(), '2');
  noHomeworks = true;
  const previousRequests = calendarRequests;
  await page.reload({waitUntil: 'domcontentloaded'});
  await page.getByRole('heading', {name: 'ДЗ пока не добавлены', exact: true}).waitFor();
  assert.equal(calendarRequests, previousRequests, 'empty homework list must not request an unfiltered calendar');
  assert.equal(await page.getByRole('combobox').count(), 0);
  assert.deepEqual(errors, []);
  console.log('PASS: browser geometry: 3 concurrent columns, partial/nested intervals, adjacent full-width slot, touch scrolling without focus, horizontal swipes, desktop/dark, slot details, 102 slots across pages, return to day selection below the grid on mobile; no assistant filter. API mocked.');
  await browser.close();
})().catch(e => {console.error(e); process.exit(1);});
