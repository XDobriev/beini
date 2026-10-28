// State Management
let currentCategory = 'all';
let cart = [];

// DOM Ready
document.addEventListener('DOMContentLoaded', () => {
  initMenu();
  initEventListeners();
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
    card.className = 'dish-card bg-white rounded-2xl overflow-hidden border border-stone-200/80 shadow-sm flex flex-col justify-between group';
    
    card.innerHTML = `
      <div>
        <div class="relative h-48 sm:h-52 overflow-hidden bg-stone-100">
          <img 
            src="${item.image}" 
            alt="${item.name}" 
            class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500"
            loading="lazy"
          >
          <div class="absolute top-3 left-3 flex flex-wrap gap-1.5">
            ${item.badge ? `<span class="px-2.5 py-1 text-xs font-semibold rounded-full bg-[#023112] text-white shadow">${item.badge}</span>` : ''}
            ${item.isHalal ? `<span class="px-2.5 py-1 text-xs font-semibold rounded-full bg-emerald-800 text-emerald-100 shadow flex items-center gap-1">Халяль</span>` : ''}
          </div>
          <span class="absolute bottom-3 right-3 px-2.5 py-1 text-xs font-medium rounded-md bg-black/60 backdrop-blur-md text-white">
            ${item.weight}
          </span>
        </div>
        <div class="p-4 sm:p-5">
          <div class="flex items-start justify-between gap-2 mb-1.5">
            <h3 class="font-bold text-lg text-stone-900 group-hover:text-[#023112] transition-colors leading-snug">
              ${item.name}
            </h3>
          </div>
          <p class="text-sm text-stone-500 leading-relaxed line-clamp-2 mb-4">
            ${item.description}
          </p>
        </div>
      </div>
      
      <div class="px-4 pb-4 sm:px-5 sm:pb-5 pt-0 flex items-center justify-between border-t border-stone-100">
        <div class="font-serif text-xl sm:text-2xl font-bold text-[#023112]">
          ${item.price} <span class="text-base font-sans font-normal text-stone-500">₽</span>
        </div>
        <button 
          onclick="addToCart('${item.id}')"
          class="flex items-center gap-1.5 px-4 py-2 rounded-xl bg-[#023112] text-white font-medium hover:bg-[#064b1d] active:scale-95 transition-all text-sm shadow-sm"
        >
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4"></path>
          </svg>
          В заказ
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
      tab.classList.add('active', 'bg-[#023112]', 'text-white', 'shadow');
      tab.classList.remove('bg-white', 'text-stone-700');
    } else {
      tab.classList.remove('active', 'bg-[#023112]', 'text-white', 'shadow');
      tab.classList.add('bg-white', 'text-stone-700');
    }
  });

  initMenu();
}

// Cart Functions
function addToCart(itemId) {
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

  updateCartUI();
  showToast(`«${item.name}» добавлен в заказ`);
}

function updateItemCount(itemId, delta) {
  const item = cart.find(c => c.id === itemId);
  if (!item) return;

  item.count += delta;
  if (item.count <= 0) {
    cart = cart.filter(c => c.id !== itemId);
  }

  updateCartUI();
}

function updateCartUI() {
  const totalCount = cart.reduce((acc, i) => acc + i.count, 0);
  const totalPrice = cart.reduce((acc, i) => acc + (i.price * i.count), 0);

  // Update Badges
  const badges = document.querySelectorAll('.cart-count-badge');
  badges.forEach(b => {
    b.textContent = totalCount;
    b.style.display = totalCount > 0 ? 'inline-flex' : 'none';
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
    <div class="flex items-center gap-3 p-3 bg-stone-50 rounded-xl border border-stone-200/60">
      <img src="${item.image}" alt="${item.name}" class="w-16 h-16 rounded-lg object-cover flex-shrink-0">
      <div class="flex-1 min-w-0">
        <h4 class="font-semibold text-stone-900 text-sm truncate">${item.name}</h4>
        <div class="text-xs text-stone-500 mb-1.5">${item.weight} • iiko: <span class="font-mono text-stone-600">${item.iikoId}</span></div>
        <div class="text-sm font-bold text-[#023112]">${(item.price * item.count).toLocaleString('ru-RU')} ₽</div>
      </div>
      <div class="flex items-center gap-1.5 bg-white border border-stone-200 rounded-lg p-1">
        <button onclick="updateItemCount('${item.id}', -1)" class="w-6 h-6 flex items-center justify-center rounded text-stone-600 hover:bg-stone-100 font-bold text-sm">−</button>
        <span class="w-6 text-center text-xs font-bold text-stone-800">${item.count}</span>
        <button onclick="updateItemCount('${item.id}', 1)" class="w-6 h-6 flex items-center justify-center rounded text-stone-600 hover:bg-stone-100 font-bold text-sm">+</button>
      </div>
    </div>
  `).join('');
}

// Open/Close Cart Drawer
function toggleCartDrawer(open) {
  const drawer = document.getElementById('cart-drawer');
  const overlay = document.getElementById('cart-overlay');
  if (!drawer || !overlay) return;

  if (open) {
    drawer.classList.remove('translate-x-full');
    overlay.classList.remove('hidden');
    document.body.classList.add('overflow-hidden');
  } else {
    drawer.classList.add('translate-x-full');
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

// Booking Modal
function toggleBookingModal(open) {
  const modal = document.getElementById('booking-modal');
  if (!modal) return;

  if (open) {
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
  msg += `_Заявка на бронь с сайта_`;

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

// Toast notification helper
function showToast(text) {
  let toast = document.getElementById('toast-notification');
  if (!toast) {
    toast = document.createElement('div');
    toast.id = 'toast-notification';
    toast.className = 'fixed bottom-24 left-1/2 -translate-x-1/2 bg-stone-900 text-white px-5 py-2.5 rounded-full text-xs sm:text-sm font-medium shadow-xl z-50 transition-opacity duration-300 pointer-events-none opacity-0';
    document.body.appendChild(toast);
  }

  toast.textContent = text;
  toast.classList.remove('opacity-0');
  toast.classList.add('opacity-100');

  setTimeout(() => {
    toast.classList.remove('opacity-100');
    toast.classList.add('opacity-0');
  }, 2200);
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
}
