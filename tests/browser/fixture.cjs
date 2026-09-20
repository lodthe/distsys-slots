const {chromium} = require('playwright');
const path = require('node:path');

const launch = () => chromium.launch({executablePath: '/usr/bin/chromium-browser', args: ['--no-sandbox', '--disable-dev-shm-usage']});
async function mockApp(context, origin, handleAPI) {
  await context.route('https://telegram.org/**', route => route.fulfill({body: ''}));
  await context.route(origin + '/**', route => {
    const url = new URL(route.request().url());
    if (url.pathname.startsWith('/api/')) return handleAPI(route);
    const file = url.pathname === '/' ? 'index.html' : url.pathname.slice(1);
    return route.fulfill({path: path.join(__dirname, '../../internal/app/web', file),
      contentType: file.endsWith('.js') ? 'text/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html'});
  });
}
module.exports = {launch, mockApp};
