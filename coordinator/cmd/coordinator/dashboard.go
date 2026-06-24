package main

import (
	"net/http"
)

func (s *server) adminDashboardRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/dashboard", http.StatusFound)
}

func (s *server) adminDashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(adminDashboardHTML))
}

const adminDashboardHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>mi admin dashboard</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f7f8fb;
      --panel: #ffffff;
      --ink: #1b2430;
      --muted: #667085;
      --line: #d9dee7;
      --blue: #2854c5;
      --green: #147a50;
      --red: #b42318;
      --amber: #a15c07;
      --teal: #157e8a;
      --shadow: 0 1px 2px rgba(16, 24, 40, 0.06);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--ink);
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      font-size: 14px;
      line-height: 1.45;
    }
    header {
      position: sticky;
      top: 0;
      z-index: 5;
      border-bottom: 1px solid var(--line);
      background: rgba(255, 255, 255, 0.96);
      backdrop-filter: blur(10px);
    }
    .topbar {
      max-width: 1440px;
      margin: 0 auto;
      padding: 14px 20px;
      display: grid;
      grid-template-columns: minmax(180px, 1fr) auto;
      gap: 16px;
      align-items: center;
    }
    h1 {
      margin: 0;
      font-size: 18px;
      font-weight: 700;
      letter-spacing: 0;
    }
    .subtitle {
      margin-top: 2px;
      color: var(--muted);
      font-size: 12px;
    }
    .controls {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      justify-content: flex-end;
      align-items: center;
    }
    input, select, button {
      height: 34px;
      border-radius: 6px;
      border: 1px solid var(--line);
      background: #fff;
      color: var(--ink);
      font: inherit;
    }
    input {
      padding: 0 10px;
      min-width: 180px;
    }
    input[type="number"] {
      min-width: 118px;
      width: 128px;
    }
    select {
      padding: 0 30px 0 10px;
      min-width: 160px;
    }
    button {
      padding: 0 12px;
      cursor: pointer;
      font-weight: 600;
    }
    button.primary {
      border-color: var(--blue);
      background: var(--blue);
      color: #fff;
    }
    button.danger {
      border-color: #f5c2bd;
      color: var(--red);
    }
    button:disabled {
      cursor: not-allowed;
      opacity: 0.55;
    }
    label.inline {
      display: flex;
      align-items: center;
      gap: 6px;
      color: var(--muted);
      font-size: 12px;
      white-space: nowrap;
    }
    label.inline input[type="checkbox"] {
      width: 16px;
      height: 16px;
      min-width: 16px;
    }
    main {
      max-width: 1440px;
      margin: 0 auto;
      padding: 18px 20px 36px;
    }
    .statusline {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      justify-content: space-between;
      gap: 10px;
      color: var(--muted);
      font-size: 12px;
      margin-bottom: 14px;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      min-height: 24px;
      padding: 2px 8px;
      border-radius: 999px;
      border: 1px solid var(--line);
      background: #fff;
      color: var(--muted);
      font-size: 12px;
      font-weight: 600;
    }
    .pill.good { border-color: #b6e3ce; color: var(--green); background: #effaf4; }
    .pill.bad { border-color: #f5c2bd; color: var(--red); background: #fff3f1; }
    .pill.warn { border-color: #f3d7a3; color: var(--amber); background: #fff8e8; }
    .grid {
      display: grid;
      gap: 12px;
    }
    .metrics {
      grid-template-columns: repeat(6, minmax(140px, 1fr));
      margin-bottom: 12px;
    }
    .metric, .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      box-shadow: var(--shadow);
    }
    .metric {
      min-height: 98px;
      padding: 14px;
    }
    .metric .label {
      color: var(--muted);
      font-size: 12px;
      font-weight: 650;
      text-transform: uppercase;
      letter-spacing: 0.04em;
    }
    .metric .value {
      margin-top: 8px;
      font-size: 24px;
      font-weight: 760;
      letter-spacing: 0;
      white-space: nowrap;
    }
    .metric .sub {
      margin-top: 4px;
      color: var(--muted);
      font-size: 12px;
      min-height: 18px;
    }
    .columns {
      grid-template-columns: minmax(0, 1.35fr) minmax(360px, 0.65fr);
      align-items: start;
    }
    .panel {
      overflow: hidden;
      margin-bottom: 12px;
    }
    .panel h2 {
      margin: 0;
      padding: 12px 14px;
      border-bottom: 1px solid var(--line);
      font-size: 14px;
      font-weight: 750;
      letter-spacing: 0;
      display: flex;
      justify-content: space-between;
      gap: 10px;
      align-items: center;
    }
    .panel-body { padding: 14px; }
    .table-wrap { overflow-x: auto; }
    table {
      width: 100%;
      border-collapse: collapse;
      min-width: 760px;
    }
    th, td {
      padding: 10px 12px;
      border-bottom: 1px solid #edf0f5;
      text-align: left;
      vertical-align: top;
      white-space: nowrap;
    }
    th {
      color: var(--muted);
      font-size: 11px;
      font-weight: 750;
      text-transform: uppercase;
      letter-spacing: 0.04em;
      background: #fbfcfe;
    }
    tr:last-child td { border-bottom: 0; }
    .stack {
      display: flex;
      flex-direction: column;
      gap: 3px;
      white-space: normal;
    }
    .strong { font-weight: 700; }
    .muted { color: var(--muted); }
    .mono {
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
      font-size: 12px;
    }
    .chips {
      display: flex;
      flex-wrap: wrap;
      gap: 5px;
      max-width: 360px;
    }
    .chip {
      border-radius: 999px;
      border: 1px solid var(--line);
      background: #f8fafc;
      padding: 2px 7px;
      font-size: 11px;
      color: #344054;
    }
    .progress {
      width: 120px;
      height: 8px;
      border-radius: 999px;
      background: #edf0f5;
      overflow: hidden;
    }
    .progress > span {
      display: block;
      height: 100%;
      background: var(--blue);
    }
    .progress > span.good { background: var(--green); }
    .progress > span.warn { background: var(--amber); }
    .progress > span.bad { background: var(--red); }
    .split {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 10px;
    }
    .kv {
      display: grid;
      grid-template-columns: minmax(120px, 0.75fr) minmax(0, 1fr);
      gap: 8px;
      padding: 8px 0;
      border-bottom: 1px solid #edf0f5;
    }
    .kv:last-child { border-bottom: 0; }
    .kv .k { color: var(--muted); }
    .empty {
      padding: 18px 14px;
      color: var(--muted);
      text-align: center;
    }
    .errorbox {
      display: none;
      margin-bottom: 12px;
      border: 1px solid #f5c2bd;
      background: #fff3f1;
      color: var(--red);
      border-radius: 8px;
      padding: 10px 12px;
      white-space: pre-wrap;
    }
    .secretbox {
      display: none;
      margin-bottom: 12px;
      border: 1px solid #b6e3ce;
      background: #effaf4;
      color: #14533a;
      border-radius: 8px;
      padding: 12px;
      box-shadow: var(--shadow);
    }
    .secret-head {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      gap: 12px;
    }
    .secret-note {
      margin-top: 2px;
      font-size: 12px;
      color: #147a50;
    }
    .secret-actions, .action-buttons, .form-actions {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
    }
    .secret-value {
      margin-top: 10px;
      padding: 9px 10px;
      border: 1px solid #b6e3ce;
      border-radius: 6px;
      background: #fff;
      color: var(--ink);
      word-break: break-all;
    }
    .operator-forms {
      display: grid;
      gap: 16px;
    }
    .admin-form {
      display: grid;
      gap: 10px;
    }
    .form-title {
      font-weight: 700;
    }
    .form-fields {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 8px;
    }
    .form-fields label {
      display: flex;
      flex-direction: column;
      gap: 4px;
      min-width: 0;
      color: var(--muted);
      font-size: 12px;
      font-weight: 650;
    }
    .form-fields input, .form-fields select {
      width: 100%;
      min-width: 0;
    }
    .form-actions {
      justify-content: flex-end;
    }
    .action-buttons {
      min-width: 230px;
    }
    .action-buttons button {
      height: 28px;
      padding: 0 8px;
      font-size: 12px;
    }
    footer {
      color: var(--muted);
      font-size: 12px;
      margin-top: 8px;
    }
    @media (max-width: 1120px) {
      .metrics { grid-template-columns: repeat(3, minmax(150px, 1fr)); }
      .columns { grid-template-columns: 1fr; }
    }
    @media (max-width: 720px) {
      .topbar { grid-template-columns: 1fr; }
      .controls { justify-content: stretch; }
      .controls input, .controls button, label.inline { flex: 1 1 100%; }
      .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .metric .value { font-size: 20px; }
      main { padding: 14px 12px 28px; }
      .split { grid-template-columns: 1fr; }
      .form-fields { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <header>
    <div class="topbar">
      <div>
        <h1>mi admin dashboard</h1>
        <div class="subtitle">Nodes, health, usage, rewards, savings, reputation</div>
      </div>
      <div class="controls">
        <input id="token" type="password" autocomplete="current-password" placeholder="admin_token">
        <button id="saveToken" class="primary" type="button">Save</button>
        <button id="refresh" type="button">Refresh</button>
        <label class="inline">Cloud $/1M <input id="cloudPrice" type="number" min="0" step="0.01" value="0.20"></label>
        <label class="inline"><input id="autoRefresh" type="checkbox" checked> Auto</label>
      </div>
    </div>
  </header>

  <main>
    <div class="statusline">
      <div id="authStatus" class="pill warn">Admin token required</div>
      <div id="lastUpdated">Not loaded</div>
    </div>
    <div id="errors" class="errorbox"></div>
    <div id="secretBox" class="secretbox" role="status" aria-live="polite">
      <div class="secret-head">
        <div>
          <div class="strong" id="secretTitle">One-time secret</div>
          <div class="secret-note">Shown only once and must be stored now.</div>
        </div>
        <div class="secret-actions">
          <button id="copySecret" type="button">Copy</button>
          <button id="dismissSecret" type="button">Dismiss</button>
        </div>
      </div>
      <div id="secretValue" class="secret-value mono"></div>
    </div>

    <section class="grid metrics" aria-label="Key metrics">
      <div class="metric"><div class="label">Healthy nodes</div><div id="healthyNodes" class="value">-</div><div id="nodeSub" class="sub"></div></div>
      <div class="metric"><div class="label">Capacity</div><div id="capacity" class="value">-</div><div id="capacitySub" class="sub"></div></div>
      <div class="metric"><div class="label">Tokens</div><div id="totalTokens" class="value">-</div><div id="tokenSub" class="sub"></div></div>
      <div class="metric"><div class="label">Rewards</div><div id="rewards" class="value">-</div><div id="rewardSub" class="sub"></div></div>
      <div class="metric"><div class="label">Cloud equiv</div><div id="cloudCost" class="value">-</div><div id="cloudSub" class="sub"></div></div>
      <div class="metric"><div class="label">Est. savings</div><div id="savings" class="value">-</div><div id="savingsSub" class="sub"></div></div>
    </section>

    <section class="grid columns">
      <div>
        <section class="panel">
          <h2><span>Nodes</span><span id="nodesCount" class="pill">0</span></h2>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Node</th>
                  <th>Health</th>
                  <th>Provider</th>
                  <th>Load</th>
                  <th>Observed</th>
                  <th>Models</th>
                  <th>Hardware</th>
                  <th>Last seen</th>
                </tr>
              </thead>
              <tbody id="nodesBody"></tbody>
            </table>
          </div>
        </section>

        <section class="panel">
          <h2><span>Provider Reputation</span><span id="providersCount" class="pill">0</span></h2>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Provider</th>
                  <th>Score</th>
                  <th>Nodes</th>
                  <th>Events</th>
                  <th>Rewards</th>
                  <th>Challenges</th>
                  <th>Notes</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody id="providersBody"></tbody>
            </table>
          </div>
        </section>

        <section class="panel">
          <h2><span>Consumers</span><span id="consumersCount" class="pill">0</span></h2>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Consumer</th>
                  <th>Status</th>
                  <th>Requests</th>
                  <th>Tokens</th>
                  <th>Quota</th>
                  <th>Updated</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody id="consumersBody"></tbody>
            </table>
          </div>
        </section>
      </div>

      <aside>
        <section class="panel">
          <h2><span>Operator Write Actions</span><span class="pill warn">Admin</span></h2>
          <div class="panel-body">
            <div class="operator-forms">
              <form id="createConsumer" class="admin-form" autocomplete="off">
                <div class="form-title">Create consumer</div>
                <div class="form-fields">
                  <label>ID <input id="createConsumerID" name="id" required placeholder="consumer-id"></label>
                  <label>Display name <input id="createConsumerName" name="display_name" placeholder="Display name"></label>
                  <label>Total token limit <input id="createConsumerLimit" name="total_token_limit" type="number" min="0" step="1" placeholder="optional"></label>
                </div>
                <div class="form-actions">
                  <button id="createConsumerSubmit" class="primary" type="submit">Create consumer</button>
                </div>
              </form>

              <form id="createProvider" class="admin-form" autocomplete="off">
                <div class="form-title">Create provider</div>
                <div class="form-fields">
                  <label>ID <input id="createProviderID" name="id" required placeholder="provider-id"></label>
                  <label>Display name <input id="createProviderName" name="display_name" placeholder="Display name"></label>
                  <label>Privacy mode <select id="createProviderPrivacy" name="privacy_mode">
                    <option value="public">public</option>
                    <option value="community">community</option>
                    <option value="private">private</option>
                  </select></label>
                </div>
                <div class="form-actions">
                  <button id="createProviderSubmit" class="primary" type="submit">Create provider</button>
                </div>
              </form>
            </div>
          </div>
        </section>

        <section class="panel">
          <h2><span>Network</span><span id="networkBadge" class="pill">-</span></h2>
          <div id="networkBody" class="panel-body"></div>
        </section>

        <section class="panel">
          <h2><span>Settlement</span><span id="settlementBadge" class="pill">-</span></h2>
          <div id="settlementBody" class="panel-body"></div>
        </section>

        <section class="panel">
          <h2><span>Provider Rewards</span><span id="rewardCount" class="pill">0</span></h2>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Provider</th>
                  <th>Tokens</th>
                  <th>Reward</th>
                  <th>Latency</th>
                </tr>
              </thead>
              <tbody id="rewardsBody"></tbody>
            </table>
          </div>
        </section>

        <section class="panel">
          <h2><span>Integrity</span><span id="integrityBadge" class="pill">-</span></h2>
          <div id="integrityBody" class="panel-body"></div>
        </section>
      </aside>
    </section>

    <footer>Savings assumes settlement micros are USD micros. Adjust the cloud comparison price to match your alternative provider.</footer>
  </main>

  <script>
    const $ = (id) => document.getElementById(id);
    const fmt = new Intl.NumberFormat(undefined);
    const moneyFmt = new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 2 });
    const compactFmt = new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 });
    let timer = null;
    let secretText = '';

    const state = {
      status: null,
      nodes: [],
      city: null,
      settlement: null,
      reputation: null,
      integrity: null,
      challenges: null
    };

    function esc(value) {
      return String(value ?? '').replace(/[&<>"']/g, (ch) => ({
        '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
      }[ch]));
    }

    function token() {
      return $('token').value.trim();
    }

    function headers() {
      const h = { 'Accept': 'application/json' };
      if (token()) h.Authorization = 'Bearer ' + token();
      return h;
    }

    async function getJSON(path, admin = true) {
      const res = await fetch(path, { headers: admin ? headers() : { 'Accept': 'application/json' }, cache: 'no-store' });
      if (!res.ok) throw new Error(path + ' returned ' + res.status);
      return res.json();
    }

    async function postJSON(path, body) {
      const h = headers();
      h['Content-Type'] = 'application/json';
      const res = await fetch(path, {
        method: 'POST',
        headers: h,
        body: JSON.stringify(body || {})
      });
      return parseActionResponse(res, path);
    }

    async function sendAction(path, options) {
      options = options || {};
      if (!requireAdminToken()) {
        return null;
      }
      const button = options.button || null;
      if (button) button.disabled = true;
      try {
        let data;
        if ((options.method || 'POST') === 'POST') {
          data = await postJSON(path, options.body || {});
        } else {
          const res = await fetch(path, { method: options.method, headers: headers() });
          data = await parseActionResponse(res, path);
        }
        showReturnedSecret(data);
        await refresh();
        return data;
      } catch (err) {
        showError(err.message);
        return null;
      } finally {
        if (button) button.disabled = false;
      }
    }

    async function parseActionResponse(res, path) {
      const text = await res.text();
      let data = {};
      if (text) {
        try {
          data = JSON.parse(text);
        } catch (err) {
          data = { message: text.trim() };
        }
      }
      if (!res.ok) {
        const message = data && data.error && data.error.message ? data.error.message : data.message || (path + ' returned ' + res.status);
        throw new Error(message);
      }
      return data;
    }

    function requireAdminToken() {
      if (!token()) {
        showError('Enter the admin token before using admin actions.');
        return false;
      }
      return true;
    }

    function showError(message) {
      $('errors').style.display = 'block';
      $('errors').textContent = message || 'Request failed';
    }

    function showReturnedSecret(data) {
      if (data && data.api_key) {
        showSecret('Consumer API key', data.api_key);
      }
      if (data && data.provider_token) {
        showSecret('Provider token', data.provider_token);
      }
    }

    function showSecret(title, value) {
      secretText = String(value || '');
      $('secretTitle').textContent = title;
      $('secretValue').textContent = secretText;
      $('secretBox').style.display = 'block';
    }

    function hideSecret() {
      secretText = '';
      $('secretValue').textContent = '';
      $('secretBox').style.display = 'none';
    }

    function copyText(value, button) {
      if (!value) {
        showError('Nothing to copy.');
        return;
      }
      const done = () => {
        if (!button) return;
        const old = button.textContent;
        button.textContent = 'Copied';
        setTimeout(() => {
          button.textContent = old;
        }, 1200);
      };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(value).then(done).catch(() => fallbackCopy(value, done));
        return;
      }
      fallbackCopy(value, done);
    }

    function fallbackCopy(value, done) {
      const area = document.createElement('textarea');
      area.value = value;
      area.setAttribute('readonly', '');
      area.style.position = 'fixed';
      area.style.left = '-9999px';
      document.body.appendChild(area);
      area.select();
      try {
        document.execCommand('copy');
        done();
      } catch (err) {
        showError('Copy failed. Select and copy the value manually.');
      } finally {
        document.body.removeChild(area);
      }
    }

    function saveSettings() {
      localStorage.setItem('mi.adminToken', token());
      localStorage.setItem('mi.cloudPrice', $('cloudPrice').value);
      refresh();
    }

    function loadSettings() {
      const params = new URLSearchParams(window.location.search);
      const queryToken = params.get('token');
      if (queryToken) {
        localStorage.setItem('mi.adminToken', queryToken);
        history.replaceState(null, '', window.location.pathname);
      }
      $('token').value = localStorage.getItem('mi.adminToken') || '';
      $('cloudPrice').value = localStorage.getItem('mi.cloudPrice') || '0.20';
    }

    async function refresh() {
      $('refresh').disabled = true;
      $('errors').style.display = 'none';
      $('errors').textContent = '';
      const requests = [
        ['status', getJSON('/network/status', false)],
        ['nodes', getJSON('/admin/nodes')],
        ['city', getJSON('/admin/city')],
        ['settlement', getJSON('/admin/settlement?limit=8')],
        ['reputation', getJSON('/admin/reputation')],
        ['integrity', getJSON('/admin/integrity')],
        ['challenges', getJSON('/admin/challenges?limit=8')]
      ];
      const results = await Promise.allSettled(requests.map((item) => item[1]));
      const errors = [];
      results.forEach((result, index) => {
        const key = requests[index][0];
        if (result.status === 'fulfilled') {
          state[key] = result.value;
        } else {
          errors.push(result.reason.message);
        }
      });
      render(errors);
      $('refresh').disabled = false;
    }

    function render(errors) {
      const authed = !errors.some((err) => err.includes('/admin/') && (err.includes('401') || err.includes('403')));
      $('authStatus').className = 'pill ' + (authed ? 'good' : 'warn');
      $('authStatus').textContent = authed ? 'Admin data loaded' : 'Admin token required';
      $('lastUpdated').textContent = 'Updated ' + new Date().toLocaleTimeString();
      if (errors.length) {
        $('errors').style.display = 'block';
        $('errors').textContent = errors.join('\n');
      }
      renderMetrics();
      renderNetwork();
      renderNodes();
      renderProviders();
      renderConsumers();
      renderSettlement();
      renderRewards();
      renderIntegrity();
    }

    function renderMetrics() {
      const status = state.status || {};
      const city = state.city || {};
      const settlement = state.settlement || {};
      const consumerUsage = city.consumer_usage || [];
      const totalTokens = sum(consumerUsage, 'total_tokens');
      const totalRequests = sum(consumerUsage, 'requests');
      const providerBalances = settlement.provider_balances || [];
      const consumerBalances = settlement.consumer_balances || [];
      const rewards = sum(providerBalances, 'reward_micros');
      const debit = sum(consumerBalances, 'debit_micros');
      const cloudPrice = Number($('cloudPrice').value || 0);
      const cloudCost = totalTokens * cloudPrice / 1000000;
      const localCost = debit / 1000000;
      const savings = cloudCost - localCost;

      $('healthyNodes').textContent = fmt.format(status.healthy_nodes || 0) + '/' + fmt.format(status.nodes || 0);
      $('nodeSub').textContent = (status.cooldown_nodes || 0) + ' cooldown';
      $('capacity').textContent = fmt.format(status.available_slots || 0) + '/' + fmt.format(status.max_concurrent || 0);
      $('capacitySub').textContent = fmt.format(status.active_requests || 0) + ' active';
      $('totalTokens').textContent = compactFmt.format(totalTokens || 0);
      $('tokenSub').textContent = fmt.format(totalRequests || 0) + ' requests';
      $('rewards').textContent = formatMicros(rewards);
      $('rewardSub').textContent = fmt.format(providerBalances.length) + ' providers';
      $('cloudCost').textContent = moneyFmt.format(cloudCost);
      $('cloudSub').textContent = moneyFmt.format(cloudPrice) + ' / 1M tokens';
      $('savings').textContent = moneyFmt.format(savings);
      $('savingsSub').textContent = 'local debit ' + formatMicros(debit);
    }

    function renderNetwork() {
      const s = state.status || {};
      $('networkBadge').textContent = (s.healthy_nodes || 0) + ' healthy';
      $('networkBadge').className = 'pill ' + ((s.healthy_nodes || 0) > 0 ? 'good' : 'warn');
      $('networkBody').innerHTML = [
        kv('Models', chips(s.models)),
        kv('Backends', chips(s.backends)),
        kv('Devices', chips(s.device_kinds)),
        kv('Accelerators', chips(s.accelerators)),
        kv('Cities', chips(s.cities)),
        kv('Avg latency', s.average_latency_ms ? fmt.format(s.average_latency_ms) + ' ms' : '-'),
        kv('Avg TTFT', s.average_ttft_ms ? fmt.format(s.average_ttft_ms) + ' ms' : '-'),
        kv('Avg tok/s', s.average_tokens_per_second ? Number(s.average_tokens_per_second).toFixed(2) : '-'),
        kv('Free memory', s.total_memory_free_mb ? fmt.format(s.total_memory_free_mb) + ' MB' : '-')
      ].join('');
    }

    function renderNodes() {
      const nodes = state.nodes || [];
      $('nodesCount').textContent = nodes.length;
      if (!nodes.length) {
        $('nodesBody').innerHTML = '<tr><td colspan="8" class="empty">No nodes connected</td></tr>';
        return;
      }
      $('nodesBody').innerHTML = nodes.map((n) => {
        const health = n.healthy && !n.in_cooldown ? badge('Healthy', 'good') : n.in_cooldown ? badge('Cooldown', 'warn') : badge('Unhealthy', 'bad');
        const load = esc((n.active || 0) + '/' + (n.max_concurrent || 0)) + '<div class="muted">queue ' + esc(n.queue_depth || 0) + '</div>';
        const observed = [
          n.observed_latency_ms ? esc(n.observed_latency_ms) + ' ms latency' : '',
          n.observed_ttft_ms ? esc(n.observed_ttft_ms) + ' ms TTFT' : '',
          n.observed_tokens_per_second ? esc(Number(n.observed_tokens_per_second).toFixed(2)) + ' tok/s' : ''
        ].filter(Boolean).join('<br>') || '<span class="muted">no observations</span>';
        return '<tr>' +
          td(stack(esc(n.public_name || n.id), esc(n.id), 'mono muted')) +
          td(health + (n.last_error ? '<div class="muted">' + esc(n.last_error) + '</div>' : '')) +
          td(stack(esc(n.provider_id || '-'), 'score ' + esc(n.provider_score || 0), 'muted')) +
          td(load) +
          td(observed) +
          td(chips(n.models)) +
          td(stack(esc([n.backend, n.device_kind, n.soc].filter(Boolean).join(' / ') || '-'), esc((n.accelerators || []).join(', ')), 'muted')) +
          td(timeAgo(n.last_seen)) +
          '</tr>';
      }).join('');
    }

    function renderProviders() {
      const providers = (state.reputation && state.reputation.providers) || [];
      $('providersCount').textContent = providers.length;
      if (!providers.length) {
        $('providersBody').innerHTML = '<tr><td colspan="8" class="empty">No providers yet</td></tr>';
        return;
      }
      $('providersBody').innerHTML = providers.map((p) => {
        const cls = p.score >= 75 ? 'good' : p.score >= 50 ? 'warn' : 'bad';
        return '<tr>' +
          td(stack(esc(p.display_name || p.provider_id), esc(p.provider_id), 'mono muted')) +
          td('<div class="stack"><span class="strong">' + esc(p.score) + ' / 100 ' + esc(p.grade || '') + '</span><div class="progress"><span class="' + cls + '" style="width:' + clamp(p.score, 0, 100) + '%"></span></div></div>') +
          td(esc((p.healthy_nodes || 0) + '/' + (p.total_nodes || 0)) + '<div class="muted">' + esc(p.active_requests || 0) + ' active</div>') +
          td(esc(p.completed_events || 0) + '<div class="muted">' + esc(p.total_tokens || 0) + ' tokens</div>') +
          td(formatMicros(p.reward_micros || 0) + (p.penalty_micros ? '<div class="muted">penalty ' + formatMicros(p.penalty_micros) + '</div>' : '')) +
          td(formatBPS(p.challenge_pass_rate_bps) + '<div class="muted">' + esc(p.challenges || 0) + ' checks</div>') +
          td(chips(p.notes || [])) +
          td(providerActions(p)) +
          '</tr>';
      }).join('');
    }

    function renderConsumers() {
      const city = state.city || {};
      const consumers = city.consumers || [];
      const usage = indexBy(city.consumer_usage || [], 'account_id');
      $('consumersCount').textContent = consumers.length;
      if (!consumers.length) {
        $('consumersBody').innerHTML = '<tr><td colspan="7" class="empty">No consumers configured</td></tr>';
        return;
      }
      $('consumersBody').innerHTML = consumers.map((c) => {
        const u = usage[c.id] || {};
        const limit = c.total_token_limit || 0;
        const used = u.total_tokens || 0;
        const pct = limit ? Math.min(100, Math.round(used * 100 / limit)) : 0;
        const cls = c.disabled ? 'bad' : limit && pct > 90 ? 'warn' : 'good';
        const quota = limit ? '<div class="stack"><span>' + fmt.format(used) + ' / ' + fmt.format(limit) + '</span><div class="progress"><span class="' + cls + '" style="width:' + pct + '%"></span></div></div>' : '<span class="muted">unlimited</span>';
        return '<tr>' +
          td(stack(esc(c.display_name || c.id), esc(c.id), 'mono muted')) +
          td(c.disabled ? badge('Disabled', 'bad') : badge('Active', 'good')) +
          td(fmt.format(u.requests || 0)) +
          td(fmt.format(used)) +
          td(quota) +
          td(timeAgo(u.updated_at)) +
          td(consumerActions(c)) +
          '</tr>';
      }).join('');
    }

    function renderSettlement() {
      const s = state.settlement || {};
      $('settlementBadge').textContent = s.enabled ? 'Enabled' : 'Off';
      $('settlementBadge').className = 'pill ' + (s.enabled ? 'good' : 'warn');
      $('settlementBody').innerHTML = [
        kv('Events', fmt.format(s.events || 0)),
        kv('Last hash', s.last_hash ? '<span class="mono">' + esc(shortHash(s.last_hash)) + '</span>' : '-'),
        kv('Price', s.price_per_thousand_tokens_micros ? formatMicros(s.price_per_thousand_tokens_micros) + ' / 1K tokens' : '-'),
        kv('Provider share', s.provider_reward_share_bps ? formatBPS(s.provider_reward_share_bps) : '-'),
        kv('SLA target', s.target_latency_ms ? fmt.format(s.target_latency_ms) + ' ms' : '-'),
        kv('Store', esc(s.sqlite_path || s.chain_path || '-'))
      ].join('');
    }

    function renderRewards() {
      const balances = (state.settlement && state.settlement.provider_balances) || [];
      $('rewardCount').textContent = balances.length;
      if (!balances.length) {
        $('rewardsBody').innerHTML = '<tr><td colspan="4" class="empty">No provider rewards yet</td></tr>';
        return;
      }
      $('rewardsBody').innerHTML = balances.map((b) => '<tr>' +
        td(esc(b.account_id)) +
        td(fmt.format(b.total_tokens || 0)) +
        td(formatMicros(b.reward_micros || 0) + (b.penalty_micros ? '<div class="muted">penalty ' + formatMicros(b.penalty_micros) + '</div>' : '')) +
        td(b.average_latency_ms ? fmt.format(b.average_latency_ms) + ' ms' : '-') +
        '</tr>').join('');
    }

    function renderIntegrity() {
      const i = state.integrity || {};
      $('integrityBadge').textContent = i.valid ? 'Valid' : 'Check';
      $('integrityBadge').className = 'pill ' + (i.valid ? 'good' : 'warn');
      const anchor = i.anchor || {};
      $('integrityBody').innerHTML = [
        kv('Settlement', statusText(i.settlement)),
        kv('Challenges', statusText(i.challenges)),
        kv('Anchor hash', anchor.anchor_hash ? '<span class="mono">' + esc(shortHash(anchor.anchor_hash)) + '</span> <button id="copyAnchorHash" type="button">Copy anchor hash</button>' : '-'),
        kv('Generated', i.generated_at ? new Date(i.generated_at).toLocaleString() : '-')
      ].join('');
    }

    function statusText(value) {
      if (!value) return '-';
      const cls = value.valid ? 'good' : 'bad';
      return badge(value.valid ? 'Valid' : 'Invalid', cls) + ' <span class="muted">' + esc(value.events || 0) + ' events</span>';
    }

    function sum(items, field) {
      return (items || []).reduce((total, item) => total + Number(item[field] || 0), 0);
    }

    function indexBy(items, field) {
      return (items || []).reduce((out, item) => {
        out[item[field]] = item;
        return out;
      }, {});
    }

    function td(html) {
      return '<td>' + html + '</td>';
    }

    function kv(key, value) {
      return '<div class="kv"><div class="k">' + esc(key) + '</div><div>' + (value || '-') + '</div></div>';
    }

    function stack(primary, secondary, secondaryClass) {
      return '<div class="stack"><span class="strong">' + primary + '</span><span class="' + (secondaryClass || 'muted') + '">' + secondary + '</span></div>';
    }

    function badge(text, cls) {
      return '<span class="pill ' + cls + '">' + esc(text) + '</span>';
    }

    function chips(values) {
      values = (values || []).filter(Boolean);
      if (!values.length) return '<span class="muted">-</span>';
      return '<div class="chips">' + values.slice(0, 8).map((v) => '<span class="chip">' + esc(v) + '</span>').join('') + (values.length > 8 ? '<span class="chip">+' + (values.length - 8) + '</span>' : '') + '</div>';
    }

    function consumerActions(c) {
      return '<div class="action-buttons">' +
        actionButton('rotate-consumer', 'Rotate key', 'data-consumer-id', c.id, c.disabled) +
        actionButton('disable-consumer', 'Disable', 'data-consumer-id', c.id, c.disabled, 'danger') +
        '</div>';
    }

    function providerActions(p) {
      return '<div class="action-buttons">' +
        actionButton('rotate-provider', 'Rotate token', 'data-provider-id', p.provider_id, p.disabled) +
        actionButton('disable-provider', 'Disable', 'data-provider-id', p.provider_id, p.disabled, 'danger') +
        actionButton('run-challenge', 'Run challenge', 'data-provider-id', p.provider_id, p.disabled) +
        '</div>';
    }

    function actionButton(action, label, attr, id, disabled, cls) {
      return '<button type="button" class="' + esc(cls || '') + '" data-action="' + esc(action) + '" ' + attr + '="' + esc(id) + '"' + (disabled ? ' disabled' : '') + '>' + esc(label) + '</button>';
    }

    function formatBPS(value) {
      if (value === undefined || value === null || value === '') return '-';
      return (Number(value) / 100).toFixed(1).replace(/\.0$/, '') + '%';
    }

    function formatMicros(value) {
      return moneyFmt.format(Number(value || 0) / 1000000);
    }

    function timeAgo(value) {
      if (!value || value === '0001-01-01T00:00:00Z') return '-';
      const then = new Date(value).getTime();
      if (!then) return '-';
      const seconds = Math.max(0, Math.round((Date.now() - then) / 1000));
      if (seconds < 60) return seconds + 's ago';
      const minutes = Math.round(seconds / 60);
      if (minutes < 60) return minutes + 'm ago';
      const hours = Math.round(minutes / 60);
      if (hours < 48) return hours + 'h ago';
      return new Date(value).toLocaleDateString();
    }

    function shortHash(value) {
      value = String(value || '');
      if (value.length <= 18) return value;
      return value.slice(0, 10) + '...' + value.slice(-6);
    }

    function clamp(value, min, max) {
      return Math.max(min, Math.min(max, Number(value || 0)));
    }

    $('createConsumer').addEventListener('submit', async (event) => {
      event.preventDefault();
      const body = {
        id: $('createConsumerID').value.trim(),
        display_name: $('createConsumerName').value.trim()
      };
      const limit = $('createConsumerLimit').value.trim();
      if (limit !== '') {
        body.total_token_limit = Number(limit);
      }
      const result = await sendAction('/admin/consumers', { button: $('createConsumerSubmit'), body: body });
      if (result) event.currentTarget.reset();
    });

    $('createProvider').addEventListener('submit', async (event) => {
      event.preventDefault();
      const body = {
        id: $('createProviderID').value.trim(),
        display_name: $('createProviderName').value.trim(),
        privacy_mode: $('createProviderPrivacy').value
      };
      const result = await sendAction('/admin/providers', { button: $('createProviderSubmit'), body: body });
      if (result) event.currentTarget.reset();
    });

    document.addEventListener('click', async (event) => {
      const button = event.target.closest('button');
      if (!button) return;
      if (button.id === 'copySecret') {
        copyText(secretText, button);
        return;
      }
      if (button.id === 'dismissSecret') {
        hideSecret();
        return;
      }
      if (button.id === 'copyAnchorHash') {
        const anchor = (state.integrity && state.integrity.anchor) || {};
        copyText(anchor.anchor_hash || '', button);
        return;
      }
      const action = button.getAttribute('data-action');
      if (!action) return;
      if (!requireAdminToken()) return;
      const consumerID = button.getAttribute('data-consumer-id') || '';
      const providerID = button.getAttribute('data-provider-id') || '';
      if (action === 'rotate-consumer') {
        await sendAction('/admin/consumers/' + encodeURIComponent(consumerID) + '/rotate-key', { button: button, body: {} });
      }
      if (action === 'disable-consumer' && window.confirm('Disable consumer ' + consumerID + '?')) {
        await sendAction('/admin/consumers/' + encodeURIComponent(consumerID), { button: button, method: 'DELETE' });
      }
      if (action === 'rotate-provider') {
        await sendAction('/admin/providers/' + encodeURIComponent(providerID) + '/rotate-token', { button: button, body: {} });
      }
      if (action === 'disable-provider' && window.confirm('Disable provider ' + providerID + '?')) {
        await sendAction('/admin/providers/' + encodeURIComponent(providerID), { button: button, method: 'DELETE' });
      }
      if (action === 'run-challenge') {
        await sendAction('/admin/challenges/run?provider_id=' + encodeURIComponent(providerID), { button: button, body: {} });
      }
    });

    $('saveToken').addEventListener('click', saveSettings);
    $('refresh').addEventListener('click', refresh);
    $('cloudPrice').addEventListener('change', () => {
      localStorage.setItem('mi.cloudPrice', $('cloudPrice').value);
      renderMetrics();
    });
    $('autoRefresh').addEventListener('change', () => {
      if (timer) clearInterval(timer);
      timer = $('autoRefresh').checked ? setInterval(refresh, 10000) : null;
    });
    $('token').addEventListener('keydown', (event) => {
      if (event.key === 'Enter') saveSettings();
    });

    loadSettings();
    refresh();
    timer = setInterval(refresh, 10000);
  </script>
</body>
</html>`
