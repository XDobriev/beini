// Exercise real menu filtering and refresh without network or a browser.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
let source = fs.readFileSync(path.join(__dirname, '../js/app.js'), 'utf8');
const marker = '  /* ---------- События ---------- */';
source = source.replace(marker, `
  renderMenu = () => {}; renderCart = () => {}; readForm = () => {};
  window.test = { getMenu: () => menu, isValidDish, refreshMenu }; return;
${marker}`);
const id = n => '11111111-1111-4111-8111-' + String(n).padStart(12, '0');
const dish = (n, extra = {}) => ({ id: 'dish-' + n, iikoId: id(n), name: 'Блюдо', price: 100, ...extra });
const context = {
  window: {}, navigator: {}, document: { getElementById: () => ({}) },
  localStorage: { getItem: () => null }, setTimeout: () => 0, clearTimeout() {}, AbortSignal,
  MENU_DATA: [{ category: 'breakfasts', categoryName: 'Завтраки', items: [
    dish(1), dish(2, { name: 'NEW Скрытое', hidden: true, image: 'photo.jpg' })
  ] }],
  fetch: async () => ({ ok: true, json: async () => ({ version: 1,
    orderingAvailable: true, menuUpdatedAt: new Date().toISOString(), stopListUpdatedAt: new Date().toISOString(),
    categories: [{ id: 'new-category', name: 'Новая категория', sortOrder: 1 }],
    items: [dish(3, { category: 'new-category', available: true }),
      dish(4, { category: 'new-category', hidden: true, available: true })] }) })
};
vm.runInNewContext(source, context);
const app = context.window.test;
assert.equal(app.getMenu().length, 1, 'breakfast category disappeared');
assert.equal(app.getMenu()[0].items.length, 1, 'missing image excluded item or hidden NEW leaked');
assert.equal(app.isValidDish(dish(1), 'bad"category'), false, 'unsafe category accepted');
app.refreshMenu().then(() => {
  assert.equal(app.getMenu()[0].category, 'new-category', 'server category disappeared');
  assert.equal(app.getMenu()[0].categoryName, 'Новая категория');
  assert.equal(app.getMenu()[0].items.length, 1, 'hidden runtime item leaked');
  console.log('Menu categories, photo fallback and hidden NEW protection passed');
}).catch(error => { console.error(error); process.exitCode = 1; });
