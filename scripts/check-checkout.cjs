// Проверяем переход между заказами на реальной логике app.js без DOM и сети.
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');
let source = fs.readFileSync(path.join(__dirname, '../js/app.js'), 'utf8');
const marker = '  /* ---------- События ---------- */';
assert.ok(source.includes(marker), 'frontend test entry point missing');
source = source.replace(marker, `
  renderCart = () => {}; updateDishControl = () => {}; bump = () => {};
  window.test = { state, changeQty, restoreAttempt, pollStatus };
  return;
${marker}`);
function boot(storage, fetchRequest) {
  const context = {
    window: {}, navigator: {},
    fetch: fetchRequest, AbortSignal, setTimeout: () => 0, clearTimeout: () => {},
    localStorage: {
      getItem: key => storage.get(key) || null,
      setItem: (key, value) => storage.set(key, value),
      removeItem: key => storage.delete(key)
    },
    MENU_DATA: [{ category: 'grill', items: [{ id: 'one',
      iikoId: '11111111-1111-4111-8111-111111111111', price: 620,
      image: 'one.jpg', available: true }] }]
  };
  vm.runInNewContext(source, context);
  const app = context.window.test;
  app.restoreAttempt();
  app.state.menuReady = true;
  app.state.menuExpiresAt = Date.now() + 60000;
  return app;
}
const storage = new Map([['beini_order_attempt', JSON.stringify({ key: 'old-key', status: 'confirmed', totalSum: 620 })]]);
boot(storage).changeQty('one', 1);
const restored = boot(storage);
assert.equal(restored.state.cart.one, 1);
assert.equal(restored.state.orderStatus, 'idle', 'old confirmed order masks new unsubmitted cart after refresh');
for (const status of ['pending', 'unknown', 'sending']) {
  const data = new Map([['beini_order_attempt', JSON.stringify({ key: 'keep-key', body: '{"unchanged":true}', status })]]);
  const app = boot(data);
  app.changeQty('one', 1);
  assert.equal(app.state.cart.one, undefined, 'unresolved order must block new cart changes');
  assert.equal(JSON.parse(data.get('beini_order_attempt')).key, 'keep-key');
}
(async () => {
  let rejectPoll;
  const data = new Map([['beini_order_attempt', JSON.stringify({ key: 'old-poll', body: '{}', status: 'unknown' })]]);
  const app = boot(data, () => new Promise((resolve, reject) => { rejectPoll = reject; }));
  const pending = app.pollStatus();
  // A retry has already completed, and the guest closed the result screen.
  app.state.attempt = null;
  app.state.orderStatus = 'idle';
  rejectPoll(new Error('late timeout'));
  await pending;
  assert.equal(app.state.orderStatus, 'idle', 'old polling error must not overwrite the next checkout');
  let resolvePoll;
  const next = boot(data, () => new Promise(resolve => { resolvePoll = resolve; }));
  const old = next.pollStatus();
  next.state.attempt = { key: 'new-order', status: 'unknown', body: '{}' };
  resolvePoll({ json: async () => ({ status: 'confirmed', creationStatus: 'Success', totalSum: 620 }) });
  await old;
  assert.equal(next.state.attempt.key, 'new-order', 'old response must not confirm a different attempt');
  assert.equal(next.state.orderStatus, 'unknown');
  console.log('Checkout persistence and stale polling protection passed');
})().catch(error => { console.error(error); process.exitCode = 1; });
