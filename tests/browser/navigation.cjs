const {launch, mockApp} = require('./fixture.cjs');
const assert = require('node:assert/strict');

(async () => {
  const browser = await launch();
  const errors = [];
  for (const [role, profile] of [['student', 'empty'], ['student', 'complete'], ['assistant', 'empty'], ['admin', 'empty']]) {
    let user = {id: 1, assistant: role === 'assistant', admin: role === 'admin',
      full_name: profile === 'complete' ? 'Иван Иванов' : '',
      repository_username: profile === 'complete' ? 'ivanov' : ''};
    let failSave = role === 'student', saves = 0;
    const context = await browser.newContext({viewport: {width: 390, height: 844}});
    await mockApp(context, 'https://navigation.test', async route => {
      const request = route.request(), url = new URL(request.url());
      let body = [];
      if (url.pathname === '/api/v1/me') {
        if (request.method() === 'PATCH') {
          saves++;
          if (failSave) {failSave = false; return route.fulfill({status: 503, json: {message: 'Не удалось сохранить имя. Попробуйте ещё раз.'}});}
          const payload = request.postDataJSON();
          assert.equal(request.headers()['x-csrf-token'], 'test');
          user = {...user, ...payload};
        }
        body = {user, csrf_token: 'test'};
      }
      await route.fulfill({json: body});
    });
    const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message));
    await page.goto('https://navigation.test', {waitUntil: 'domcontentloaded'});
    if (role !== 'student') {
      await (role === 'admin' ? page.getByRole('heading', {name: 'ДЗ и даты защит', exact: true}) : page.locator('.window-list')).waitFor();
      if (role === 'admin') {
        assert.equal(await page.locator('nav').isVisible(), false);
        assert.equal(await page.locator('nav button').count(), 0);
      }
      assert.equal(await page.locator('input').count(), 0, 'staff should not fill in a name');
      assert.equal(await page.getByRole('button', {name: 'Профиль', exact: true}).count(), 0);
      assert.equal(await page.getByRole('button', {name: 'Ассистенты', exact: true}).count(), 0);
      assert.equal(saves, 0);
      await page.getByRole('tab', {name: 'Студент', exact: true}).click();
    }
    if (profile !== 'complete') {
      await page.getByRole('heading', {name: 'Как вас записать?', exact: true}).waitFor();
      assert.equal(await page.locator('nav').isVisible(), false);
      assert.equal(await page.locator('#refresh').isVisible(), false);
      assert.equal(await page.getByLabel('ФИО', {exact: true}).inputValue(), user.full_name);
      await page.getByLabel('ФИО', {exact: true}).fill(' Иван Иванов ');
      await page.getByLabel('Юзернейм репозитория', {exact: true}).fill('ivanov');
      await page.getByRole('button', {name: 'Перейти к слотам', exact: true}).click();
      if (role === 'student') {
        await page.getByText('Не удалось сохранить имя. Попробуйте ещё раз.', {exact: true}).waitFor();
        assert.equal(await page.getByLabel('ФИО', {exact: true}).inputValue(), ' Иван Иванов ');
        await page.getByRole('button', {name: 'Перейти к слотам', exact: true}).click();
      }
    }
    await page.getByRole('heading', {name: 'Расписание защит', exact: true}).waitFor();
    assert.equal(await page.locator('#role-switch').isVisible(), role !== 'student');
    if (role !== 'student') {
      assert.equal(await page.getByRole('tab', {name: 'Студент', exact: true}).getAttribute('aria-selected'), 'true');
      await page.getByRole('tab', {name: role === 'admin' ? 'Админ' : 'Ассистент', exact: true}).click();
      await (role === 'admin' ? page.getByRole('heading', {name: 'ДЗ и даты защит', exact: true}) : page.locator('.window-list')).waitFor();
    }
    if (role === 'student' && profile === 'complete') {
      await page.getByRole('button', {name: 'Мои записи', exact: true}).click();
      await page.getByRole('button', {name: 'Выбрать слот', exact: true}).click();
      await page.getByRole('heading', {name: 'Расписание защит', exact: true}).waitFor();
      await page.getByRole('button', {name: 'Профиль', exact: true}).click();
      await page.getByLabel('ФИО', {exact: true}).fill('Несохранённое');
      await page.getByRole('button', {name: 'Назад', exact: true}).click();
    }
    assert.equal(user.full_name, 'Иван Иванов');
    assert.equal(user.repository_username, 'ivanov');
    assert.equal(saves, profile === 'complete' ? 0 : role === 'student' ? 2 : 1);
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile overflow');
    await context.close();
  }
  assert.deepEqual(errors, []);
  await browser.close();
  console.log('PASS: student onboarding and returning profile, staff entry and role switching, failed save retry, profile back without saving, empty bookings CTA.');
})().catch(e => {console.error(e); process.exit(1);});
