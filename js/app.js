// ==========================================================================
// RESTAURANT BEINI — LUXURY GASTRONOMY WEB CLIENT & CART ENGINE
// ==========================================================================

// State Management
let currentCategory = 'all';
let cart = [];

// DOM Ready
document.addEventListener('DOMContentLoaded', () => {
  initMenu();
  initEventListeners();
  initBookingDefaults();
  updateCartUI();
});

// Initialize Menu Render
function initMenu() {
  const container = document.getElementById('menu-items-container');
  if (!container) return;

  container.innerHTML = '';

  let itemsToRender = [];
  if (currentCategory === 'all') {
    MENU_DATA.forEach(cat => {
      itemsToRender = itemsToRender.concat(cat.items);
    });
  } else {
    const found = MENU_DATA.find(c => c.category === currentCategory);
    if (found) itemsToRender = found.items;
  }

  itemsToRender.forEach(item => {
    const card = document.createElement('div');
    card.className = 'dish-card bg-white rounded-2xl overflow-hidden border border-stone-200/90 shadow-sm flex flex-col justify-between group relative';
    
    // Badge styling mapping
    let badgeHtml = '';
    if (item.badge) {
      let badgeClass = 'badge-hit';
      if (item.badge.includes('шеф') || item.badge.includes('Шеф')) {
        badgeClass = 'badge-chef';
      } else if (item.badge.includes('Премиум')) {
        badgeClass = 'badge-premium';
      }
      badgeHtml = `<span class="${badgeClass} px-2.5 py-1 rounded-full shadow-sm">${item.badge}</span>`;
    }

    const halalHtml = item.isHalal 
      ? `<span class="badge-halal px-2.5 py-1 rounded-full flex items-center gap-1 shadow-sm"><span class="text-[#e6c587]">✦</span> Халяль</span>` 
      : '';

    card.innerHTML = `
      <div>
        <div class="relative h-48 sm:h-56 overflow-hidden bg-stone-100">
          <img 
            src="${item.image}" 
            alt="${item.name}" 
            class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500"
            loading="lazy"
          >
          <div class="absolute top-3 left-3 flex flex-wrap gap-1.5 z-10">
            ${badgeHtml}
            ${halalHtml}
          </div>
          <span class="badge-weight absolute bottom-3 right-3 px-2.5 py-1 rounded-md text-xs font-semibold">
            ${item.weight}
          </span>
        </div>
        <div class="p-4 sm:p-5">
          <div class="flex items-start justify-between gap-2 mb-1.5">
            <h3 class="font-bold text-base sm:text-lg text-stone-900 group-hover:text-[#023112] transition-colors leading-snug">
              ${item.name}
            </h3>
          </div>
          <p class="text-xs sm:text-sm text-stone-600 leading-relaxed line-clamp-2 mb-4 font-normal">
            ${item.description}
          </p>
        </div>
      </div>
      
      <div class="px-4 pb-4 sm:px-5 sm:pb-5 pt-0 flex items-center justify-between border-t border-stone-100">
        <div class="font-serif text-xl sm:text-2xl font-bold text-[#023112] font-tabular">
          ${item.price} <span class="text-base font-sans font-normal text-stone-500">₽</span>
        </div>
        <button 
          type="button"
          onclick="addToCart('${item.id}', this)"
          class="add-to-cart-btn relative min-h-[44px] px-5 py-2.5 rounded-xl bg-[#023112] text-white font-semibold hover:bg-[#064b1d] active:scale-95 transition-all text-xs sm:text-sm shadow-sm flex items-center gap-1.5 touch-target"
        >
          <svg class="w-4 h-4 text-[#e6c587]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M12 4v16m8-8H4"></path>
          </svg>
          <span>В заказ</span>
        </button>
      </div>
    `;
    container.appendChild(card);
  });
}

// Category Filter Click
function filterCategory(catKey) {
  currentCategory = catKey;
  
  document.querySelectorAll('.category-tab').forEach(tab => {
    if (tab.dataset.category === catKey) {
      tab.classList.add('active');
    } else {
      tab.classList.remove('active');
    }
  });

  initMenu();
}

// Cart Functions with Microanimations & Haptics
function addToCart(itemId, buttonEl) {
  let item = null;
  for (const cat of MENU_DATA) {
    const found = cat.items.find(i => i.id === itemId);
    if (found) { item = found; break; }
  }
  if (!item) return;

  const existing = cart.find(c => c.id === itemId);
  if (existing) {
    existing.count += 1;
  } else {
    cart.push({ ...item, count: 1 });
  }

  // 1. Mobile Haptic feedback (if hardware supported)
  if (typeof navigator !== 'undefined' && 'vibrate' in navigator) {
    try { navigator.vibrate([20, 30, 20]); } catch(e) {}
  }

  // 2. Button Visual Tactile Feedback
  if (buttonEl) {
    buttonEl.classList.add('anim-haptic-tap', 'btn-added-state');
    const originalText = buttonEl.querySelector('span')?.textContent || 'В заказ';
    
    // Spawn floating +1 flying pill
    const flyPill = document.createElement('span');
    flyPill.className = 'fly-pill bg-[#023112] text-[#e6c587] font-bold text-[11px] px-2 py-0.5 rounded-full border border-[#c5a059]/50 shadow-md';
    flyPill.textContent = '+1';
    flyPill.style.left = '50%';
    flyPill.style.marginLeft = '-14px';
    flyPill.style.top = '-10px';
    buttonEl.appendChild(flyPill);
    setTimeout(() => flyPill.remove(), 700);

    const spanEl = buttonEl.querySelector('span');
    if (spanEl) spanEl.textContent = 'Добавлено ✓';

    setTimeout(() => {
      buttonEl.classList.remove('anim-haptic-tap', 'btn-added-state');
      if (spanEl) spanEl.textContent = originalText;
    }, 900);
  }

  updateCartUI(true);
  showToast(`«${item.name}» добавлен в заказ`);
}

function updateItemCount(itemId, delta) {
  const item = cart.find(c => c.id === itemId);
  if (!item) return;

  item.count += delta;
  if (item.count <= 0) {
    cart = cart.filter(c => c.id !== itemId);
  }

  // Quick tactile bump on change
  if (typeof navigator !== 'undefined' && 'vibrate' in navigator) {
    try { navigator.vibrate(15); } catch(e) {}
  }

  updateCartUI(false);
}

function updateCartUI(triggerPopAnim = false) {
  const totalCount = cart.reduce((acc, i) => acc + i.count, 0);
  const totalPrice = cart.reduce((acc, i) => acc + (i.price * i.count), 0);

  // Update Badges with pop animation
  const badges = document.querySelectorAll('.cart-count-badge');
  badges.forEach(b => {
    b.textContent = totalCount;
    b.style.display = totalCount > 0 ? 'inline-flex' : 'none';
    if (triggerPopAnim && totalCount > 0) {
      b.classList.remove('badge-pop-anim');
      void b.offsetWidth; // trigger reflow
      b.classList.add('badge-pop-anim');
    }
  });

  // Update floating bar total
  const totalElements = document.querySelectorAll('.cart-total-price');
  totalElements.forEach(el => {
    el.textContent = `${totalPrice.toLocaleString('ru-RU')} ₽`;
  });

  // Mobile Bottom Bar visibility
  const floatingBar = document.getElementById('mobile-cart-bar');
  if (floatingBar) {
    if (totalCount > 0) {
      floatingBar.classList.remove('translate-y-full');
    } else {
      floatingBar.classList.add('translate-y-full');
    }
  }

  // Render Drawer Items
  renderCartDrawer(totalPrice, totalCount);
}

function renderCartDrawer(totalPrice, totalCount) {
  const list = document.getElementById('cart-items-list');
  const emptyState = document.getElementById('cart-empty-state');
  const footer = document.getElementById('cart-drawer-footer');

  if (!list) return;

  if (cart.length === 0) {
    list.innerHTML = '';
    if (emptyState) emptyState.style.display = 'block';
    if (footer) footer.style.display = 'none';
    return;
  }

  if (emptyState) emptyState.style.display = 'none';
  if (footer) footer.style.display = 'block';

  list.innerHTML = cart.map(item => `
    <div class="flex items-center gap-3.5 p-3.5 bg-stone-50/90 rounded-2xl border border-stone-200/80 hover:border-[#c5a059]/40 transition-colors">
      <img src="${item.image}" alt="${item.name}" class="w-16 h-16 rounded-xl object-cover flex-shrink-0 border border-stone-200/60">
      <div class="flex-1 min-w-0">
        <h4 class="font-bold text-stone-900 text-sm truncate">${item.name}</h4>
        <div class="text-[11px] text-stone-500 mb-1 flex items-center gap-1.5">
          <span>${item.weight}</span>
          <span>•</span>
          <span class="font-mono text-stone-500 bg-stone-200/60 px-1 rounded">${item.iikoId}</span>
        </div>
        <div class="text-sm font-bold text-[#023112] font-tabular">${(item.price * item.count).toLocaleString('ru-RU')} ₽</div>
      </div>
      <div class="flex items-center gap-1 bg-white border border-stone-200 rounded-xl p-1 shadow-sm">
        <button 
          type="button"
          onclick="updateItemCount('${item.id}', -1)" 
          class="cart-qty-btn text-stone-600 hover:text-red-700"
          aria-label="Уменьшить количество"
        >
          ${item.count === 1 ? '🗑' : '−'}
        </button>
        <span class="w-7 text-center text-xs font-bold text-stone-900 font-tabular">${item.count}</span>
        <button 
          type="button"
          onclick="updateItemCount('${item.id}', 1)" 
          class="cart-qty-btn text-stone-800 hover:text-[#023112]"
          aria-label="Увеличить количество"
        >
          +
        </button>
      </div>
    </div>
  `).join('');
}

// Open/Close Cart Drawer & Bottom Sheet
function toggleCartDrawer(open) {
  const drawer = document.getElementById('cart-drawer');
  const overlay = document.getElementById('cart-overlay');
  if (!drawer || !overlay) return;

  if (open) {
    drawer.classList.remove('translate-x-full');
    drawer.classList.add('drawer-open', 'sheet-open');
    overlay.classList.remove('hidden');
    document.body.classList.add('overflow-hidden');
  } else {
    drawer.classList.add('translate-x-full');
    drawer.classList.remove('drawer-open', 'sheet-open');
    overlay.classList.add('hidden');
    document.body.classList.remove('overflow-hidden');
  }
}

// Send Order to WhatsApp with Structured Receipt
function sendOrderToWhatsApp() {
  if (cart.length === 0) return;

  const nameInput = document.getElementById('order-name');
  const phoneInput = document.getElementById('order-phone');
  const addressInput = document.getElementById('order-address');
  const typeDelivery = document.querySelector('input[name="order-type"]:checked')?.value || 'доставка';

  const name = nameInput ? nameInput.value.trim() : '';
  const phone = phoneInput ? phoneInput.value.trim() : '';
  const address = addressInput ? addressInput.value.trim() : '';

  if (!phone) {
    alert('Пожалуйста, укажите ваш контактный номер телефона');
    if (phoneInput) phoneInput.focus();
    return;
  }

  const totalPrice = cart.reduce((acc, i) => acc + (i.price * i.count), 0);

  let message = `*Новый заказ из сайта ресторана BEINI*\n`;
  message += `━━━━━━━━━━━━━━━━━━━━━\n`;
  message += `👤 *Гость:* ${name || 'Гость'}\n`;
  message += `📞 *Телефон:* ${phone}\n`;
  message += `🛵 *Тип:* ${typeDelivery === 'доставка' ? 'Доставка по адресу' : 'Самовывоз (ул. Осканова, 21)'}\n`;
  if (typeDelivery === 'доставка' && address) {
    message += `📍 *Адрес:* ${address}\n`;
  }
  message += `━━━━━━━━━━━━━━━━━━━━━\n`;
  message += `📋 *СОСТАВ ЗАКАЗА:*\n\n`;

  cart.forEach((item, index) => {
    message += `${index + 1}. *${item.name}*\n`;
    message += `   ${item.count} шт. × ${item.price} ₽ = *${item.count * item.price} ₽* [${item.iikoId}]\n`;
  });

  message += `\n━━━━━━━━━━━━━━━━━━━━━\n`;
  message += `💳 *ИТОГО К ОПЛАТЕ: ${totalPrice.toLocaleString('ru-RU')} ₽*\n`;
  message += `_Заказ сформирован через сайт beini.rest_`;

  const phoneNum = '79204827777';
  const waUrl = `https://wa.me/${phoneNum}?text=${encodeURIComponent(message)}`;
  
  window.open(waUrl, '_blank');
}

// Booking Modal Controls & Ergonomics
function initBookingDefaults() {
  // Set min date to today
  const dateInput = document.getElementById('book-date');
  if (dateInput) {
    const today = new Date().toISOString().split('T')[0];
    dateInput.min = today;
    if (!dateInput.value) dateInput.value = today;
  }
}

function setBookingGuests(num, el) {
  const guestInput = document.getElementById('book-guests');
  if (guestInput) guestInput.value = num;

  document.querySelectorAll('.guest-chip').forEach(c => c.classList.remove('active'));
  if (el) el.classList.add('active');
}

function setBookingTime(timeStr, el) {
  const timeInput = document.getElementById('book-time');
  if (timeInput) timeInput.value = timeStr;

  document.querySelectorAll('.time-chip').forEach(c => c.classList.remove('active'));
  if (el) el.classList.add('active');
}

function selectBookingZone(zoneValue, cardEl) {
  const zoneSelect = document.getElementById('book-zone');
  if (zoneSelect) zoneSelect.value = zoneValue;

  document.querySelectorAll('.zone-option-card').forEach(c => c.classList.remove('selected'));
  if (cardEl) cardEl.classList.add('selected');
}

function toggleBookingModal(open) {
  const modal = document.getElementById('booking-modal');
  if (!modal) return;

  if (open) {
    initBookingDefaults();
    modal.classList.remove('hidden');
    modal.classList.add('flex');
    document.body.classList.add('overflow-hidden');
  } else {
    modal.classList.add('hidden');
    modal.classList.remove('flex');
    document.body.classList.remove('overflow-hidden');
  }
}

function submitBooking(event) {
  event.preventDefault();
  const name = document.getElementById('book-name').value;
  const phone = document.getElementById('book-phone').value;
  const zone = document.getElementById('book-zone').value;
  const date = document.getElementById('book-date').value;
  const time = document.getElementById('book-time').value;
  const guests = document.getElementById('book-guests').value;

  let msg = `*Бронирование столика / кабинки в BEINI*\n`;
  msg += `━━━━━━━━━━━━━━━━━━━━━\n`;
  msg += `👤 Имя: ${name}\n`;
  msg += `📞 Телефон: ${phone}\n`;
  msg += `🛋 Зона: ${zone}\n`;
  msg += `📅 Дата: ${date}\n`;
  msg += `⏰ Время: ${time}\n`;
  msg += `👥 Количество гостей: ${guests} чел.\n`;
  msg += `━━━━━━━━━━━━━━━━━━━━━\n`;
  msg += `_Заявка на бронь с сайта beini.rest_`;

  const phoneNum = '79204827777';
  const waUrl = `https://wa.me/${phoneNum}?text=${encodeURIComponent(msg)}`;
  
  toggleBookingModal(false);
  window.open(waUrl, '_blank');
}

// iiko Cloud API Inspector Modal (Для демонстрации руководству)
function toggleIikoModal(open) {
  const modal = document.getElementById('iiko-modal');
  if (!modal) return;

  if (open) {
    const payloadEl = document.getElementById('iiko-json-payload');
    if (payloadEl) {
      const iikoPayload = {
        organizationId: "beini-rest-nazran-org-001",
        terminalGroupId: "pos-terminal-delivery-01",
        order: {
          phone: document.getElementById('order-phone')?.value || "+7 (928) 000-00-00",
          customer: {
            name: document.getElementById('order-name')?.value || "Гость Ресторана"
          },
          orderType: "Delivery",
          deliveryPoint: {
            address: {
              street: { name: document.getElementById('order-address')?.value || "ул. С.С. Осканова, Назрань" }
            }
          },
          items: cart.length > 0 ? cart.map(i => ({
            id: i.iikoId,
            name: i.name,
            amount: i.count,
            price: i.price,
            sum: i.price * i.count
          })) : [
            { id: "iiko-meat-0101", name: "Фирменное ассорти BEINI", amount: 1, price: 1850 }
          ],
          sourceKey: "BEINI_WEB_ECOSYSTEM",
          comment: "Заказ сформирован через онлайн-сервис ресторана BEINI"
        }
      };
      payloadEl.textContent = JSON.stringify(iikoPayload, null, 2);
    }
    modal.classList.remove('hidden');
    modal.classList.add('flex');
  } else {
    modal.classList.add('hidden');
    modal.classList.remove('flex');
  }
}

// Toast notification helper with Glassmorphism Finish
function showToast(text) {
  let toast = document.getElementById('toast-notification');
  if (!toast) {
    toast = document.createElement('div');
    toast.id = 'toast-notification';
    toast.className = 'fixed bottom-24 left-1/2 -translate-x-1/2 bg-stone-900/95 backdrop-blur-md text-white border border-[#c5a059]/40 px-5 py-3 rounded-2xl text-xs sm:text-sm font-medium shadow-2xl z-50 transition-all duration-300 pointer-events-none opacity-0 flex items-center gap-2';
    document.body.appendChild(toast);
  }

  toast.innerHTML = `<span class="text-[#e6c587]">✦</span> <span>${text}</span>`;
  toast.classList.remove('opacity-0', 'translate-y-2');
  toast.classList.add('opacity-100', 'translate-y-0');

  setTimeout(() => {
    toast.classList.remove('opacity-100', 'translate-y-0');
    toast.classList.add('opacity-0', 'translate-y-2');
  }, 2300);
}

function initEventListeners() {
  // Order type switcher (delivery vs pickup)
  const deliveryRadios = document.querySelectorAll('input[name="order-type"]');
  const addressContainer = document.getElementById('address-input-container');

  deliveryRadios.forEach(radio => {
    radio.addEventListener('change', (e) => {
      if (addressContainer) {
        addressContainer.style.display = e.target.value === 'самовывоз' ? 'none' : 'block';
      }
    });
  });

  // Close modals on Escape key
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      toggleCartDrawer(false);
      toggleBookingModal(false);
      toggleIikoModal(false);
    }
  });
}
