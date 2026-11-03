// CSRF stays in memory; the session is an HttpOnly server cookie.
(function () {
  'use strict';
  let snapshot = null;
  let reading = null;
  async function read() {
    if (reading) return reading;
    reading = (async () => {
      const response = await fetch('/api/access', { credentials: 'same-origin', cache: 'no-store', signal: AbortSignal.timeout(10000) });
      const data = await response.json();
      if (!response.ok || !data.csrfToken) throw new Error('access_unavailable');
      snapshot = data;
      return data;
    })();
    try { return await reading; } finally { reading = null; }
  }
  async function post(path, body, headers = {}) {
    // Re-read the cookie binding: another tab may have logged in or out.
    await read();
    if (path === '/api/order' && (!snapshot.canOrder || snapshot.authenticated && Date.now() >= Date.parse(snapshot.sessionExpiresAt))) throw new Error('ordering_closed');
    return fetch(path, {
      method: 'POST', credentials: 'same-origin', cache: 'no-store',
      headers: { ...headers, 'Content-Type': 'application/json', 'X-CSRF-Token': snapshot.csrfToken },
      body, signal: AbortSignal.timeout(path === '/api/order' ? 40000 : 10000)
    });
  }
  function broadcast() {
    try { localStorage.setItem('beini_access_changed', String(Date.now())); } catch (e) { /* focus also refreshes access */ }
  }
  window.BEINIAccess = { read, post, broadcast };
})();
