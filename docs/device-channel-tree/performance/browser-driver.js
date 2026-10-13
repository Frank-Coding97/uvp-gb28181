async (page, count = 1000) => {
  const out = '/tmp/uvp-directory-load-20261012';
  const context = await page.context().browser().newContext({ viewport: { width: 1440, height: 900 } });
  const bench = await context.newPage();
  const errors = [];
  bench.on('pageerror', e => errors.push(e.message));
  bench.on('console', msg => {if (msg.type() === 'error') errors.push(msg.text())});
  const source = await (await context.request.get('http://localhost:5189/src/views/gb28181/device-mgmt/components/DirectoryPanel.vue')).text();
  const vueURL = source.match(/from "([^"]*\/vue.js[^\"]*)"/)[1];
  const html = (await (await context.request.get('http://127.0.0.1:18991/browser-harness.html')).text()).replace('__VUE_URL__', vueURL);
  await bench.route('**/src/views/gb28181/device-mgmt/api.ts*', route => route.fulfill({ contentType: 'application/javascript', body: 'export const listDirectoryTree = async () => window.__payload;' }));
  await bench.route('**/__directory_load/data?*', async route => route.fulfill({ contentType: 'application/json', body: await (await context.request.get('http://127.0.0.1:18991/tree-' + count + '.json')).text() }));
  await bench.route('**/__directory_load/index', route => route.fulfill({ contentType: 'text/html', body: html }));
  let result;
  const timeout = setTimeout(() => context.close().catch(() => {}), 55000);
  try {
    await bench.goto('http://localhost:5189/__directory_load/index');
    await bench.waitForFunction(() => window.__ready, { timeout: 30000 });
    await bench.evaluate(n => window.loadFixture(n), count);
    const mount = await bench.evaluate(() => window.mountFixture());
    const exercise = await bench.evaluate(() => window.exercise());
    result = { devices: count, ...mount, ...exercise, errors, browser: page.context().browser().version(), note: 'Isolated real DirectoryPanel; local synthetic API response; excludes DB/network and rest of application.' };
    await bench.screenshot({ path: out + '/browser-' + count + '.png' });
  } catch (e) {
    result = { devices: count, failed: String(e), errors, note: '55-second safety deadline per browser scenario' };
  } finally {
    clearTimeout(timeout);
    await context.close();
  }
  return result;
}
