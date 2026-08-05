(function () {
  'use strict';

  // API base is served from the same origin by the gateway.
  const API_BASE = '/v1';
  const WS_SCHEME = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const WS_BASE = WS_SCHEME + '//' + window.location.host;

  // State ------------------------------------------------------------
  let accessToken = null;
  let refreshToken = null;
  let currentRole = null;
  let ws = null;
  let loggedOut = false;
  let reconnectDelay = 1000;
  let reconnectTimer = null;
  let pendingBids = new Map(); // bid_id -> status element
  let pollTimer = null;

  // DOM refs ---------------------------------------------------------
  const statusEl = document.getElementById('status');
  const eventLogEl = document.getElementById('event-log');
  const emailEl = document.getElementById('email');
  const passwordEl = document.getElementById('password');
  const roleEl = document.getElementById('role');
  const productNameEl = document.getElementById('product-name');
  const productDescEl = document.getElementById('product-description');
  const productQtyEl = document.getElementById('product-quantity');
  const productIdEl = document.getElementById('product-id');
  const startPriceEl = document.getElementById('start-price');
  const minIncrementEl = document.getElementById('min-increment');
  const startsAtEl = document.getElementById('starts-at');
  const endsAtEl = document.getElementById('ends-at');
  const auctionIdEl = document.getElementById('auction-id');
  const bidAmountEl = document.getElementById('bid-amount');
  const idempotencyKeyEl = document.getElementById('idempotency-key');

  const btnRegister = document.getElementById('btn-register');
  const btnLogin = document.getElementById('btn-login');
  const btnLogout = document.getElementById('btn-logout');
  const btnCreateProduct = document.getElementById('btn-create-product');
  const btnCreateAuction = document.getElementById('btn-create-auction');
  const btnConnect = document.getElementById('btn-connect');
  const btnDisconnect = document.getElementById('btn-disconnect');
  const btnNewKey = document.getElementById('btn-new-key');
  const btnSubmitBid = document.getElementById('btn-submit-bid');
  const btnClearLog = document.getElementById('btn-clear-log');

  // Helpers ----------------------------------------------------------
  function setStatus(text) {
    statusEl.textContent = text;
  }

  function logEvent(text, type) {
    const item = document.createElement('li');
    item.textContent = new Date().toLocaleTimeString() + ' ' + text;
    if (type) item.className = type;
    eventLogEl.appendChild(item);
    eventLogEl.scrollTop = eventLogEl.scrollHeight;
  }

  function generateUUID() {
    return crypto.randomUUID();
  }

  function newIdempotencyKey() {
    idempotencyKeyEl.value = generateUUID();
  }

  function setEnabled() {
    const ok = !!accessToken;
    btnLogout.disabled = !ok;
    btnCreateProduct.disabled = !ok;
    btnCreateAuction.disabled = !ok;
    btnConnect.disabled = !ok;
    btnDisconnect.disabled = !ok || !ws;
    btnSubmitBid.disabled = !ok;
  }

  function setSession(tokens, role) {
    accessToken = tokens ? tokens.access_token : null;
    refreshToken = tokens ? tokens.refresh_token : null;
    currentRole = role || null;
    loggedOut = !tokens;
    setStatus(tokens ? `Logged in as ${role || 'user'}.` : 'Not logged in.');
    setEnabled();
  }

  function clearSession() {
    accessToken = null;
    refreshToken = null;
    currentRole = null;
  }

  function toISO(input) {
    if (!input) return null;
    const d = new Date(input);
    if (isNaN(d.getTime())) return null;
    return d.toISOString();
  }

  // API --------------------------------------------------------------
  async function api(method, path, body, headers = {}, retryWithRefresh = true) {
    const url = API_BASE + path;
    const options = {
      method,
      headers: { 'Content-Type': 'application/json', ...headers },
    };
    if (accessToken) {
      options.headers.Authorization = 'Bearer ' + accessToken;
    }
    if (body !== undefined && body !== null) {
      options.body = JSON.stringify(body);
    }

    let response = await fetch(url, options);

    if (response.status === 401 && retryWithRefresh && refreshToken) {
      const refreshed = await refreshAccessToken();
      if (refreshed) {
        options.headers.Authorization = 'Bearer ' + accessToken;
        response = await fetch(url, options);
      }
    }

    if (!response.ok) {
      const data = await response.json().catch(() => ({}));
      throw new Error((data.code || response.status) + ': ' + (data.message || response.statusText));
    }

    if (response.status === 204) return null;
    return response.json();
  }

  async function refreshAccessToken() {
    try {
      const data = await api('POST', '/auth/refresh', { refresh_token: refreshToken }, false);
      accessToken = data.access_token;
      refreshToken = data.refresh_token;
      return true;
    } catch (err) {
      setStatus('Session expired; please log in again. (' + err.message + ')');
      clearSession();
      setEnabled();
      return false;
    }
  }

  // WebSocket --------------------------------------------------------
  function connectWebSocket() {
    const auctionId = auctionIdEl.value.trim();
    if (!auctionId) {
      setStatus('Enter an auction ID before connecting.');
      return;
    }
    if (!accessToken) {
      setStatus('Log in before connecting.');
      return;
    }
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      setStatus('Already connected or connecting.');
      return;
    }

    const url = WS_BASE + '/v1/auctions/' + encodeURIComponent(auctionId) + '/live?access_token=' + encodeURIComponent(accessToken);
    setStatus('Connecting WebSocket for auction ' + auctionId + '...');
    try {
      ws = new WebSocket(url, ['auction-live']);
    } catch (err) {
      setStatus('WebSocket error: ' + err.message);
      scheduleReconnect();
      return;
    }

    ws.onopen = function () {
      setStatus('WebSocket connected to auction ' + auctionId + '.');
      logEvent('Connected to auction ' + auctionId);
      reconnectDelay = 1000;
      setEnabled();
    };

    ws.onmessage = function (event) {
      let envelope;
      try {
        envelope = JSON.parse(event.data);
      } catch (err) {
        logEvent('Malformed WebSocket message: ' + event.data);
        return;
      }
      const type = envelope.event_type;
      const payload = envelope.payload || {};

      if (type === 'auction.bid.placed.v1') {
        const status = payload.status || 'unknown';
        const bidId = payload.bid_id;
        if (bidId && pendingBids.has(bidId)) {
          pendingBids.get(bidId).textContent = status;
          pendingBids.delete(bidId);
        }
        logEvent(
          'Bid ' + bidId + ' ' + status +
          ' for ' + (payload.amount_cents / 100).toFixed(2) +
          (payload.rejection_code ? ' (' + payload.rejection_code + ')' : ''),
          'event-bid'
        );
      } else if (type === 'auction.closed.v1') {
        logEvent(
          'Auction closed. Winner: ' + (payload.winner_id || 'none') +
          ' at ' + (payload.final_price_cents / 100).toFixed(2),
          'event-closed'
        );
      } else {
        logEvent('Event: ' + type + ' ' + JSON.stringify(payload));
      }
    };

    ws.onclose = function () {
      ws = null;
      setStatus('WebSocket disconnected.');
      setEnabled();
      startPendingPoll();
      if (!loggedOut) {
        scheduleReconnect();
      }
    };

    ws.onerror = function () {
      logEvent('WebSocket error.');
    };
  }

  function disconnectWebSocket() {
    if (ws) {
      const s = ws;
      ws = null;
      s.close();
    }
    if (reconnectTimer) {
      window.clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  }

  function scheduleReconnect() {
    if (reconnectTimer || loggedOut) return;
    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = null;
      connectWebSocket();
    }, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, 10000);
  }

  // Pending bid polling (only when WS is disconnected) -----------------
  function startPendingPoll() {
    if (pollTimer) return;
    pollTimer = window.setInterval(async () => {
      if (ws && ws.readyState === WebSocket.OPEN) {
        stopPendingPoll();
        return;
      }
      if (pendingBids.size === 0) {
        stopPendingPoll();
        return;
      }
      for (const [bidId, statusEl] of pendingBids) {
        try {
          const bid = await api('GET', '/bids/' + encodeURIComponent(bidId), null, true);
          statusEl.textContent = bid.status || 'unknown';
          if (bid.status && bid.status !== 'pending') {
            pendingBids.delete(bidId);
          }
        } catch (err) {
          // leave pending; network may be down.
          logEvent('Poll bid ' + bidId + ' failed: ' + err.message);
        }
      }
    }, 2000);
  }

  function stopPendingPoll() {
    if (pollTimer) {
      window.clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  // Handlers ---------------------------------------------------------
  async function register() {
    const email = emailEl.value.trim();
    const password = passwordEl.value;
    const role = roleEl.value;
    if (!email || !password) {
      setStatus('Email and password are required.');
      return;
    }
    try {
      await api('POST', '/auth/register', { email, password, role }, false);
      setStatus('Registered. Now log in.');
    } catch (err) {
      setStatus('Registration failed: ' + err.message);
    }
  }

  async function login() {
    const email = emailEl.value.trim();
    const password = passwordEl.value;
    const role = roleEl.value;
    if (!email || !password) {
      setStatus('Email and password are required.');
      return;
    }
    try {
      const tokens = await api('POST', '/auth/login', { email, password }, false);
      setSession(tokens, role);
      logEvent('Logged in as ' + email + ' (' + role + ')');
      newIdempotencyKey();
    } catch (err) {
      setStatus('Login failed: ' + err.message);
    }
  }

  function logout() {
    disconnectWebSocket();
    const rt = refreshToken;
    clearSession();
    setSession(null);
    pendingBids.clear();
    stopPendingPoll();
    if (rt) {
      api('POST', '/auth/logout', { refresh_token: rt }, false).catch(() => {});
    }
    setStatus('Logged out.');
  }

  async function createProduct() {
    const name = productNameEl.value.trim();
    const description = productDescEl.value.trim();
    const quantity = parseInt(productQtyEl.value, 10);
    if (!name || !description || !quantity) {
      setStatus('Name, description, and quantity are required.');
      return;
    }
    try {
      const product = await api('POST', '/products', { name, description, quantity });
      productIdEl.value = product.id;
      setStatus('Product created: ' + product.id);
      logEvent('Created product ' + product.id);
    } catch (err) {
      setStatus('Create product failed: ' + err.message);
    }
  }

  async function createAuction() {
    const productId = productIdEl.value.trim();
    const startingPriceCents = parseInt(startPriceEl.value, 10);
    const minimumIncrementCents = parseInt(minIncrementEl.value, 10);
    let startsAt = toISO(startsAtEl.value);
    let endsAt = toISO(endsAtEl.value);
    if (!startsAt) {
      const now = new Date();
      startsAt = now.toISOString();
      startsAtEl.value = now.toISOString().slice(0, 16);
    }
    if (!endsAt) {
      const end = new Date(Date.now() + 60000);
      endsAt = end.toISOString();
      endsAtEl.value = end.toISOString().slice(0, 16);
    }
    if (!productId || !startingPriceCents || !minimumIncrementCents) {
      setStatus('Product ID, start price, and minimum increment are required.');
      return;
    }
    try {
      const auction = await api('POST', '/auctions', {
        product_id: productId,
        starting_price_cents: startingPriceCents,
        minimum_increment_cents: minimumIncrementCents,
        starts_at: startsAt,
        ends_at: endsAt,
        anti_sniping_window_seconds: 0,
        anti_sniping_extension_seconds: 0,
      });
      auctionIdEl.value = auction.id;
      setStatus('Auction created: ' + auction.id);
      logEvent('Created auction ' + auction.id);
    } catch (err) {
      setStatus('Create auction failed: ' + err.message);
    }
  }

  async function submitBid() {
    const auctionId = auctionIdEl.value.trim();
    const amountCents = parseInt(bidAmountEl.value, 10);
    const idempotencyKey = idempotencyKeyEl.value.trim();
    if (!auctionId || !amountCents || !idempotencyKey) {
      setStatus('Auction ID, amount, and idempotency key are required.');
      return;
    }
    try {
      const result = await api('POST', '/auctions/' + encodeURIComponent(auctionId) + '/bids', {
        amount_cents: amountCents,
      }, { 'Idempotency-Key': idempotencyKey });
      const bidId = result.bid_id;
      setStatus('Bid submitted: ' + bidId + ' (' + result.status + ')');
      logEvent('Bid ' + bidId + ' pending');

      const item = document.createElement('li');
      item.textContent = new Date().toLocaleTimeString() + ' Bid ' + bidId + ' status: ';
      const statusSpan = document.createElement('span');
      statusSpan.textContent = 'pending';
      item.appendChild(statusSpan);
      eventLogEl.appendChild(item);
      eventLogEl.scrollTop = eventLogEl.scrollHeight;

      pendingBids.set(bidId, statusSpan);
      newIdempotencyKey();
      if (!ws || ws.readyState !== WebSocket.OPEN) {
        startPendingPoll();
      }
    } catch (err) {
      setStatus('Bid failed: ' + err.message);
    }
  }

  function clearLog() {
    eventLogEl.textContent = '';
  }

  // Event wiring -----------------------------------------------------
  btnRegister.addEventListener('click', register);
  btnLogin.addEventListener('click', login);
  btnLogout.addEventListener('click', logout);
  btnCreateProduct.addEventListener('click', createProduct);
  btnCreateAuction.addEventListener('click', createAuction);
  btnConnect.addEventListener('click', connectWebSocket);
  btnDisconnect.addEventListener('click', disconnectWebSocket);
  btnNewKey.addEventListener('click', newIdempotencyKey);
  btnSubmitBid.addEventListener('click', submitBid);
  btnClearLog.addEventListener('click', clearLog);

  // Defaults ---------------------------------------------------------
  newIdempotencyKey();
  setEnabled();
})();
