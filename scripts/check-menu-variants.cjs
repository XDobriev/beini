// Real app.js selection/cart/checkout contract; the only transport is an in-memory fixture.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { randomUUID } = require('node:crypto');
const root = path.resolve(__dirname, '..');
const marker = '  /* ---------- События ---------- */';
let source = fs.readFileSync(path.join(root, 'js/app.js'), 'utf8');
assert.ok(source.includes(marker));
source = source.replace(marker, `
  renderCart = () => {}; readForm = () => {}; bump = () => {};
  window.test = { state, getMenu: () => menu, renderMenu, cartLines, cartTotal,
    cards: items => typeof menuCards === 'function' ? menuCards(items) : items,
    select: (...args) => selectVariant(...args), changeQty, refreshMenu, placeOrder, acceptPrices };
  return;
${marker}`);
const item = (id, label, order, price, extra = {}) => ({ id,
  iikoId: '11111111-1111-4111-8111-' + String(order).padStart(12, '0'),
  category: 'drinks', name: 'Лимонад ' + label.toLowerCase(), description: '',
  price, available: true, variantGroup: 'lemonade', variantName: 'Лимонад',
  variantLabel: label, variantOrder: order, ...extra });
const glass = item('glass', 'Стакан', 1, 260);
const jug = item('jug', 'Графин', 2, 650);
const json = value => JSON.parse(JSON.stringify(value));
function boot(items = [jug, glass], storage = new Map()) {
  const nodes = new Map();
  const node = id => {
    if (!nodes.has(id)) nodes.set(id, { innerHTML: '', textContent: '', value: '' });
    return nodes.get(id);
  };
  let runtime = json(items);
  let sent;
  const context = {
    window: {}, navigator: {}, crypto: { randomUUID }, AbortSignal,
    document: { activeElement: null, getElementById: node, querySelector: () => null, querySelectorAll: () => [] },
    localStorage: { getItem: key => storage.get(key) || null,
      setItem: (key, value) => storage.set(key, value), removeItem: key => storage.delete(key) },
    setTimeout: () => 0, clearTimeout() {},
    MENU_DATA: [{ category: 'drinks', items: json(items) }],
    fetch: async (url, options) => {
      if (url === '/api/order') {
        sent = JSON.parse(options.body);
        return { status: 200, json: async () => ({ status: 'pending', totalSum: 1170 }) };
      }
      return { ok: true, json: async () => ({ version: 1, orderingAvailable: true,
        menuUpdatedAt: new Date().toISOString(), stopListUpdatedAt: new Date().toISOString(),
        categories: [{ id: 'drinks', name: 'Напитки', sortOrder: 1 }], items: json(runtime) }) };
    }
  };
  vm.runInNewContext(source, context);
  const app = context.window.test;
  app.state.menuReady = true;
  app.state.menuExpiresAt = Date.now() + 60000;
  return { app, node, storage, runtime: items => { runtime = items; }, sent: () => sent };
}

(async () => {
  const b = boot();
  const { app } = b;
  const cards = app.cards(app.getMenu()[0].items);
  assert.equal(cards.length, 1, 'two portions must render as one dish');
  assert.equal(cards[0].name, 'Лимонад');
  assert.equal(cards[0].id, 'glass', 'default is smallest available portion, independent of source order');
  assert.deepEqual(json(cards[0].variants.map(i => i.id)), ['glass', 'jug']);
  app.renderMenu();
  assert.equal((b.node('menu-sections').innerHTML.match(/<article /g) || []).length, 1);
  assert.match(b.node('menu-sections').innerHTML, /Стакан/);
  assert.match(b.node('menu-sections').innerHTML, /Графин/);
  app.state.query = 'графин';
  app.renderMenu();
  assert.equal((b.node('menu-sections').innerHTML.match(/<article /g) || []).length, 1);
  assert.match(b.node('menu-sections').innerHTML, /Стакан/, 'search retains all portions of a matching dish');
  app.state.query = '';

  app.changeQty('glass', 2);
  app.select('lemonade', 'jug');
  assert.equal(app.cards(app.getMenu()[0].items)[0].price, 650);
  app.changeQty('jug', 1);
  assert.equal(app.cartTotal(), 1170);
  assert.deepEqual(json(app.cartLines().map(l => [l.dish.id, l.qty, l.dish.name])),
    [['glass', 2, 'Лимонад стакан'], ['jug', 1, 'Лимонад графин']]);
  const restored = boot(undefined, b.storage).app;
  assert.equal(restored.cartTotal(), 1170, 'old product-keyed cart survives grouping and refresh');

  const defaultChoice = boot();
  defaultChoice.app.changeQty('glass', 1);
  defaultChoice.runtime([{ ...glass, available: false }, jug]);
  await defaultChoice.app.refreshMenu();
  const stoppedDefault = defaultChoice.app.cards(defaultChoice.app.getMenu()[0].items)[0];
  assert.equal(stoppedDefault.id, 'glass', 'adding the default portion counts as a choice; stop must not switch to jug');
  defaultChoice.app.changeQty(stoppedDefault.id, 1);
  assert.equal(defaultChoice.app.state.cart.glass, 1);
  assert.equal(defaultChoice.app.state.cart.jug, undefined, 'a stopped default must never add a replacement product');

  b.runtime([{ ...glass, price: 280 }, { ...jug, available: false }]);
  await app.refreshMenu();
  assert.equal(app.cards(app.getMenu()[0].items)[0].id, 'jug', 'stop-list must not silently change explicit selection');
  assert.equal(app.state.priceNotice, true, 'portion price change still requires confirmation');
  app.state.orderType = 'pickup';
  app.state.form.phone = '79991234567';
  await app.placeOrder();
  assert.equal(b.sent(), undefined, 'changed prices must block checkout before explicit consent');
  app.acceptPrices();
  assert.equal(app.state.priceNotice, false);
  await app.placeOrder();
  assert.equal(b.sent(), undefined, 'price consent must not allow the stopped portion');
  app.changeQty('jug', 1);
  assert.equal(app.state.cart.jug, 1, 'stopped portion cannot be added');
  app.select('lemonade', 'glass');
  assert.equal(app.cards(app.getMenu()[0].items)[0].price, 280);
  b.runtime([{ ...glass, price: 280 }]);
  await app.refreshMenu();
  assert.equal(app.cartLines().find(l => l.dish.id === 'jug').dish.available, false,
    'disappeared portion remains removable in cart');

  const initialStop = boot([{ ...glass, available: false }, jug]).app;
  assert.equal(initialStop.cards(initialStop.getMenu()[0].items)[0].id, 'jug');
  const four = boot([glass, jug, item('bottle', 'Бутылка', 3, 800), item('large', 'Большой графин', 4, 1000)]).app;
  assert.equal(four.cards(four.getMenu()[0].items)[0].variants.length, 4);
  four.select('lemonade', 'large');
  assert.equal(four.cards(four.getMenu()[0].items)[0].id, 'large');
  const hiddenBoot = boot([glass, { ...jug, hidden: true }]);
  const hidden = hiddenBoot.app;
  hidden.renderMenu();
  assert.doesNotMatch(hiddenBoot.node('menu-sections').innerHTML, /Графин/);
  assert.equal(hidden.cards(hidden.getMenu()[0].items)[0].variants.length, 1);
  const unrelated = boot([glass, { ...jug, variantGroup: undefined, variantName: undefined,
    variantLabel: undefined, variantOrder: undefined }]).app;
  assert.equal(unrelated.cards(unrelated.getMenu()[0].items).length, 2, 'similar names alone must not merge');
  unrelated.select('lemonade', 'jug');
  assert.equal(unrelated.cards(unrelated.getMenu()[0].items)[0].id, 'glass', 'unrelated product cannot become a portion');

  const checkout = boot();
  checkout.app.changeQty('glass', 2);
  checkout.app.changeQty('jug', 1);
  checkout.app.state.orderType = 'pickup';
  checkout.app.state.form.phone = '79991234567';
  await checkout.app.placeOrder();
  assert.deepEqual(checkout.sent().items, [{ productId: 'glass', amount: 2 }, { productId: 'jug', amount: 1 }]);
  assert.deepEqual(checkout.sent().expectedPrices, [260, 650]);

  const catalog = JSON.parse(fs.readFileSync(path.join(root, 'data/menu_catalog.json'), 'utf8'));
  const groups = new Map();
  catalog.items.filter(i => !i.hidden && i.variantGroup).forEach(i => {
    if (!groups.has(i.variantGroup)) groups.set(i.variantGroup, []);
    groups.get(i.variantGroup).push(i);
  });
  assert.equal(groups.size, 29, 'reviewed catalog must have 19 lemonade, 8 jam and 2 honey groups');
  for (const group of groups.values()) assert.equal(group.length, 2);
  const actual = boot(catalog.items.filter(i => !i.hidden));
  const cardCount = actual.app.cards(actual.app.getMenu()[0].items).length;
  assert.equal(cardCount, 222, '251 source products become 222 cards');
  console.log('Portion selection, search, stops, cart restoration, price consent and checkout passed; 29 groups');
})().catch(error => { console.error(error); process.exitCode = 1; });
