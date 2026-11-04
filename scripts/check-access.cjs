// Regression checks run the real checkout logic without production network.
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');
const marker = '  /* ---------- События ---------- */';
let source = fs.readFileSync(path.join(__dirname, '../js/app.js'), 'utf8');
source = source.replace(marker, `
  renderMenu = () => {}; renderCart = () => {};
  window.test = { state, placeOrder, sendAttempt, refreshAccess, canSubmit, renderSheet, orderToWhatsApp }; return;
${marker}`);
let nextKey = 0;
let locked = false;
const locks = { request: async (_name, _options, work) => {
  if (locked) return work(null);
  locked = true;
  try { return await work({}); } finally { locked = false; }
} };
function boot(access, post, store = new Map(), read = async () => access) {
  const nodes = new Map();
  const opened = [];
  const context = {
    window: { BEINIAccess: { read, post }, open: url => opened.push(url) }, navigator: { locks },
    document: { getElementById(id) { if (['f-name', 'f-phone', 'f-address', 'f-comment'].includes(id) && !nodes.has(id)) return null; if (!nodes.has(id)) nodes.set(id, { hidden: false, innerHTML: '', textContent: '' }); return nodes.get(id); }, activeElement: null },
    crypto: { randomUUID: () => 'brand-new-key-' + (++nextKey) },
    fetch: async () => { throw new Error('guest must never create'); },
    AbortSignal, setTimeout: () => 0, clearTimeout() {},
    localStorage: { getItem: k => store.get(k) || null, setItem: (k, v) => store.set(k, v), removeItem: k => store.delete(k) },
    MENU_DATA: [{ category: 'grill', items: [{ id: 'one', iikoId: '11111111-1111-4111-8111-111111111111', name: 'Блюдо', price: 620, available: true }] }]
  };
  vm.runInNewContext(source, context);
  const app = context.window.test;
  Object.assign(app.state, { menuReady: true, menuExpiresAt: Date.now() + 60000, cart: { one: 1 }, form: { phone: '89990000000', address: 'Ленина, 1' } });
  return { app, store, nodes, opened };
}
(async () => {
  const guest = { mode: 'tester', authenticated: false, canOrder: false, mock: false };
  const { app, nodes } = boot(guest, () => { throw new Error('guest POST'); });
  await app.refreshAccess();
  await app.placeOrder();
  assert.equal(app.state.attempt, null, 'guest reserved a new attempt');
  app.renderSheet();
  assert.match(nodes.get('sheet-overlay').innerHTML, /tel:\+79204827777/);
  assert.doesNotMatch(nodes.get('sheet-overlay').innerHTML, /data-action="place-order"/);
  const saved = { key: 'keep-key', body: '{"unchanged":true}', status: 'unknown' };
  app.state.attempt = { ...saved }; app.state.orderStatus = 'unknown';
  await app.sendAttempt();
  assert.equal(app.state.attempt.key, saved.key, 'expired access changed key');
  assert.equal(app.state.attempt.body, saved.body, 'expired access lost body');
  let calls = 0;
  const owner = boot({ mode: 'tester', authenticated: true, canOrder: true, sessionExpiresAt: new Date(Date.now()+60000).toISOString(), mock: true }, async (url, body, extra) => {
    calls++; assert.equal(url, '/api/order'); assert.equal(body, saved.body); assert.equal(extra['Idempotency-Key'], saved.key);
    return { ok: true, status: 200, json: async () => ({ status: 'pending', outcomeUnknown: false }) };
  });
  owner.app.state.attempt = { ...saved }; owner.app.state.orderStatus = 'unknown';
  await owner.app.sendAttempt();
  assert.equal(calls, 1); assert.equal(owner.app.state.attempt.key, saved.key);
  const expired = boot({ ...guest, canOrder: true, authenticated: true, sessionExpiresAt: new Date(Date.now()-1000).toISOString() });
  await expired.app.refreshAccess();
  assert.equal(expired.app.canSubmit(), false, 'expired snapshot authorized submit');
  const shared = new Map();
  let creates = 0;
  const access = { mode: 'tester', authenticated: true, canOrder: true, sessionExpiresAt: new Date(Date.now()+60000).toISOString(), mock: true };
  const post = async () => { creates++; return { status: 200, json: async () => ({status:'pending'}) }; };
  const tab1 = boot(access, post, shared), tab2 = boot(access, post, shared);
  await Promise.all([tab1.app.placeOrder(), tab2.app.placeOrder()]);
  assert.equal(creates, 1, 'two tabs created distinct attempts');
  assert.equal(JSON.parse(shared.get('beini_order_attempt')).key, tab1.app.state.attempt.key);
  let resolveAccess;
  const typing = boot(access, post, new Map(), () => new Promise(resolve => { resolveAccess = resolve; }));
  typing.nodes.set('f-phone', { value: '89990000000' });
  const refreshing = typing.app.refreshAccess();
  typing.nodes.get('f-phone').value = '89991112233';
  resolveAccess(access); await refreshing;
  assert.equal(typing.app.state.form.phone, '89991112233', 'slow access refresh overwrote typing');
  const blockedStorage = new Map([['beini_order_attempt', JSON.stringify(saved)]]);
  const diskFull = boot(access, post, blockedStorage);
  blockedStorage.set = () => { throw new Error('quota'); };
  await diskFull.app.sendAttempt();
  assert.equal(diskFull.app.state.attempt?.key, saved.key, 'storage failure lost existing key');
  const sentComments = [];
  const commented = boot(access, async (_url, body, headers) => {
    sentComments.push({ body, key: headers['Idempotency-Key'] });
    return { status: 200, json: async () => ({ status: 'pending' }) };
  });
  commented.nodes.set('f-comment', { value: '  Без сахара <&> "пожалуйста"  ' });
  await commented.app.refreshAccess();
  commented.app.renderSheet();
  assert.match(commented.nodes.get('sheet-overlay').innerHTML, /Без сахара &lt;&amp;&gt; &quot;пожалуйста&quot;/, 'comment must be escaped when the form renders');
  await commented.app.placeOrder();
  assert.equal(JSON.parse(sentComments[0].body).comment, 'Без сахара <&> "пожалуйста"', 'checkout dropped the entered comment');
  commented.nodes.get('f-comment').value = 'Изменённый текст';
  commented.app.state.orderStatus = 'unknown';
  await commented.app.sendAttempt();
  assert.deepEqual(sentComments[1], sentComments[0], 'retry changed the saved comment or key');
  const wa = boot(access);
  wa.nodes.set('f-comment', { value: 'Без сахара & сиропа' });
  wa.app.orderToWhatsApp();
  assert.match(new URL(wa.opened[0]).searchParams.get('text'), /Комментарий: Без сахара & сиропа/);
  let blankPayload;
  const blank = boot(access, async (_url, body) => {
    blankPayload = JSON.parse(body);
    return { status: 200, json: async () => ({ status: 'pending' }) };
  });
  blank.nodes.set('f-comment', { value: '   ' });
  await blank.app.placeOrder();
  assert.equal(blankPayload.comment, 'Заказ с сайта', 'blank comment changed existing checkout behavior');
  let limitPayload;
  const atLimit = boot(access, async (_url, body) => {
    limitPayload = JSON.parse(body);
    return { status: 200, json: async () => ({ status: 'pending' }) };
  });
  atLimit.nodes.set('f-comment', { value: 'я'.repeat(500) });
  await atLimit.app.placeOrder();
  assert.equal(limitPayload.comment.length, 500, '500-character comment was rejected');
  const longComment = boot(access, () => { throw new Error('overlong comment submitted'); });
  longComment.nodes.set('f-comment', { value: 'я'.repeat(501), focus() {} });
  await longComment.app.placeOrder();
  assert.equal(longComment.app.state.badField, 'f-comment');
  assert.match(longComment.app.state.orderError, /500/);
  console.log('Access gate, guest call, expiry and unchanged retry passed');
})().catch(e => { console.error(e); process.exitCode = 1; });
