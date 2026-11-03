const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const assert = require('node:assert/strict');
const source = fs.readFileSync(path.join(__dirname, '../js/access.js'), 'utf8');
(async () => {
  let allowed = true;
  let nonce = 0;
  const requests = [];
  const context = { window: {}, AbortSignal, Date, localStorage: { setItem: (key, value) => {
    assert.equal(key, 'beini_access_changed'); assert.match(value, /^\d+$/);
  } }, fetch: async (url, options) => {
    requests.push({ url, options });
    assert.equal(options.credentials, 'same-origin');
    assert.equal(options.cache, 'no-store');
    if (url === '/api/access') return { ok: true, json: async () => ({ canOrder: allowed, authenticated: false, csrfToken: 'token-' + (++nonce) }) };
    return { ok: true, status: 200 };
  } };
  vm.runInNewContext(source, context);
  const client = context.window.BEINIAccess;
  await Promise.all([client.read(), client.read()]);
  assert.equal(requests.length, 1, 'concurrent reads were not coalesced');
  await client.post('/api/order', '{}', { 'Idempotency-Key': 'kept-key' });
  const posted = requests.at(-1);
  assert.equal(posted.options.headers['X-CSRF-Token'], 'token-2', 'POST used stale CSRF');
  assert.equal(posted.options.headers['Idempotency-Key'], 'kept-key');
  const postCount = requests.filter(r => r.url === '/api/order').length;
  allowed = false;
  await assert.rejects(() => client.post('/api/order', '{}'), /ordering_closed/);
  assert.equal(requests.filter(r => r.url === '/api/order').length, postCount, 'guest browser sent create');
  client.broadcast();
  console.log('Real access client refresh, CSRF and guest POST guard passed');
})().catch(error => { console.error(error); process.exitCode = 1; });
