# Mobile-First Menu Tabs & Compact Cards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Сократить длину страницы меню на мобильных устройствах в 8–12 раз за счёт режима одного открытого раздела со сквозным поиском, нижней кнопкой перехода к следующему разделу и компактных карточек блюд без пустых фото-заглушек.

**Architecture:** В `js/app.js` рендер меню (`renderMenu`) при пустом поисковом запросе выводит только активный раздел `state.active` и блок нижней навигации `.menu-pager` («Следующий раздел: … →» и «↑ К началу меню»), а при вводе текста в `#menu-search` временно отображает результаты по всем разделам сразу. Карточки блюд (`.dish`) и строки шторки заказа (`.sheet-row`) проверяют наличие реального фото через `hasDishImage(dish)` и при `placeholder.svg` рендерятся без тега `<img>` в компактной мобильной сетке.

**Tech Stack:** HTML5, CSS Grid/Flexbox (`css/style.css`), Vanilla JS (`js/app.js`), Playwright E2E (`e2e/storefront.spec.js`), Python/Node/Go check suite (`scripts/check.py`).

---

### Task 1: Обновление и расширение E2E-тестов под мобильные вкладки и компактные карточки

**Files:**
- Modify: `e2e/storefront.spec.js:1-43`

- [x] **Step 1: Обновить существующий тест и добавить тесты вкладок, поиска и отсутствия пустых заглушек**

В `e2e/storefront.spec.js` по умолчанию теперь будет открыта только первая категория (`eastern`), а блюда с вариантами порций находятся в `desserts` и `drinks`. Добавим проверку, что изначально показан только 1 раздел и нет `placeholder.svg`, затем переключимся на «Напитки» (кликом по категории), проверим выбор порции, нижнюю кнопку «Следующий раздел» и сквозной поиск по всем категориям.

```javascript
const { test, expect } = require('@playwright/test');

test('renders single active category tab without placeholder images and switches tabs', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', error => pageErrors.push(error.message));

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Меню' })).toBeVisible();

  // По умолчанию рендерится ровно один раздел меню (Национальная и восточная кухня)
  const sections = page.locator('#menu-sections .menu-cat');
  await expect(sections).toHaveCount(1);
  await expect(sections.first().locator('.h3')).toContainText('Национальная и восточная кухня');

  // Пустые заглушки placeholder.svg не рендерятся как <img>
  await expect(page.locator('#menu-sections img[src*="placeholder.svg"]')).toHaveCount(0);

  // Кнопка следующего раздела внизу списка переключает на «Мангал и стейки»
  const nextBtn = page.locator('.menu-pager [data-action="jump"]');
  await expect(nextBtn).toContainText('Следующий раздел: Мангал и стейки');
  await nextBtn.click();

  await expect(sections).toHaveCount(1);
  await expect(sections.first().locator('.h3')).toContainText('Мангал и стейки');

  // Поиск ищет сквозь все разделы меню
  const searchInput = page.locator('#menu-search');
  await searchInput.fill('лимонад');
  await expect(page.locator('#menu-sections .dish-with-variants').first()).toBeVisible();

  // Очистка поиска возвращает режим одного раздела
  await page.locator('#search-clear').click();
  await expect(sections).toHaveCount(1);
  expect(pageErrors).toEqual([]);
});

test('guest can switch to drinks category, select a portion and add it to the cart', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', error => pageErrors.push(error.message));

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Меню' })).toBeVisible();

  const drinksBtn = page.locator('[data-action="jump"][data-cat="drinks"]:visible').first();
  await drinksBtn.click();

  const dish = page.locator('.dish-with-variants').first();
  const group = dish.locator('.dish-variants');
  const variants = group.locator('button.dish-variant:not([disabled])');
  await expect(variants).toHaveCount(2);
  const secondVariant = variants.nth(1);
  await secondVariant.click();
  await expect(secondVariant).toHaveAttribute('aria-pressed', 'true');

  await dish.getByRole('button', { name: /Добавить в заказ:/ }).click();
  await expect(page.locator('#header-cart .cart-count')).toHaveText('1');
  await expect(dish.locator('.stepper')).toContainText('1');

  const mock = await page.evaluate(() => fetch('/__mock__').then(response => response.json()));
  expect(mock.creates).toBe(0);
  expect(pageErrors).toEqual([]);
});

test('mobile guest can open and close the cart sheet without submitting', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'mobile-chromium');

  await page.goto('/');
  await page.getByRole('button', { name: /Добавить в заказ:/ }).first().click();
  const cartBar = page.locator('#cart-bar');
  await expect(cartBar).toBeVisible();
  await cartBar.getByRole('button').click();

  const cart = page.getByRole('dialog', { name: 'Ваш заказ' });
  await expect(cart).toBeVisible();
  await expect(cart.locator('.sheet-row img[src*="placeholder.svg"]')).toHaveCount(0);
  await cart.getByRole('button', { name: 'Закрыть' }).click();
  await expect(cart).toBeHidden();

  const mock = await page.evaluate(() => fetch('/__mock__').then(response => response.json()));
  expect(mock.creates).toBe(0);
});
```

- [x] **Step 2: Запустить Playwright тест, чтобы убедиться, что он падает до реализации**

Run: `npx playwright test`
Expected: FAIL (currently renders all 11 `.menu-cat` sections and `placeholder.svg` images).

---

### Task 2: Реализация логики вкладок, нижнего переключателя и проверки реального фото в `js/app.js`

**Files:**
- Modify: `js/app.js:10-13`
- Modify: `js/app.js:220-290`
- Modify: `js/app.js:428-435`
- Modify: `js/app.js:558-563`
- Modify: `js/app.js:874-897`

- [x] **Step 1: Обновить словари `CAT_NAMES` и `CAT_SHORT` и добавить `hasDishImage` в `js/app.js`**

В `js/app.js` обновить строки 10–13:
```javascript
  const IMAGE_PLACEHOLDER = 'assets/images/dishes/placeholder.svg';
  const CAT_NAMES = { breakfasts: 'Завтраки', hot: 'Горячие блюда и рыба', sides: 'Гарниры и соусы', eastern: 'Национальная и восточная кухня', grill: 'Мангал и стейки', soups: 'Супы', salads: 'Салаты и закуски', sushi: 'Суши и роллы', pizza: 'Пицца и выпечка', desserts: 'Десерты', drinks: 'Кофе и напитки' };
  const CAT_SHORT = { breakfasts: 'Завтраки', hot: 'Горячее и рыба', sides: 'Гарниры', eastern: 'Восточная', grill: 'Мангал и стейки', soups: 'Супы', salads: 'Салаты', sushi: 'Суши и роллы', pizza: 'Пицца и выпечка', desserts: 'Десерты', drinks: 'Напитки' };
```

И добавить вспомогательную функцию `hasDishImage(dish)` перед `renderMenu()`:
```javascript
  function hasDishImage(dish) {
    if (!dish || typeof dish.image !== 'string') return false;
    const src = dish.image.trim();
    return Boolean(src && src !== IMAGE_PLACEHOLDER && !src.endsWith('/placeholder.svg'));
  }
```

- [x] **Step 2: Обновить `renderMenu()`, `renderCategories()` и `jump()` в `js/app.js`**

Заменить блок `renderMenu`, `renderCategories`, `jump` и `onScroll` в `js/app.js` на:
```javascript
  function renderDishCard(i) {
    const showImg = hasDishImage(i);
    const cls = ['dish'];
    if (!showImg) cls.push('dish-no-img');
    if (i.variants) cls.push('dish-with-variants');
    return `
          <article class="${cls.join(' ')}">
            ${showImg ? `<img src="${esc(i.image)}" alt="${esc(i.name)}" loading="lazy" width="112" height="112" onerror="this.closest('.dish').classList.add('dish-no-img');this.remove();">` : ''}
            <div class="dish-body">
              <div class="dish-main">
                <div class="dish-info">
                  ${i.badge ? `<div class="dish-badge">${esc(i.badge)}</div>` : ''}
                  <div class="dish-name">${esc(i.name)}</div>
                  ${i.description ? `<div class="dish-desc">${esc(i.description)}</div>` : ''}
                </div>
                ${!i.variants ? `
                <div class="dish-foot">
                  <span class="price">${fmt(i.price)} ₽ ${i.weight ? `<span class="weight">· ${esc(i.weight)}</span>` : ''}</span>
                  <span class="dish-ctl" data-id="${i.id}">${dishControl(i.id)}</span>
                </div>` : ''}
              </div>
              ${variantControls(i)}
              ${i.variants ? `
              <div class="dish-foot">
                <span class="price">${fmt(i.price)} ₽ ${i.weight ? `<span class="weight">· ${esc(i.weight)}</span>` : ''}</span>
                <span class="dish-ctl" data-id="${i.id}">${dishControl(i.id)}</span>
              </div>` : ''}
            </div>
          </article>`;
  }

  function renderPager() {
    if (menu.length <= 1) return '';
    const idx = menu.findIndex(c => c.category === state.active);
    const next = menu[(idx >= 0 ? idx + 1 : 1) % menu.length];
    const nextTitle = CAT_NAMES[next.category] || next.categoryName;
    return `
      <div class="menu-pager">
        <button type="button" class="btn btn-line menu-next-btn" data-action="jump" data-cat="${esc(next.category)}" data-scroll="top">
          <span>Следующий раздел: ${esc(nextTitle)}</span>
          <span aria-hidden="true">→</span>
        </button>
        <button type="button" class="menu-top-btn" data-action="menu-top">↑ К выбору раздела</button>
      </div>`;
  }

  function renderMenu() {
    const focused = document.activeElement;
    const focusedVariant = focused && focused.dataset && focused.dataset.action === 'variant' ? focused.dataset.id : null;
    if (menu.length && !menu.some(c => c.category === state.active)) {
      state.active = menu[0].category;
    }
    const q = normalizeSearch(state.query);
    const cards = menu.map(c => ({ ...c, items: menuCards(c.items) }));
    const matched = q
      ? cards.map(c => ({
          ...c,
          items: c.items.filter(i => normalizeSearch([i.name, ...(i.variants || [i]).map(v => [v.name, v.description, v.variantLabel].join(' '))].join(' ')).includes(q))
        })).filter(c => c.items.length)
      : cards.filter(c => c.category === state.active);
    const visible = (!q && !matched.length && cards.length) ? [cards[0]] : matched;
    const count = visible.reduce((n, c) => n + c.items.length, 0);
    $('search-status').textContent = q ? (count ? 'Найдено блюд: ' + count : 'Ничего не найдено. Попробуйте другой запрос или очистите поиск.') : '';
    $('menu-sections').innerHTML = visible.map(c => `
      <div class="menu-cat" id="cat-${c.category}">
        <div class="menu-cat-head">
          <h3 class="h3">${esc(CAT_NAMES[c.category] || c.categoryName)}</h3>
          <span class="menu-cat-count">${c.items.length}</span>
        </div>
        ${c.items.map(renderDishCard).join('')}
        ${!q ? renderPager() : ''}
      </div>`).join('');
    renderCategories();
    if (focusedVariant) {
      const button = Array.from(document.querySelectorAll('[data-action="variant"]')).find(el => el.dataset.id === focusedVariant);
      if (button) button.focus({ preventScroll: true });
    }
  }

  function renderCategories() {
    const hasQuery = Boolean(normalizeSearch(state.query));
    $('cat-side').innerHTML = menu.map(c => {
      const active = !hasQuery && c.category === state.active;
      return `<button type="button" data-action="jump" data-cat="${c.category}" aria-pressed="${active}"${active ? ' class="on"' : ''}>${esc(CAT_NAMES[c.category] || c.categoryName)}</button>`;
    }).join('');
    $('cat-chips').innerHTML = menu.map(c => {
      const active = !hasQuery && c.category === state.active;
      return `<button type="button" class="chip${active ? ' on' : ''}" data-action="jump" data-cat="${c.category}" aria-pressed="${active}">${esc(CAT_SHORT[c.category] || c.categoryName)}</button>`;
    }).join('');
  }

  function scrollMenuIntoView(forceTop) {
    const target = $('menu-sections');
    if (!target) return;
    const offset = isWide() ? 90 : 124;
    const rect = target.getBoundingClientRect();
    if (forceTop || rect.top < offset - 16 || rect.top > window.innerHeight * 0.7) {
      window.scrollTo({ top: Math.max(0, rect.top + window.scrollY - offset), behavior: 'smooth' });
    }
  }

  function jump(cat, forceTop) {
    if (!cat) return;
    state.active = cat;
    if (state.query) {
      state.query = '';
      $('menu-search').value = '';
    }
    renderMenu();
    const chip = document.querySelector('#cat-chips .chip.on');
    if (chip && chip.scrollIntoView) chip.scrollIntoView({ inline: 'center', block: 'nearest', behavior: 'smooth' });
    scrollMenuIntoView(forceTop);
  }
```

- [x] **Step 3: Обновить строку шторки заказа `.sheet-row` и обработчики событий в `js/app.js`**

В `renderSheet()` (строки 429–434) убрать рендер пустой заглушки `placeholder.svg`:
```javascript
        ${lines.map(l => {
          const showImg = hasDishImage(l.dish);
          return `
          <div class="sheet-row${showImg ? '' : ' sheet-row-no-img'}">
            ${showImg ? `<img src="${esc(l.dish.image)}" alt="" onerror="this.closest('.sheet-row').classList.add('sheet-row-no-img');this.remove();">` : ''}
            <div><div class="cart-row-name">${esc(l.dish.name)}</div><div class="cart-row-sum">${fmt(l.qty * l.dish.price)} ₽</div></div>
            ${miniStep(l)}
          </div>`;
        }).join('')}
```

В блоке обработчика кликов `document.addEventListener('click', ...)` обновить `case 'jump'` и добавить `case 'menu-top'`, а также удалить `window.addEventListener('scroll', onScroll, { passive: true });`:
```javascript
      case 'jump': jump(el.dataset.cat, el.dataset.scroll === 'top'); break;
      case 'menu-top': {
        const menuEl = $('menu');
        if (menuEl) window.scrollTo({ top: Math.max(0, menuEl.getBoundingClientRect().top + window.scrollY - 72), behavior: 'smooth' });
        break;
      }
```

---

### Task 3: Компактная Mobile-First вёрстка карточек, чипов и переключателя разделов в `css/style.css`

**Files:**
- Modify: `css/style.css:160-199`
- Modify: `css/style.css:295-299`
- Modify: `css/style.css:340-345`

- [x] **Step 1: Обновить стили `.cat-chips`, `.menu-cat`, `.dish`, `.dish-no-img`, `.menu-pager` и `.sheet-row-no-img` в `css/style.css`**

В `css/style.css` улучшить мобильную полосу чипов (убрать системный скроллбар, добавить `scroll-snap-type`), заголовок категории со счётчиком, компактную сетку `.dish-no-img` и блок `.menu-pager`:

```css
.cat-chips {
  position: sticky; top: 65px; z-index: 10; background: var(--bg);
  display: flex; gap: 8px; overflow-x: auto; scroll-snap-type: x proximity;
  scrollbar-width: none; -ms-overflow-style: none;
  padding: 8px var(--pad) 12px; margin: 0 calc(var(--pad) * -1); border-bottom: 1px solid var(--img-placeholder);
}
.cat-chips::-webkit-scrollbar { display: none; }
.chip {
  flex: none; height: 40px; padding: 0 15px; display: flex; align-items: center; white-space: nowrap;
  scroll-snap-align: start;
  border-radius: 999px; font-size: 14px; border: 1px solid var(--line-2);
}
.chip.on { background: var(--ink); color: var(--bg); border-color: var(--ink); font-weight: 500; }
.cat-side { position: sticky; top: 90px; flex-direction: column; gap: 4px; }
.cat-side button { padding: 9px 0; font-size: 16px; text-align: left; color: var(--muted); }
.cat-side button.on { color: var(--accent); font-weight: 500; }

.menu-cat { scroll-margin-top: 120px; padding: 20px 0 8px; }
.menu-cat-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 4px; padding-bottom: 8px; border-bottom: 1px solid var(--line); }
.menu-cat-count { font-size: 13px; color: var(--muted-2); font-variant-numeric: lining-nums; white-space: nowrap; }
.dish { display: grid; grid-template-columns: 88px 1fr; gap: 16px; padding: 18px 0; border-bottom: 1px solid var(--line); }
.dish.dish-no-img { grid-template-columns: 1fr; padding: 14px 0; }
.dish img { width: 100%; aspect-ratio: 1; object-fit: cover; border-radius: 2px; background: var(--img-placeholder); }
.dish img.img-fallback, .sheet-row img.img-fallback { opacity: .6; }
.dish-body { display: flex; flex-direction: column; min-width: 0; }
.dish-main { display: flex; flex-direction: column; gap: 8px; }
.dish-no-img:not(.dish-with-variants) .dish-main {
  flex-direction: row; align-items: center; justify-content: space-between; gap: 12px;
}
.dish-info { min-width: 0; flex: 1; }
.dish-badge { font-size: 12.5px; color: var(--accent); margin-bottom: 2px; }
.dish-name { font-family: var(--serif); font-weight: 600; font-size: 22px; line-height: 1.12; }
.dish-desc { font-size: 13.5px; color: var(--muted); line-height: 1.45; margin-top: 4px; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.dish-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: auto; padding-top: 10px; }
.dish-no-img:not(.dish-with-variants) .dish-foot {
  flex: none; padding-top: 0; margin-top: 0; justify-content: flex-end; gap: 12px;
}
.dish-no-img:not(.dish-with-variants) .price {
  display: flex; flex-direction: column; align-items: flex-end; text-align: right; line-height: 1.2;
}
.dish-no-img:not(.dish-with-variants) .weight { font-size: 12px; }
.menu-pager { margin-top: 20px; padding-top: 4px; display: flex; flex-direction: column; align-items: center; gap: 10px; }
.menu-next-btn { width: 100%; min-height: 48px; padding: 0 18px; display: flex; align-items: center; justify-content: space-between; gap: 12px; border-radius: 6px; font-size: 15px; font-weight: 500; }
.menu-top-btn { min-height: 44px; padding: 0 12px; font-size: 14px; color: var(--muted); text-decoration: underline; text-underline-offset: 4px; }
```

И для `.sheet-row-no-img` (около строки 295) и десктопного `@media (min-width: 1024px)` (около строки 343):
```css
.sheet-row.sheet-row-no-img { grid-template-columns: 1fr auto; }
```
В `@media (min-width: 1024px)`:
```css
  .dish { grid-template-columns: 112px 1fr; }
  .dish.dish-no-img { grid-template-columns: 1fr; }
```

---

### Task 4: Обновление документации (`docs/design-system.md`, `docs/product.md`) и запуск всех проверок

**Files:**
- Modify: `docs/design-system.md:80-102`
- Modify: `docs/product.md:16-32`

- [x] **Step 1: Обновить описание меню в `docs/design-system.md` и `docs/product.md`**

Зафиксировать в `docs/design-system.md` и `docs/product.md`:
- Вкладочный режим показа одного активного раздела меню при пустом поиске и сквозной поиск по всем категориям при вводе запроса.
- Нижний переключатель раздела `.menu-pager` («Следующий раздел: … →» и «↑ К выбору раздела»).
- Компактную вёрстку карточек `.dish-no-img` и строк `.sheet-row-no-img` без рендера пустых заглушек `placeholder.svg` (при сохранении автоматического показа реальных фото, когда они появятся).

- [x] **Step 2: Выполнить полную проверку проекта и E2E-тесты**

Run:
```powershell
python scripts/check.py
npx playwright test
```
Expected: Все проверки `scripts/check.py` (Go tests, Go vet, JS syntax, menu consistency, Python importer tests) и все Playwright E2E тесты (`desktop-chromium` и `mobile-chromium`) успешно проходят.

