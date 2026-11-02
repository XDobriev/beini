// BEINI — логика сайта: меню, корзина, заказ (Go API), бронь (WhatsApp)
(function () {
  'use strict';

  const WA_PHONE = '79204827777';
  const CART_KEY = 'beini_cart';
  const ORDER_KEY = 'beini_order_attempt';
  const CART_PRICES_KEY = 'beini_cart_prices';
  const CAT_ORDER = ['eastern', 'grill', 'hot', 'soups', 'salads', 'sushi', 'pizza', 'breakfasts', 'sides', 'desserts', 'drinks'];
  const IMAGE_PLACEHOLDER = 'assets/images/dishes/placeholder.svg';
  const CAT_NAMES = { breakfasts: 'Завтраки', hot: 'Горячие блюда', sides: 'Гарниры и соусы', eastern: 'Национальная и восточная', grill: 'Мангал и стейки', soups: 'Супы', salads: 'Салаты и закуски', sushi: 'Роллы', pizza: 'Пицца и выпечка', desserts: 'Десерты', drinks: 'Кофе и бар' };
  const CAT_SHORT = { breakfasts: 'Завтраки', hot: 'Горячее', sides: 'Гарниры', eastern: 'Национальная', grill: 'Мангал', soups: 'Супы', salads: 'Салаты', sushi: 'Роллы', pizza: 'Пицца', desserts: 'Десерты', drinks: 'Напитки' };
  const ZONES = ['Кабинка до 10 гостей', 'Основной зал', 'Банкет'];
  const TIMES = ['13:00', '15:00', '17:00', '19:00', '21:00'];
  const GUESTS = ['2', '4', '6', '8', '10+'];
  const WEEKDAYS = ['вс', 'пн', 'вт', 'ср', 'чт', 'пт', 'сб'];
  const REELS = ['reel_10', 'reel_11', 'reel_12', 'reel_13', 'reel_15', 'reel_17'];

  // Проверяем идентификатор, цену и безопасную категорию. Фото может быть заменено заглушкой.
  const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  function isValidDish(dish, cat) {
    if (!dish || typeof dish !== 'object') return false;
    // 1. iiko ID: обязателен и строго формата UUID v4
    if (!dish.iikoId || typeof dish.iikoId !== 'string' || !UUID_RE.test(dish.iikoId.trim())) return false;
    // 2. Цена: число строго больше 0
    if (typeof dish.price !== 'number' || isNaN(dish.price) || dish.price <= 0) return false;
    // 3. Категории из серверного каталога, включая новые, имеют безопасные ID.
    const dishCat = cat || dish.category;
    if (!dishCat || !/^[a-z][a-z0-9-]{0,63}$/.test(dishCat)) return false;
    if (dish.hidden === true) return false;
    // Для отсутствующего фото renderMenu использует нейтральную заглушку.
    return true;
  }

  const rawMenu = (typeof MENU_DATA !== 'undefined' && Array.isArray(MENU_DATA)) ? MENU_DATA : [];
  let menu = rawMenu
    .filter(c => c && /^[a-z][a-z0-9-]{0,63}$/.test(c.category) && Array.isArray(c.items))
    .map(c => ({
      ...c,
      items: c.items.filter(i => isValidDish(i, c.category))
    }))
    .filter(c => c.items.length > 0)
    .sort((a, b) => (CAT_ORDER.includes(a.category) ? CAT_ORDER.indexOf(a.category) : CAT_ORDER.length) - (CAT_ORDER.includes(b.category) ? CAT_ORDER.indexOf(b.category) : CAT_ORDER.length));

  const dishes = {};
  menu.forEach(c => c.items.forEach(i => {
    if (!i.category) i.category = c.category;
    dishes[i.id] = i;
  }));

  const state = {
    menuReady: false,
    menuExpiresAt: 0,
    menuMessage: 'Проверяем актуальность меню…',
    priceNotice: false,
    canRetry: false,
    cart: loadCart(),
    cartPrices: loadCartPrices(),
    orderType: 'delivery', // delivery | pickup
    active: menu[0] ? menu[0].category : CAT_ORDER[0],
    sheetOpen: false,
    orderStatus: 'idle',
    query: '',
    selectedVariants: {},
    attempt: loadAttempt(),
    orderError: '',
    badField: '',
    orderedTotal: 0,
    form: { name: '', phone: '', address: '' },
    book: { zone: 0, day: 0, time: 3, guests: 1, name: '', phone: '', error: '', done: false }
  };

  const $ = id => document.getElementById(id);
  const fmt = n => Number(n).toLocaleString('ru-RU');
  const esc = s => String(s == null ? '' : s).replace(/[&<>"']/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch]));
  const isWide = () => window.matchMedia('(min-width: 1024px)').matches;

  /* ---------- Корзина ---------- */
  function loadCart() {
    try {
      const raw = JSON.parse(localStorage.getItem(CART_KEY) || '{}');
      const cart = {};
      Object.keys(raw).forEach(id => { if (typeof raw[id] === 'number' && raw[id] > 0) cart[id] = raw[id]; });
      return cart;
    } catch (e) { return {}; }
  }
  function loadAttempt() {
    try {
      const a = JSON.parse(localStorage.getItem(ORDER_KEY) || 'null');
      return a && typeof a.key === 'string' && typeof a.status === 'string' ? a : null;
    } catch (e) { return null; }
  }
  function saveAttempt() {
    try {
      if (state.attempt) localStorage.setItem(ORDER_KEY, JSON.stringify(state.attempt)); else localStorage.removeItem(ORDER_KEY);
      return true;
    } catch (e) { return false; }
  }
  function loadCartPrices() {
    try { return JSON.parse(localStorage.getItem(CART_PRICES_KEY) || '{}') || {}; } catch (e) { return {}; }
  }
  function saveCart() {
    const prices = {};
    Object.keys(state.cart).forEach(id => {
      if (dishes[id]) prices[id] = state.cartPrices[id] == null ? dishes[id].price : state.cartPrices[id];
    });
    state.cartPrices = prices;
    try {
      localStorage.setItem(CART_KEY, JSON.stringify(state.cart));
      localStorage.setItem(CART_PRICES_KEY, JSON.stringify(prices));
    } catch (e) { /* приватный режим */ }
  }
  function acceptPrices() {
    readForm();
    Object.keys(state.cart).forEach(id => { if (dishes[id]) state.cartPrices[id] = dishes[id].price; });
    state.priceNotice = false;
    saveCart();
    renderCart();
  }
  function cartLines() {
    return Object.keys(state.cart).filter(id => dishes[id]).map(id => ({ dish: dishes[id], qty: state.cart[id] }));
  }
  function cartCount() { return cartLines().reduce((n, l) => n + l.qty, 0); }
  function cartTotal() { return cartLines().reduce((n, l) => n + l.qty * l.dish.price, 0); }

  function changeQty(id, delta) {
    if (['sending', 'pending', 'unknown'].includes(state.orderStatus)) return;
    if (delta > 0 && (!orderingReady() || !dishes[id] || !dishes[id].available)) return;
    const group = dishes[id] && dishes[id].variantGroup;
    if (delta > 0 && group && !Object.hasOwn(state.selectedVariants, group)) state.selectedVariants[group] = id;
    const next = Math.max(0, (state.cart[id] || 0) + delta);
    if (next) state.cart[id] = next; else delete state.cart[id];
    if (state.orderStatus === 'success') {
      state.orderStatus = 'idle';
      state.attempt = null;
      state.priceNotice = false;
      saveAttempt();
    }
    state.orderError = '';
    saveCart();
    if (delta > 0 && navigator.vibrate) navigator.vibrate(10);
    renderCart();
    updateDishControl(id);
    if (delta > 0) bump();
  }
  function bump() {
    const b = $('header-cart');
    b.classList.remove('bump'); void b.offsetWidth; b.classList.add('bump');
  }

  /* ---------- Меню ---------- */
  function dishControl(id) {
    if (!orderingReady() || !dishes[id].available) return '<span class="pickup-note">Сейчас недоступно</span>';
    const qty = state.cart[id] || 0;
    if (!qty) return `<button type="button" class="add${dishes[id].variantGroup ? ' add-variant' : ''}" data-action="inc" data-id="${id}" aria-label="${esc('Добавить в заказ: ' + (dishes[id].name || 'блюдо'))}">${dishes[id].variantGroup ? 'Добавить' : '+'}</button>`;
    return `<span class="stepper"><button type="button" data-action="dec" data-id="${id}" aria-label="Убрать одну порцию">−</button><span>${qty}</span><button type="button" data-action="inc" data-id="${id}" aria-label="Добавить ещё одну порцию">+</button></span>`;
  }
  function updateDishControl(id) {
    const el = document.querySelector(`.dish-ctl[data-id="${id}"]`);
    if (el) {
      const action = el.contains(document.activeElement) ? document.activeElement.dataset.action : null;
      el.innerHTML = dishControl(id);
      if (action) {
        const next = el.querySelector(`[data-action="${action}"]`) || el.querySelector('button');
        if (next) next.focus();
      }
    }
  }

  function normalizeSearch(value) {
    return value.toLocaleLowerCase('ru-RU').replace(/ё/g, 'е').trim().replace(/\s+/g, ' ');
  }
  // Presentation groups never replace the individual products used by the cart/API.
  function menuCards(items) {
    const slots = [], groups = new Map();
    items.forEach(item => {
      if (!/^[a-z][a-z0-9-]{0,63}$/.test(item.variantGroup || '') || !item.variantName || !item.variantLabel || !Number.isInteger(item.variantOrder) || item.variantOrder < 1) {
        slots.push(item);
        return;
      }
      if (!groups.has(item.variantGroup)) {
        const group = { groupId: item.variantGroup, variants: [] };
        groups.set(item.variantGroup, group);
        slots.push(group);
      }
      groups.get(item.variantGroup).variants.push(item);
    });
    return slots.flatMap(slot => {
      if (!slot.variants) return [slot];
      const variants = slot.variants.slice().sort((a, b) => a.variantOrder - b.variantOrder);
      // Malformed metadata must not hide or merge unrelated products.
      if (variants.some(i => i.variantName !== variants[0].variantName || i.category !== variants[0].category) ||
          new Set(variants.map(i => i.variantLabel)).size !== variants.length ||
          new Set(variants.map(i => i.variantOrder)).size !== variants.length) return variants;
      const selected = variants.find(i => i.id === state.selectedVariants[slot.groupId]) ||
        variants.find(i => i.available) || variants[0];
      return [{ ...selected, name: selected.variantName, groupId: slot.groupId, variants }];
    });
  }
  function selectVariant(group, id) {
    const dish = dishes[id];
    if (!dish || dish.variantGroup !== group || (orderingReady() && !dish.available)) return;
    state.selectedVariants[group] = id;
    renderMenu();
    const button = Array.from(document.querySelectorAll('[data-action="variant"]')).find(el => el.dataset.id === id);
    if (button) button.focus({ preventScroll: true });
  }
  function variantControls(card) {
    if (!card.variants) return '';
    return `<div class="dish-variants" role="group" aria-label="${esc('Порция: ' + card.name)}">${card.variants.map(v => {
      const unavailable = orderingReady() && !v.available;
      return `<button type="button" class="dish-variant" data-action="variant" data-group="${esc(card.groupId)}" data-id="${esc(v.id)}" aria-pressed="${v.id === card.id}"${unavailable ? ' disabled' : ''}>
        <span>${esc(v.variantLabel)}</span><span class="variant-price">${fmt(v.price)} ₽</span>${unavailable ? '<span class="variant-unavailable">Нет в наличии</span>' : ''}
      </button>`;
    }).join('')}</div>`;
  }
  function renderMenu() {
    const focused = document.activeElement;
    const focusedVariant = focused && focused.dataset && focused.dataset.action === 'variant' ? focused.dataset.id : null;
    const q = normalizeSearch(state.query);
    const cards = menu.map(c => ({ ...c, items: menuCards(c.items) }));
    const visible = q ? cards.map(c => ({ ...c, items: c.items.filter(i => normalizeSearch([i.name, ...(i.variants || [i]).map(v => [v.name, v.description, v.variantLabel].join(' '))].join(' ')).includes(q)) })).filter(c => c.items.length) : cards;
    const count = visible.reduce((n, c) => n + c.items.length, 0);
    $('search-status').textContent = q ? (count ? 'Найдено блюд: ' + count : 'Ничего не найдено. Попробуйте другой запрос или очистите поиск.') : '';
    $('menu-sections').innerHTML = visible.map(c => `
      <div class="menu-cat" id="cat-${c.category}">
        <h3 class="h3">${esc(CAT_NAMES[c.category] || c.categoryName)}</h3>
        ${c.items.map(i => `
          <article class="dish${i.variants ? ' dish-with-variants' : ''}">
            <img src="${esc(i.image || IMAGE_PLACEHOLDER)}" alt="${esc(i.name)}" loading="lazy" width="112" height="112" onerror="this.onerror=null;this.src='assets/images/dishes/placeholder.svg';">
            <div class="dish-body">
              ${i.badge ? `<div class="dish-badge">${esc(i.badge)}</div>` : ''}
              <div class="dish-name">${esc(i.name)}</div>
              <div class="dish-desc">${esc(i.description)}</div>
              ${variantControls(i)}
              <div class="dish-foot">
                <span class="price">${fmt(i.price)} ₽ ${i.weight ? `<span class="weight">· ${esc(i.weight)}</span>` : ''}</span>
                <span class="dish-ctl" data-id="${i.id}">${dishControl(i.id)}</span>
              </div>
            </div>
          </article>`).join('')}
      </div>`).join('');
    renderCategories();
    if (focusedVariant) {
      const button = Array.from(document.querySelectorAll('[data-action="variant"]')).find(el => el.dataset.id === focusedVariant);
      if (button) button.focus({ preventScroll: true });
    }
  }

  function renderCategories() {
    $('cat-side').innerHTML = menu.map(c =>
      `<button type="button" data-action="jump" data-cat="${c.category}"${c.category === state.active ? ' class="on"' : ''}>${esc(CAT_NAMES[c.category] || c.categoryName)}</button>`).join('');
    $('cat-chips').innerHTML = menu.map(c =>
      `<button type="button" class="chip${c.category === state.active ? ' on' : ''}" data-action="jump" data-cat="${c.category}">${esc(CAT_SHORT[c.category] || c.categoryName)}</button>`).join('');
  }

  function jump(cat) {
    if (state.query) { state.query = ''; $('menu-search').value = ''; renderMenu(); }
    const el = $('cat-' + cat);
    if (!el) return;
    const offset = isWide() ? 90 : 120;
    window.scrollTo({ top: el.getBoundingClientRect().top + window.scrollY - offset, behavior: 'smooth' });
    state.active = cat;
    renderCategories();
  }

  let spyTicking = false;
  function onScroll() {
    if (spyTicking) return;
    spyTicking = true;
    requestAnimationFrame(() => {
      spyTicking = false;
      if (!menu.length || state.query) return;
      let cur = menu[0].category;
      menu.forEach(c => {
        const el = $('cat-' + c.category);
        if (el && el.getBoundingClientRect().top < 160) cur = c.category;
      });
      if (cur !== state.active) {
        state.active = cur;
        renderCategories();
        const chip = document.querySelector('#cat-chips .chip.on');
        if (chip && chip.scrollIntoView) chip.scrollIntoView({ inline: 'center', block: 'nearest' });
      }
    });
  }

  /* ---------- Тип заказа ---------- */
  function renderOrderType() {
    const btns = [['delivery', 'Доставка'], ['pickup', 'Самовывоз']];
    $('order-type-top').innerHTML = btns.map(([v, n]) =>
      `<button type="button" data-action="type" data-type="${v}" class="${state.orderType === v ? 'on' : ''}">${n}</button>`).join('');
  }
  function setType(t) {
    if (['sending', 'pending', 'unknown'].includes(state.orderStatus)) return;
    state.orderType = t;
    state.orderError = '';
    renderOrderType();
    renderCart();
  }

  /* ---------- Рендер корзины ---------- */
  function renderCart() {
    const lines = cartLines();
    const count = cartCount();
    const total = cartTotal();
    const typeName = state.orderType === 'delivery' ? 'Доставка' : 'Самовывоз';
    const typeNote = state.orderType === 'delivery' ? '~45 мин' : 'Осканова, 21';

    // Кнопка в шапке
    $('header-cart').innerHTML = 'Корзина' + (count ? `<span class="cart-count">${count}</span>` : '');

    // Карточка на десктопе
    $('cart-card').innerHTML = `
      <div class="cart-title">Ваш заказ</div>
      ${count === 0 ? '<div class="cart-empty">Пока пусто. Нажмите «+» рядом с блюдом.</div>' : `
        ${lines.map(l => `
          <div class="cart-row">
            <div><div class="cart-row-name">${esc(l.dish.name)}</div><div class="cart-row-sum">${fmt(l.qty * l.dish.price)} ₽</div></div>
            ${miniStep(l)}
          </div>`).join('')}
        <div class="cart-meta"><span>${typeName}</span><span>${typeNote}</span></div>
        <div class="total-row"><span>Итого</span><span class="total-sum">${fmt(total)} ₽</span></div>
        <button type="button" class="btn btn-ink" style="height:50px" data-action="open-sheet">Оформить заказ</button>`}`;

    // Плавающая полоса на мобильном
    const bar = $('cart-bar');
    bar.hidden = count === 0 || state.sheetOpen;
    bar.innerHTML = `<button type="button" data-action="open-sheet"><span>Корзина · ${count}</span><span class="sum">${fmt(total)} ₽</span></button>`;

    if (state.sheetOpen) renderSheet();
  }

  function miniStep(l) {
    return `<span class="mini-step"><button type="button" data-action="dec" data-id="${l.dish.id}" aria-label="Убрать одну порцию">−</button><span>${l.qty}</span><button type="button" data-action="inc" data-id="${l.dish.id}" aria-label="Добавить ещё одну порцию">+</button></span>`;
  }

  /* ---------- Шторка оформления ---------- */
  const BACKGROUND = ['header', 'main', 'footer'];
  let returnFocus = null;

  function setBackgroundInert(on) {
    BACKGROUND.forEach(tag => {
      const el = document.querySelector(tag);
      if (el) el.inert = on;
    });
  }

  function openSheet() {
    returnFocus = document.activeElement;
    state.sheetOpen = true;
    document.body.classList.add('locked');
    setBackgroundInert(true);
    renderCart();
    const title = $('sheet-title');
    if (title) title.focus();
  }
  function closeSheet() {
    state.sheetOpen = false;
    if (state.orderStatus === 'success') {
      state.orderStatus = 'idle';
      state.attempt = null;
      state.priceNotice = false;
      saveAttempt();
    }
    state.orderError = '';
    state.badField = '';
    document.body.classList.remove('locked');
    setBackgroundInert(false);
    $('sheet-overlay').hidden = true;
    $('sheet-overlay').innerHTML = '';
    renderCart();
    const back = returnFocus && document.contains(returnFocus) && !returnFocus.closest('[hidden]') ? returnFocus : $('header-cart');
    back.focus();
    returnFocus = null;
  }

  // Ключ фокуса: чтобы после перерисовки шторки вернуть его на тот же элемент
  function focusKey(el) {
    if (!el || !el.closest || !el.closest('.sheet')) return null;
    if (el.id) return { sel: '#' + el.id, start: el.selectionStart, end: el.selectionEnd };
    if (el.dataset && el.dataset.action) {
      const sel = '.sheet [data-action="' + el.dataset.action + '"]' +
        (el.dataset.id ? '[data-id="' + el.dataset.id + '"]' : '') +
        (el.dataset.type ? '[data-type="' + el.dataset.type + '"]' : '');
      return { sel };
    }
    return null;
  }
  function restoreFocus(key) {
    if (!key) return;
    const el = document.querySelector(key.sel);
    if (!el) return;
    el.focus();
    if (key.start != null && el.setSelectionRange) {
      try { el.setSelectionRange(key.start, key.end); } catch (e) { /* type=tel не поддерживает */ }
    }
  }

  // aria-invalid и связь поля с текстом ошибки
  function fieldErr(id) {
    return state.badField === id && state.orderError ? ' aria-invalid="true" aria-describedby="order-error"' : '';
  }

  function renderSheet() {
    const key = focusKey(document.activeElement);
    const overlay = $('sheet-overlay');
    const lines = cartLines();
    const total = cartTotal();
    const f = state.form;
    let body;

    if (state.orderStatus === 'success') {
      body = `<p class="sheet-msg">Заказ на ${fmt(state.orderedTotal)} ₽ принят. Мы позвоним, чтобы подтвердить.</p>
        <button type="button" class="btn btn-line" data-action="close-sheet">Вернуться в меню</button>`;
    } else if (state.orderStatus === 'rejected') {
      body = '<p class="sheet-msg" role="alert">Касса отклонила заказ. Заказ не подтверждён. Уточните причину в ресторане.</p><a class="btn btn-line" href="tel:+79204827777">Позвонить в ресторан</a><button type="button" class="btn btn-line" data-action="new-order">Вернуться к оформлению</button>';
    } else if (state.orderStatus === 'pending' || state.orderStatus === 'unknown') {
      body = `<p class="sheet-msg" role="status">${state.orderStatus === 'pending' ? 'Заказ отправлен, но касса ещё не подтвердила его приём.' : 'Не удалось узнать результат отправки. Заказ мог быть передан ресторану.'} Статус обновляется автоматически. Не оформляйте второй заказ.</p>${state.canRetry ? '<button type="button" class="btn btn-line" data-action="retry-order">Повторить запрос с прежним ключом</button>' : ''}<a class="btn btn-line" href="tel:+79204827777">Позвонить в ресторан</a><button type="button" class="btn btn-line" data-action="close-sheet">Вернуться в меню</button>`;
    } else if (!lines.length) {
      body = '<div class="sheet-empty">Пока пусто. Добавьте блюда из меню — кнопка «+» рядом с ценой.</div>';
    } else {
      const sending = state.orderStatus === 'sending';
      body = `
        ${lines.map(l => `
          <div class="sheet-row">
            <img src="${esc(l.dish.image || IMAGE_PLACEHOLDER)}" alt="">
            <div><div class="cart-row-name">${esc(l.dish.name)}</div><div class="cart-row-sum">${fmt(l.qty * l.dish.price)} ₽</div></div>
            ${miniStep(l)}
          </div>`).join('')}
        <div class="seg-box">
          <button type="button" data-action="type" data-type="delivery" class="${state.orderType === 'delivery' ? 'on' : ''}">Доставка</button>
          <button type="button" data-action="type" data-type="pickup" class="${state.orderType === 'pickup' ? 'on' : ''}">Самовывоз</button>
        </div>
        <div class="fields">
          <label class="sr-only" for="f-name">Ваше имя</label>
          <input class="input" id="f-name" placeholder="Ваше имя" autocomplete="name" value="${esc(f.name)}">
          <label class="sr-only" for="f-phone">Телефон</label>
          <input class="input${state.badField === 'f-phone' ? ' bad' : ''}" id="f-phone" type="tel" placeholder="Телефон" autocomplete="tel" inputmode="tel" value="${esc(f.phone)}"${fieldErr('f-phone')}>
          ${state.orderType === 'delivery'
            ? `<label class="sr-only" for="f-address">Адрес в Назрани: улица, дом, квартира</label>
               <input class="input${state.badField === 'f-address' ? ' bad' : ''}" id="f-address" placeholder="Адрес в Назрани: улица, дом, квартира" autocomplete="street-address" value="${esc(f.address)}"${fieldErr('f-address')}>`
            : '<div class="pickup-note">Заберёте по адресу: Осканова, 21</div>'}
        </div>
        <p class="pickup-note" role="status">${esc(state.menuMessage)}</p>
        ${state.priceNotice ? '<p class="form-error" role="alert">Цены в корзине изменились. Проверьте новые цены и сумму.</p><button type="button" class="btn btn-line" data-action="accept-prices">Подтвердить новые цены</button>' : ''}
        ${lines.some(l => !l.dish.available) ? '<p class="form-error">В корзине есть недоступные блюда. Удалите их для оформления.</p>' : ''}
        ${state.orderError ? `<div class="form-error" id="order-error" role="alert">${esc(state.orderError)}</div>` : ''}
        <div class="sheet-total"><span class="lbl">Итого · оплата наличными</span><span class="sum">${fmt(total)} ₽</span></div>
        <button type="button" class="btn btn-ink" data-action="place-order"${sending || !orderingReady() || state.priceNotice || lines.some(l => !l.dish.available) ? ' disabled' : ''}>${sending ? 'Отправляем…' : 'Оформить заказ'}</button>
`;
    }

    overlay.innerHTML = `
      <div class="sheet" role="dialog" aria-modal="true" aria-labelledby="sheet-title">
        <div class="sheet-head"><h2 class="sheet-title" id="sheet-title" tabindex="-1">Ваш заказ</h2><button type="button" class="sheet-close" data-action="close-sheet" aria-label="Закрыть">×</button></div>
        ${body}
      </div>`;
    overlay.hidden = false;
    if (state.orderStatus === 'sending') {
      overlay.querySelectorAll('input, [data-action="inc"], [data-action="dec"], [data-action="type"]').forEach(el => { el.disabled = true; });
    }
    restoreFocus(key);
  }

  /* ---------- Отправка заказа ---------- */
  function readForm() {
    const n = $('f-name'), p = $('f-phone'), a = $('f-address');
    if (n) state.form.name = n.value.trim();
    if (p) state.form.phone = p.value.trim();
    if (a) state.form.address = a.value.trim();
  }

  function validateOrder() {
    const digits = state.form.phone.replace(/\D/g, '');
    if (digits.length < 10) return 'Укажите телефон, чтобы мы могли подтвердить заказ.';
    if (state.orderType === 'delivery' && !state.form.address) return 'Укажите адрес доставки: улица, дом, квартира.';
    return '';
  }

  function orderingReady() {
    return state.menuReady && Date.now() < state.menuExpiresAt;
  }

  let menuTimer;
  async function refreshMenu() {
    clearTimeout(menuTimer);
    try {
      const res = await fetch('/api/menu', { cache: 'no-store', signal: AbortSignal.timeout(10000) });
      const data = await res.json();
      if (!res.ok || !Array.isArray(data.items) || !Array.isArray(data.categories) || !data.version) throw new Error('menu');
      const expires = Math.min(Date.parse(data.menuUpdatedAt) + 2 * 3600000, Date.parse(data.stopListUpdatedAt) + 5 * 60000);
      state.menuReady = data.orderingAvailable === true && Number.isFinite(expires) && Date.now() < expires;
      state.menuExpiresAt = expires;
      readForm();
      const previous = { ...dishes };
      Object.keys(dishes).forEach(id => { delete dishes[id]; });
      menu = data.categories.slice().sort((a, b) => a.sortOrder - b.sortOrder).map(c => ({
        category: c.id, categoryName: c.name,
        items: data.items.filter(i => i.category === c.id && isValidDish(i, c.id)).map(i => ({ ...i, available: state.menuReady && i.available === true }))
      })).filter(c => c.items.length);
      menu.forEach(c => c.items.forEach(i => { dishes[i.id] = i; }));
      // Исчезнувшие позиции остаются в корзине, чтобы гость мог их удалить.
      Object.keys(state.cart).forEach(id => {
        if (!dishes[id] && previous[id]) dishes[id] = { ...previous[id], available: false };
        if (['idle', 'error'].includes(state.orderStatus) && dishes[id] && previous[id] && dishes[id].price !== (state.cartPrices[id] == null ? previous[id].price : state.cartPrices[id])) state.priceNotice = true;
      });
      state.menuMessage = state.menuReady
        ? 'Меню проверено: ' + new Date(data.menuUpdatedAt).toLocaleTimeString('ru-RU') + '. Наличие: ' + new Date(data.stopListUpdatedAt).toLocaleTimeString('ru-RU') + '.'
        : 'Данные меню устарели. Оформление временно недоступно.';
    } catch (e) {
      readForm();
      state.menuReady = false;
      state.menuMessage = 'Не удалось проверить меню. Доступен просмотр, оформление временно недоступно.';
    }
    $('menu-freshness').textContent = state.menuMessage;
    renderMenu();
    renderCart();
    menuTimer = setTimeout(refreshMenu, 30000);
  }

  let statusTimer;
  let attemptGeneration = 0;
  let statusDelay = 5000;
  function scheduleStatus(delay = statusDelay) {
    clearTimeout(statusTimer);
    if (state.attempt && ['sending', 'pending', 'unknown'].includes(state.attempt.status)) statusTimer = setTimeout(pollStatus, delay);
  }
  function applyStatus(data) {
    if (data.status === 'confirmed' && data.creationStatus === 'Success' && Number.isFinite(data.totalSum)) {
      state.cart = {};
      saveCart();
      state.orderedTotal = data.totalSum;
      state.orderStatus = 'success';
      state.priceNotice = false;
      state.attempt = { key: state.attempt.key, status: 'confirmed', totalSum: data.totalSum };
    } else if (data.status === 'rejected') {
      state.orderStatus = 'rejected';
      state.attempt = { key: state.attempt.key, status: 'rejected' };
    } else if (data.status === 'pending') {
      state.orderStatus = data.outcomeUnknown ? 'unknown' : 'pending';
      state.attempt.status = state.orderStatus;
    } else { return false; }
    state.orderError = '';
    saveAttempt();
    return true;
  }
  async function pollStatus() {
    if (!state.attempt || !['sending', 'pending', 'unknown'].includes(state.attempt.status)) return;
    const attempt = state.attempt;
    const generation = attemptGeneration;
    const current = () => state.attempt === attempt && attemptGeneration === generation
      && ['sending', 'pending', 'unknown'].includes(attempt.status);
    try {
      const res = await fetch('/api/order/status', {
        headers: { 'Idempotency-Key': attempt.key }, cache: 'no-store', signal: AbortSignal.timeout(10000)
      });
      const data = await res.json();
      if (!current()) return;
      if (!applyStatus(data)) {
        state.canRetry = res.status === 404 || res.status >= 500;
        throw new Error('status');
      }
      state.canRetry = false;
      statusDelay = Math.max(5000, Math.min(60000, (Number(res.headers.get('Retry-After')) || 5) * 1000));
    } catch (e) {
      if (!current()) return;
      state.canRetry = true;
      state.orderStatus = 'unknown';
      state.attempt.status = 'unknown';
      saveAttempt();
      statusDelay = Math.min(60000, statusDelay * 2);
    }
    renderCart();
    scheduleStatus();
  }
  async function sendAttempt() {
    attemptGeneration++; // Поздний polling не может изменить результат этого POST.
    clearTimeout(statusTimer);
    state.orderStatus = 'sending';
    state.attempt.status = 'sending';
    if (!saveAttempt()) {
      state.orderStatus = 'error';
      state.orderError = 'Не удалось сохранить ключ заказа. Разрешите локальное хранение данных и повторите.';
      state.attempt = null;
      renderCart();
      return;
    }
    renderCart();
    try {
      const res = await fetch('/api/order', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': state.attempt.key },
        body: state.attempt.body, signal: AbortSignal.timeout(40000)
      });
      const data = await res.json();
      if (res.status === 409 && data.code === 'price_changed' && Array.isArray(data.items)) {
        data.items.forEach(i => {
          Object.values(dishes).filter(d => d.iikoId === i.productId).forEach(d => { d.price = i.price; });
        });
        state.priceNotice = true;
        state.orderStatus = 'idle';
        state.attempt = null;
        saveAttempt();
      } else if (applyStatus(data)) {
        state.canRetry = false;
      } else if (res.status === 400 || res.status === 413 || res.status === 415) {
        state.orderStatus = 'error';
        state.orderError = data.error || 'Проверьте данные заказа.';
        state.attempt = null;
        saveAttempt();
      } else { throw new Error('unknown'); }
    } catch (err) {
      state.orderStatus = 'unknown';
      state.attempt.status = 'unknown';
      state.canRetry = true;
      saveAttempt();
    }
    renderMenu();
    renderCart();
    scheduleStatus();
  }
  async function placeOrder() {
    if (['sending', 'pending', 'unknown', 'rejected'].includes(state.orderStatus)) return;
    if (!orderingReady() || state.priceNotice || cartLines().some(l => !l.dish.available)) return;
    readForm();
    const problem = validateOrder();
    if (problem) {
      state.orderError = problem;
      state.orderStatus = 'idle';
      state.badField = state.form.phone.replace(/\D/g, '').length < 10 ? 'f-phone' : 'f-address';
      renderSheet();
      const el = $(state.badField); if (el) el.focus();
      return;
    }
    state.badField = '';
    const lines = cartLines();
    if (!lines.length) return;
    const payload = {
      customerName: state.form.name || 'Гость', phone: state.form.phone, deliveryType: state.orderType,
      address: state.orderType === 'delivery' ? state.form.address : '', comment: 'Заказ с сайта',
      items: lines.map(l => ({ productId: l.dish.id, amount: l.qty })), expectedPrices: lines.map(l => l.dish.price)
    };
    state.attempt = { body: JSON.stringify(payload), key: crypto.randomUUID(), status: 'sending' };
    await sendAttempt();
  }

  function orderToWhatsApp() {
    readForm();
    const lines = cartLines();
    let msg = 'Заказ с сайта «Бейни»\n\n';
    lines.forEach(l => { msg += `${l.dish.name} × ${l.qty} — ${fmt(l.qty * l.dish.price)} ₽\n`; });
    msg += `\nИтого: ${fmt(cartTotal())} ₽ (наличными)\n`;
    msg += `Способ: ${state.orderType === 'delivery' ? 'доставка' : 'самовывоз'}\n`;
    if (state.orderType === 'delivery' && state.form.address) msg += `Адрес: ${state.form.address}\n`;
    if (state.form.name) msg += `Имя: ${state.form.name}\n`;
    if (state.form.phone) msg += `Телефон: ${state.form.phone}\n`;
    window.open(`https://wa.me/${WA_PHONE}?text=${encodeURIComponent(msg)}`, '_blank', 'noopener');
  }

  /* ---------- Бронь ---------- */
  function dayDate(n) { const d = new Date(); d.setDate(d.getDate() + n); return d; }
  function dayLabel(n) {
    if (n === 0) return 'Сегодня';
    if (n === 1) return 'Завтра';
    const d = dayDate(n);
    return WEEKDAYS[d.getDay()] + ', ' + d.getDate();
  }
  function isoDate(n) {
    const d = dayDate(n);
    const p = x => String(x).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
  }
  function bookingSummary() {
    const b = state.book;
    return `${ZONES[b.zone]}, ${dayLabel(b.day).toLowerCase()} в ${TIMES[b.time]}, гостей: ${GUESTS[b.guests]}. Подтвердим по телефону.`;
  }

  function renderBooking() {
    const b = state.book;
    const chips = (arr, sel, action, cls) => arr.map((v, i) => `<button type="button" class="${cls}${i === sel ? ' on' : ''}" data-action="${action}" data-i="${i}">${esc(v)}</button>`).join('');
    if (b.done) {
      $('booking-card').innerHTML = `
        <div class="book-card book-done">
          <h3 class="h3" id="book-done-title" tabindex="-1">Остался один шаг</h3>
          <p>Открыли WhatsApp с вашей заявкой. Нажмите «Отправить» в мессенджере, и мы подтвердим бронь звонком.</p>
          <p class="book-done-sum">${esc(bookingSummary())}</p>
          <a class="btn btn-accent" href="${esc(b.waUrl)}" target="_blank" rel="noopener">Открыть WhatsApp ещё раз</a>
          <div class="book-done-links">
            <a href="tel:+79204827777">Не получается? Позвонить</a>
            <button type="button" class="link-btn" data-action="rebook">Изменить заявку</button>
          </div>
        </div>`;
      const t = $('book-done-title'); if (t && b.justDone) { t.focus({ preventScroll: true }); b.justDone = false; }
      return;
    }
    $('booking-card').innerHTML = `
      <div class="book-card">
        <div><div class="book-label">Где</div><div class="row">${chips(ZONES, b.zone, 'b-zone', 'pill')}</div></div>
        <div>
          <div class="book-label">Когда</div>
          <div class="row scroll">${[0, 1, 2, 3, 4, 5, 6].map(n => dayLabel(n)).map((v, i) => `<button type="button" class="opt${i === b.day ? ' on' : ''}" data-action="b-day" data-i="${i}">${esc(v)}</button>`).join('')}</div>
          <div class="grid5">${chips(TIMES, b.time, 'b-time', 'opt')}</div>
        </div>
        <div><div class="book-label">Гостей</div><div class="grid5" style="margin-top:0">${chips(GUESTS, b.guests, 'b-guests', 'opt')}</div></div>
        <div class="form-cols">
          <label class="sr-only" for="b-name">Имя</label>
          <input class="input" id="b-name" placeholder="Имя" autocomplete="name" value="${esc(b.name)}">
          <label class="sr-only" for="b-phone">Телефон</label>
          <input class="input${b.error ? ' bad' : ''}" id="b-phone" type="tel" placeholder="Телефон" autocomplete="tel" inputmode="tel" value="${esc(b.phone)}"${b.error ? ' aria-invalid="true" aria-describedby="book-error"' : ''}>
        </div>
        <button type="button" class="btn btn-accent" style="height:52px" data-action="book">Забронировать</button>
        ${b.error ? `<div class="form-error" id="book-error" role="alert">${esc(b.error)}</div>` : ''}
        <div class="book-summary">${esc(bookingSummary())}</div>
      </div>`;
  }

  function readBookForm() {
    const n = $('b-name'), p = $('b-phone');
    if (n) state.book.name = n.value.trim();
    if (p) state.book.phone = p.value.trim();
  }

  function submitBooking() {
    readBookForm();
    const b = state.book;
    if (b.phone.replace(/\D/g, '').length < 10) {
      b.error = 'Укажите телефон, чтобы мы подтвердили бронь.';
      renderBooking();
      const p = $('b-phone'); if (p) p.focus();
      return;
    }
    b.error = '';
    const msg = `Бронь в «Бейни»\n\nГде: ${ZONES[b.zone]}\nДата: ${isoDate(b.day)} (${dayLabel(b.day).toLowerCase()})\nВремя: ${TIMES[b.time]}\nГостей: ${GUESTS[b.guests]}\nИмя: ${b.name || '—'}\nТелефон: ${b.phone}`;
    b.waUrl = `https://wa.me/${WA_PHONE}?text=${encodeURIComponent(msg)}`;
    window.open(b.waUrl, '_blank', 'noopener');
    b.done = true;
    b.justDone = true;
    renderBooking();
  }

  /* ---------- Тост ---------- */
  let toastTimer;
  function toast(text) {
    const t = $('toast');
    t.textContent = text; t.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { t.hidden = true; }, 2600);
  }

  /* ---------- События ---------- */
  $('menu-search').addEventListener('input', e => { state.query = e.target.value; renderMenu(); });
  $('search-clear').addEventListener('click', () => { state.query = ''; $('menu-search').value = ''; renderMenu(); $('menu-search').focus(); });
  document.addEventListener('click', e => {
    const overlay = $('sheet-overlay');
    if (e.target === overlay) { closeSheet(); return; }
    const el = e.target.closest('[data-action]');
    if (!el) return;
    const a = el.dataset.action;
    const i = Number(el.dataset.i);
    switch (a) {
      case 'inc': changeQty(el.dataset.id, 1); break;
      case 'dec': changeQty(el.dataset.id, -1); break;
      case 'variant': selectVariant(el.dataset.group, el.dataset.id); break;
      case 'jump': jump(el.dataset.cat); break;
      case 'type': readForm(); setType(el.dataset.type); break;
      case 'open-sheet': openSheet(); break;
      case 'close-sheet': closeSheet(); break;
      case 'place-order': placeOrder(); break;
      case 'accept-prices': acceptPrices(); break;
      case 'retry-order': if (state.orderStatus === 'unknown' && state.attempt && state.attempt.body) sendAttempt(); break;
      case 'new-order': state.attempt = null; saveAttempt(); state.orderStatus = 'idle'; renderCart(); break;
      case 'order-wa': orderToWhatsApp(); break;
      case 'b-zone': readBookForm(); state.book.zone = i; renderBooking(); break;
      case 'b-day': readBookForm(); state.book.day = i; renderBooking(); break;
      case 'b-time': readBookForm(); state.book.time = i; renderBooking(); break;
      case 'b-guests': readBookForm(); state.book.guests = i; renderBooking(); break;
      case 'book': submitBooking(); break;
      case 'rebook': state.book.done = false; renderBooking(); break;
    }
  });
  document.addEventListener('keydown', e => { if (e.key === 'Escape' && state.sheetOpen) closeSheet(); });
  window.addEventListener('scroll', onScroll, { passive: true });
  // Корзина обновилась в другой вкладке (например, вернулись из Instagram)
  window.addEventListener('storage', e => {
    if (e.key === ORDER_KEY && state.orderStatus !== 'sending') {
      state.attempt = loadAttempt();
      if (state.attempt) { restoreAttempt(); renderCart(); scheduleStatus(0); }
      return;
    }
    if (e.key !== CART_KEY || state.orderStatus === 'sending') return;
    state.cart = loadCart();
    state.cartPrices = loadCartPrices();
    renderCart();
    document.querySelectorAll('.dish-ctl').forEach(el => { el.innerHTML = dishControl(el.dataset.id); });
  });

  /* ---------- Старт ---------- */
  $('reels').innerHTML = REELS.map(n => `<a href="https://instagram.com/beini.rest/" aria-label="Открыть Instagram"><img src="assets/images/instagram/${n}.jpg" alt="" loading="lazy"></a>`).join('');
  function restoreAttempt() {
    if (!state.attempt) return;
    if (state.attempt.status === 'confirmed') { state.orderStatus = 'success'; state.orderedTotal = state.attempt.totalSum; }
    else if (state.attempt.status === 'rejected') state.orderStatus = 'rejected';
    else state.orderStatus = state.attempt.status === 'pending' ? 'pending' : 'unknown';
  }
  restoreAttempt();
  renderOrderType();
  renderMenu();
  renderCart();
  renderBooking();
  refreshMenu();
  scheduleStatus(0);
  window.BEINI = { toast };
})();
