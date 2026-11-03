(function () {
  'use strict';
  const $ = id => document.getElementById(id);
  const client = window.BEINIAccess;
  let busy = false;
  async function refresh() {
    try {
      const data = await client.read();
      $('tester-form').hidden = data.authenticated || !data.loginAvailable;
      $('tester-session').hidden = !data.authenticated;
      $('tester-note').textContent = (data.mock ? 'MOCK: учебный стенд, заказы не отправляются в ресторан. ' : '') +
        (data.authenticated
          ? (data.mode === 'closed' ? 'Вы вошли. Отправка закрыта сервером для всех.' : 'Вы вошли. ' + (data.mock ? 'Оформление использует искусственную кассу.' : 'Режим тестировщика. Заказ будет реально отправлен в ресторан.'))
          : (data.loginAvailable ? 'Защищённый доступ к тестовой эксплуатации.' : 'Вход владельца пока не настроен на сервере.'));
    } catch (e) {
      $('tester-form').hidden = true;
      $('tester-session').hidden = true;
      $('tester-note').textContent = 'Не удалось проверить доступ. Обновите страницу. Для входа нужен HTTPS или настроенный локальный стенд.';
    }
  }
  $('tester-form').addEventListener('submit', async event => {
    event.preventDefault();
    if (busy) return;
    busy = true; $('tester-submit').disabled = true; $('tester-error').textContent = '';
    const input = $('tester-password');
    let password = input.value;
    input.value = '';
    try {
      const response = await client.post('/api/tester/login', JSON.stringify({ password }));
      password = '';
      const data = await response.json();
      if (!response.ok) {
        $('tester-error').textContent = data.error || 'Вход не выполнен. Повторите позже.';
        input.setAttribute('aria-invalid', 'true'); input.focus();
      } else {
        input.removeAttribute('aria-invalid'); client.broadcast();
      }
      await refresh();
    } catch (e) { $('tester-error').textContent = 'Нет ответа сервера. Обновите страницу и попробуйте снова.'; }
    finally { password = ''; busy = false; $('tester-submit').disabled = false; }
  });
  $('tester-logout').addEventListener('click', async () => {
    if (busy) return;
    busy = true; $('tester-logout').disabled = true;
    try {
      const response = await client.post('/api/tester/logout', '{}');
      if (!response.ok) throw new Error('logout');
      client.broadcast(); await refresh();
    } catch (e) { $('tester-error').textContent = 'Выход не подтверждён сервером. Обновите страницу и повторите.'; }
    finally { busy = false; $('tester-logout').disabled = false; }
  });
  window.addEventListener('focus', refresh);
  window.addEventListener('storage', event => { if (event.key === 'beini_access_changed') refresh(); });
  document.addEventListener('visibilitychange', () => { if (!document.hidden) refresh(); });
  refresh();
})();
