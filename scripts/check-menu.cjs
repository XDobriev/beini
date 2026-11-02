// Compare executable browser menu data with the editable JSON catalog, offline.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.resolve(__dirname, '..');
const catalog = JSON.parse(fs.readFileSync(path.join(root, 'data/menu_catalog.json'), 'utf8'));
const source = fs.readFileSync(path.join(root, 'js/menu-data.js'), 'utf8');
const sections = vm.runInNewContext(source + '\nJSON.stringify(MENU_DATA)', {}, { timeout: 1000 });
const menu = JSON.parse(sections);

function index(items, label) {
  const result = new Map();
  const iikoIDs = new Set();
  for (const item of items) {
    assert.ok(item.id && item.iikoId, label + ': missing id/iikoId');
    assert.ok(Number.isFinite(item.price) && item.price > 0, label + ': invalid price for ' + item.id);
    assert.ok(!result.has(item.id), label + ': duplicate id ' + item.id);
    assert.ok(!iikoIDs.has(item.iikoId), label + ': duplicate iikoId ' + item.iikoId);
    result.set(item.id, { iikoId: item.iikoId, price: item.price, category: item.category });
    iikoIDs.add(item.iikoId);
  }
  return result;
}

const expected = index(catalog.items.filter(item => item.available !== false && !item.hidden), 'JSON');
const actual = index(menu.flatMap(section => section.items.map(item => ({
  ...item, category: section.category
}))), 'JS');
assert.ok(expected.size > 0, 'Menu must not be empty');
assert.equal(actual.size, expected.size, 'Menu item counts differ');
for (const [id, item] of expected) {
  assert.deepEqual(actual.get(id), item, 'Menu mismatch for ' + id);
}
console.log('JSON/JS menu consistent: ' + expected.size + ' items');
