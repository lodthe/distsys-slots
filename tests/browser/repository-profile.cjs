const {launch, mockApp} = require('./fixture.cjs');
const assert = require('node:assert/strict');
(async () => {
  const browser = await launch();
  const context = await browser.newContext({viewport: {width:390,height:844}});
  let user={id:1,full_name:'Иванов Иван Отчество',repository_username:''}, saves=0;
  const at=new Date(Date.now()+86400000), start=at.toISOString(), end=new Date(at.getTime()+90*60000).toISOString();
  const window={id:1,homework_id:1,homework_title:'ДЗ №1',starts_at:start,ends_at:end,status:'published',booking_count:1,slot_count:1};
  await mockApp(context, 'https://repository.test', async route => {
      const request = route.request(), url = new URL(request.url());
      let body = [];
    if(url.pathname==='/api/v1/me') {
      if(route.request().method()==='PATCH') {saves++;user={...user,...route.request().postDataJSON()};}
      body={user,csrf_token:'test'};
    }
    if(url.pathname==='/api/v1/assistant/windows') body=[window];
    if(url.pathname==='/api/v1/assistant/windows/1') body={...window,slots:[{id:1,booking_id:1,status:'open',starts_at:start,ends_at:end,full_name:user.full_name,username:'student',repository_username:user.repository_username,repository_url:'https://distsys.ru/hse-2026/'+encodeURIComponent(user.repository_username)}]};
    await route.fulfill({json:body});
  });
  const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto('https://repository.test',{waitUntil:'domcontentloaded'});
  const name=page.getByLabel('ФИО',{exact:true}),repo=page.getByLabel('Юзернейм репозитория',{exact:true});
  await repo.waitFor();
  assert.equal(await page.locator('form input').count(),2);
  assert.equal(await name.inputValue(),'Иванов Иван Отчество','legacy full name should be preserved');
  assert.equal(await name.getAttribute('placeholder'),'ФИО');
  assert.equal(await repo.getAttribute('placeholder'),'ivanov_ivan_i');
  await page.getByText('https://distsys.ru/hse-2026/',{exact:true}).waitFor();
  for(const value of ['a b','a/b','a\\b','a\u00a0b','a\u0085b','a\ufeffb']) {
    await repo.fill(value); assert.equal(await repo.evaluate(el=>el.checkValidity()),false);
    await page.getByRole('button',{name:'Перейти к слотам',exact:true}).click();assert.equal(saves,0);
  }
  const username='иван.o-+@:%?#&=!"\'()[]{}<>🙂';
  await repo.fill(username);assert.equal(await repo.evaluate(el=>el.checkValidity()),true);
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'profile overflows mobile');
  await name.fill(' Иванов Иван Отчество ');
  await page.getByRole('button',{name:'Перейти к слотам',exact:true}).click();
  await page.getByRole('heading',{name:'Расписание защит',exact:true}).waitFor();
  assert.equal(saves,1);assert.equal(user.full_name,'Иванов Иван Отчество');assert.equal(user.repository_username,username);
  user.assistant=true;await page.reload({waitUntil:'domcontentloaded'});
  await page.locator('.window-list-item').click();
  const link=page.getByRole('link',{name:'Репозиторий: '+username,exact:true});
  assert.equal(await link.getAttribute('href'),'https://distsys.ru/hse-2026/'+encodeURIComponent(username));
  assert.equal(await link.getAttribute('target'),'_blank');
  assert.deepEqual(errors,[]);
  await browser.close();console.log('PASS: two-field profile, legacy full name, fixed repository prefix, forbidden whitespace/slashes, Unicode/punctuation accepted, assistant repository link and mobile layout.');
})().catch(e=>{console.error(e);process.exit(1);});
