package main

const adminHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Rooter</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f7f5ef;
      --panel: #ffffff;
      --ink: #1f2623;
      --muted: #65706a;
      --line: #d9ded8;
      --accent: #176c5d;
      --accent-ink: #ffffff;
      --danger: #b42318;
      --warn: #9a6700;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--ink);
      font: 14px/1.45 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 20px;
      padding: 18px 28px;
      border-bottom: 1px solid var(--line);
      background: #fbfaf6;
      position: sticky;
      top: 0;
      z-index: 2;
    }
    h1 { margin: 0; font-size: 20px; letter-spacing: 0; }
    h2 { margin: 0 0 14px; font-size: 15px; }
    main {
      display: grid;
      grid-template-columns: minmax(320px, 420px) minmax(0, 1fr);
      gap: 18px;
      padding: 20px 28px 32px;
      max-width: 1400px;
      margin: 0 auto;
      width: 100%;
    }
    main > *, section, .stack {
      min-width: 0;
    }
    section {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 16px;
    }
    label { display: block; color: var(--muted); font-size: 12px; margin: 10px 0 5px; }
    input, select, textarea {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #fff;
      color: var(--ink);
      padding: 8px 9px;
      font: inherit;
    }
    textarea { min-height: 92px; resize: vertical; overflow-wrap: anywhere; }
    table { width: 100%; border-collapse: collapse; }
    .models-table { min-width: 760px; table-layout: fixed; }
    .chains-table { min-width: 520px; table-layout: fixed; }
    th, td { border-bottom: 1px solid var(--line); padding: 8px; text-align: left; vertical-align: middle; }
    th { color: var(--muted); font-weight: 600; font-size: 12px; }
    td input, td select { min-width: 0; }
    .row { display: flex; gap: 8px; align-items: center; }
    .row > * { min-width: 0; }
    .filter-row input { flex: 1; }
    .field-row { display: flex; gap: 6px; align-items: stretch; }
    .field-row input { flex: 1; min-width: 0; }
    .field-row button { flex: 0 0 auto; min-width: 44px; }
    .actions { display: flex; flex-wrap: wrap; gap: 8px; justify-content: flex-end; }
    button {
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #fff;
      color: var(--ink);
      padding: 8px 11px;
      font: inherit;
      cursor: pointer;
      min-height: 36px;
    }
    button.primary { background: var(--accent); border-color: var(--accent); color: var(--accent-ink); }
    button.danger { color: var(--danger); }
    button.icon { width: 36px; padding: 0; font-weight: 700; }
    button:disabled { opacity: .45; cursor: not-allowed; }
    .stack { display: grid; gap: 16px; }
    .muted { color: var(--muted); }
    .muted, input, textarea, .pill {
      overflow-wrap: anywhere;
    }
    .status { min-height: 20px; color: var(--muted); }
    .status.error { color: var(--danger); }
    .status.ok { color: var(--accent); }
    .pill {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 4px 8px;
      color: var(--muted);
      background: #fbfaf6;
      white-space: normal;
      max-width: 100%;
      line-height: 1.25;
    }
    .hidden { display: none !important; }
    .provider {
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 12px;
      margin-bottom: 10px;
      background: #fff;
      overflow: hidden;
    }
    .provider-summary {
      display: grid;
      gap: 4px;
      min-width: 0;
    }
    .provider-summary strong {
      overflow-wrap: anywhere;
    }
    .provider-actions {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-top: 10px;
    }
    .provider-actions button {
      flex: 1 1 96px;
    }
    .provider-key {
      margin-top: 10px;
    }
    .grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
    .wide { grid-column: 1 / -1; }
    .checkbox { width: auto; margin-right: 7px; }
    .toolbar { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 10px; }
    .scroll { overflow-x: auto; }
    @media (max-width: 900px) {
      header { align-items: flex-start; flex-direction: column; padding: 16px; }
      main { grid-template-columns: 1fr; padding: 16px; }
    }
  </style>
</head>
<body>
  <header>
    <div>
      <h1>Rooter</h1>
      <div class="muted">One OpenAI-compatible surface over selected upstream models.</div>
    </div>
    <div class="actions">
      <div id="authPrompt" class="row" style="display:none; min-width: 320px;">
        <div class="field-row">
          <input id="headerAdminToken" type="password" autocomplete="off" placeholder="Admin UI token">
          <button type="button" data-toggle-password="headerAdminToken" title="Show or hide token">Show</button>
        </div>
        <button id="headerAdminTokenBtn">Use token</button>
        <span id="authStatus" class="status"></span>
      </div>
      <div id="mainActions" class="actions">
        <span id="saveState" class="status"></span>
        <button id="reloadBtn">Reload</button>
        <button id="saveBtn" class="primary">Save settings</button>
      </div>
    </div>
  </header>
  <main>
    <div class="stack">
      <section>
        <h2>Access</h2>
        <label>Admin token</label>
        <div class="field-row">
          <input id="adminToken" type="password" autocomplete="off" placeholder="Optional admin UI token">
          <button type="button" data-toggle-password="adminToken" title="Show or hide token">Show</button>
        </div>
        <label>Public API keys</label>
        <textarea id="publicKeys" spellcheck="false" placeholder="One key per line"></textarea>
        <div class="actions">
          <button id="generatePublicKeyBtn">Generate API key</button>
        </div>
      </section>
      <section>
        <div class="toolbar">
          <h2>Providers</h2>
          <button id="addProviderBtn">Add provider</button>
        </div>
        <div id="providers"></div>
      </section>
      <section>
        <h2>Activate models</h2>
        <label>Provider</label>
        <select id="activateProvider"></select>
        <label>Model names</label>
        <textarea id="activateModels" spellcheck="false" placeholder="One upstream model name per line"></textarea>
        <label><input id="activateEnabled" class="checkbox" type="checkbox" checked>Enable in public model list</label>
        <label><input id="activatePull" class="checkbox" type="checkbox">Call Ollama /api/pull first</label>
        <div class="actions">
          <button id="activateBtn">Activate</button>
        </div>
      </section>
      <section>
        <div class="toolbar">
          <h2>Model chains</h2>
          <button id="addChainBtn">Add chain</button>
        </div>
        <div class="muted">Steps use configured provider/model pairs, including models that are not shown publicly.</div>
        <div class="scroll">
          <table class="chains-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Steps</th>
                <th style="width:54px;"></th>
              </tr>
            </thead>
            <tbody id="chains"></tbody>
          </table>
        </div>
      </section>
    </div>
    <section>
      <div class="toolbar">
        <div>
          <h2>Visible models</h2>
          <div class="muted">Only enabled rows are returned by <code>/v1/models</code>. Public names are what clients send.</div>
        </div>
        <div class="actions">
          <button id="addModelBtn">Add model</button>
        </div>
      </div>
      <div class="row filter-row" style="margin-bottom: 10px;">
        <input id="modelFilter" placeholder="Filter rows">
      </div>
      <div class="scroll">
        <table class="models-table">
          <thead>
            <tr>
              <th style="width:64px;">Show</th>
              <th>Public name</th>
              <th>Provider</th>
              <th>Upstream model</th>
              <th>Model chain</th>
              <th style="width:140px;">Order</th>
              <th style="width:54px;"></th>
            </tr>
          </thead>
          <tbody id="models"></tbody>
        </table>
      </div>
    </section>
  </main>
  <script>
    const state = { config: null, editingProviderID: '' };
    const els = {
      saveState: document.getElementById('saveState'),
      authPrompt: document.getElementById('authPrompt'),
      headerAdminToken: document.getElementById('headerAdminToken'),
      authStatus: document.getElementById('authStatus'),
      mainActions: document.getElementById('mainActions'),
      adminToken: document.getElementById('adminToken'),
      publicKeys: document.getElementById('publicKeys'),
      providers: document.getElementById('providers'),
      chains: document.getElementById('chains'),
      models: document.getElementById('models'),
      modelFilter: document.getElementById('modelFilter'),
      activateProvider: document.getElementById('activateProvider'),
      activateModels: document.getElementById('activateModels'),
      activateEnabled: document.getElementById('activateEnabled'),
      activatePull: document.getElementById('activatePull'),
    };

    function token() {
      return localStorage.getItem('rooter.adminToken') || '';
    }

    function headers() {
      const h = { 'Content-Type': 'application/json' };
      if (token()) h.Authorization = 'Bearer ' + token();
      return h;
    }

    function setStatus(message, kind = '') {
      els.saveState.textContent = message;
      els.saveState.className = 'status ' + kind;
    }

    function setAuthStatus(message, kind = '') {
      els.authStatus.textContent = message;
      els.authStatus.className = 'status ' + kind;
    }

    async function api(path, options = {}) {
      const res = await fetch(path, { ...options, headers: { ...headers(), ...(options.headers || {}) } });
      if (!res.ok) {
        let detail = await res.text();
        try { detail = JSON.parse(detail).error.message; } catch (_) {}
        if (res.status === 401) showAuthPrompt(detail);
        throw new Error(detail || res.statusText);
      }
      return await res.json();
    }

    function randomKey(prefix) {
      const bytes = new Uint8Array(24);
      crypto.getRandomValues(bytes);
      return prefix + '_' + Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
    }

    function slug(value) {
      return String(value || '').trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
    }

    function providerOptions(selected) {
      return state.config.providers.map(p => '<option value="' + escapeHTML(p.id) + '"' + (p.id === selected ? ' selected' : '') + '>' + escapeHTML(p.name || p.id) + '</option>').join('');
    }

    function providerName(id) {
      const provider = state.config.providers.find(p => p.id === id);
      return provider ? (provider.name || provider.id) : id;
    }

    function chainName(id) {
      const chain = (state.config.chains || []).find(c => c.id === id);
      return chain ? (chain.name || chain.id) : id;
    }

    function chainOptions(selected) {
      return '<option value="">Direct model</option>' + (state.config.chains || []).map(c =>
        '<option value="' + escapeHTML(c.id) + '"' + (c.id === selected ? ' selected' : '') + '>' + escapeHTML(c.name || c.id) + '</option>'
      ).join('');
    }

    function modelValue(providerID, upstreamName) {
      return providerID + '|' + encodeURIComponent(upstreamName || '');
    }

    function parseModelValue(value) {
      const split = String(value || '').indexOf('|');
      if (split < 0) return { provider_id: '', upstream_name: '' };
      return {
        provider_id: slug(value.slice(0, split)),
        upstream_name: decodeURIComponent(value.slice(split + 1))
      };
    }

    function configuredModelOptions(selectedStep) {
      const hasSelected = !!(selectedStep?.provider_id && selectedStep?.upstream_name);
      const selected = hasSelected ? modelValue(selectedStep.provider_id, selectedStep.upstream_name) : '';
      const seen = new Set();
      const options = [];
      state.config.models.forEach(m => {
        const providerID = slug(m.provider_id);
        const upstreamName = String(m.upstream_name || '').trim();
        if (!providerID || !upstreamName) return;
        const value = modelValue(providerID, upstreamName);
        if (seen.has(value)) return;
        seen.add(value);
        const visibility = m.enabled ? '' : ' (not shown)';
        options.push('<option value="' + escapeHTML(value) + '"' + (value === selected ? ' selected' : '') + '>' + escapeHTML(providerName(providerID) + ' / ' + upstreamName + visibility) + '</option>');
      });
      if (hasSelected && !seen.has(selected)) {
        options.unshift('<option value="' + escapeHTML(selected) + '" selected>' + escapeHTML(providerName(selectedStep.provider_id) + ' / ' + selectedStep.upstream_name) + '</option>');
      }
      return '<option value="">Select model</option>' + options.join('');
    }

    function firstChainStep(chainID) {
      const chain = (state.config.chains || []).find(c => c.id === chainID);
      return chain?.steps?.[0] || null;
    }

    function keyFingerprint(value) {
      value = String(value || '').trim();
      if (!value) return 'no key';
      const suffix = value.slice(-6);
      return 'key ...' + suffix;
    }

    function hasProviderModel(providerID, upstreamName) {
      return state.config.models.some(m => m.provider_id === providerID && m.upstream_name === upstreamName);
    }

    function uniquePublicName(base, providerID) {
      const existing = new Set(state.config.models.map(m => m.public_name));
      if (!existing.has(base)) return base;
      const providerSuffix = providerID ? '-' + providerID : '';
      let candidate = base + providerSuffix;
      let n = 2;
      while (existing.has(candidate)) {
        candidate = base + providerSuffix + '-' + n;
        n++;
      }
      return candidate;
    }

    function uniqueChainID(base) {
      const existing = new Set((state.config.chains || []).map(c => c.id));
      let candidate = slug(base) || 'chain';
      let n = 2;
      while (existing.has(candidate)) {
        candidate = (slug(base) || 'chain') + '-' + n;
        n++;
      }
      return candidate;
    }

    function defaultBaseURL(type) {
      if (type === 'ollama_cloud') return 'https://ollama.com/api';
      if (type === 'ollama') return 'http://localhost:11434/api';
      return '';
    }

    function escapeHTML(value) {
      return String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
    }

    function hydrateForm() {
      state.config.chains = state.config.chains || [];
      els.authPrompt.style.display = 'none';
      els.mainActions.style.display = 'flex';
      setAuthStatus('');
      els.adminToken.value = state.config.admin_token || '';
      els.publicKeys.value = (state.config.public_api_keys || []).join('\n');
      els.activateProvider.innerHTML = providerOptions(els.activateProvider.value);
      renderProviders();
      renderChains();
      renderModels();
    }

    function showAuthPrompt(detail = '') {
      els.authPrompt.style.display = 'flex';
      els.mainActions.style.display = 'none';
      els.headerAdminToken.value = token();
      const message = String(detail || '').toLowerCase().includes('wrong')
        ? 'Wrong token'
        : (token() ? 'Wrong token' : 'Token required');
      setAuthStatus(message, 'error');
    }

    function rememberAdminToken(value) {
      value = String(value || '').trim();
      if (!value) {
        setAuthStatus('Enter the admin token', 'error');
        return false;
      }
      localStorage.setItem('rooter.adminToken', value);
      els.headerAdminToken.value = value;
      setAuthStatus('Checking...');
      return true;
    }

    function renderProviders() {
      els.providers.innerHTML = '';
      state.config.providers.forEach((p, index) => {
        const item = document.createElement('div');
        item.className = 'provider';
        const editing = state.editingProviderID === p.id;
        if (editing) {
          item.innerHTML =
            '<div class="provider-actions" style="margin-top:0;">' +
            '<label><input class="checkbox" data-field="enabled" type="checkbox" ' + (p.enabled ? 'checked' : '') + '>Enabled</label>' +
            '<button data-action="discover">Discover</button>' +
            '<button data-action="done">Done</button>' +
            '<button class="danger" data-action="delete">Delete</button>' +
            '</div>' +
            '<div class="grid-2">' +
            '<div><label>Name</label><input data-field="name" value="' + escapeHTML(p.name) + '"></div>' +
            '<div><label>ID</label><input data-field="id" value="' + escapeHTML(p.id) + '"></div>' +
            '<div><label>Type</label><select data-field="type">' +
            '<option value="openai"' + (p.type === 'openai' ? ' selected' : '') + '>OpenAI-compatible</option>' +
            '<option value="ollama"' + (p.type === 'ollama' ? ' selected' : '') + '>Ollama</option>' +
            '<option value="ollama_cloud"' + (p.type === 'ollama_cloud' ? ' selected' : '') + '>Ollama Cloud</option>' +
            '</select></div>' +
            '<div><label>API key</label><div class="field-row"><input id="providerKey' + index + '" data-field="api_key" type="password" value="' + escapeHTML(p.api_key || '') + '"><button type="button" data-toggle-password="providerKey' + index + '" title="Show or hide API key">Show</button></div></div>' +
            '<div class="wide"><label>Base URL</label><input data-field="base_url" value="' + escapeHTML(p.base_url) + '" placeholder="https://api.example.com/v1 or https://ollama.com/api"></div>' +
            '</div>';
          item.addEventListener('input', e => updateProvider(index, e.target));
          item.addEventListener('change', e => updateProvider(index, e.target));
          item.querySelector('[data-action="done"]').addEventListener('click', async () => {
            state.editingProviderID = '';
            await save();
          });
        } else {
          item.innerHTML =
            '<div class="provider-summary">' +
            '<strong>' + escapeHTML(p.name || p.id) + '</strong>' +
            '<span class="muted">ID ' + escapeHTML(p.id) + (p.api_key ? ' · ' + escapeHTML(keyFingerprint(p.api_key)) : ' · no key') + '</span>' +
            '<span class="muted">' + escapeHTML(p.type || 'openai') + ' · ' + escapeHTML(p.base_url || 'No base URL set') + '</span>' +
            '<span class="muted">' + (p.enabled ? 'Enabled' : 'Disabled') + '</span>' +
            '</div>' +
            (p.api_key ? '<div class="field-row provider-key"><input id="providerSummaryKey' + index + '" type="password" readonly value="' + escapeHTML(p.api_key) + '"><button type="button" data-toggle-password="providerSummaryKey' + index + '" title="Show or hide API key">Show</button></div>' : '') +
            '<div class="provider-actions">' +
            '<button data-action="discover">Discover</button>' +
            '<button data-action="edit">Edit</button>' +
            '<button class="danger" data-action="delete">Delete</button>' +
            '</div>';
          item.querySelector('[data-action="edit"]').addEventListener('click', () => {
            state.editingProviderID = p.id;
            renderProviders();
          });
        }
        item.querySelector('[data-action="discover"]').addEventListener('click', () => discoverProvider(p.id));
        item.querySelector('[data-action="delete"]').addEventListener('click', () => {
          const id = state.config.providers[index].id;
          state.config.providers.splice(index, 1);
          state.config.models = state.config.models.filter(m => m.provider_id !== id);
          state.config.models.forEach(m => { m.chain = (m.chain || []).filter(step => step.provider_id !== id); });
          state.config.chains.forEach(c => { c.steps = (c.steps || []).filter(step => step.provider_id !== id); });
          if (state.editingProviderID === id) state.editingProviderID = '';
          hydrateForm();
        });
        els.providers.appendChild(item);
      });
    }

    function updateProvider(index, target) {
      const field = target.dataset.field;
      if (!field) return;
      const oldID = state.config.providers[index].id;
      const value = target.type === 'checkbox' ? target.checked : target.value;
      state.config.providers[index][field] = value;
      if (field === 'type') {
        const defaultURL = defaultBaseURL(value);
        if (defaultURL) state.config.providers[index].base_url = defaultURL;
        renderProviders();
      }
      if (field === 'name' && !state.config.providers[index].id) state.config.providers[index].id = slug(value);
      if (field === 'id') {
        state.config.models.forEach(m => { if (m.provider_id === oldID) m.provider_id = slug(value); });
        state.config.models.forEach(m => (m.chain || []).forEach(step => { if (step.provider_id === oldID) step.provider_id = slug(value); }));
        state.config.chains.forEach(c => (c.steps || []).forEach(step => { if (step.provider_id === oldID) step.provider_id = slug(value); }));
        state.config.providers[index].id = slug(value);
        state.editingProviderID = state.config.providers[index].id;
        els.activateProvider.innerHTML = providerOptions(els.activateProvider.value);
        renderChains();
        renderModels();
      }
    }

    function renderChains() {
      els.chains.innerHTML = '';
      state.config.chains.sort((a, b) => (a.order || 0) - (b.order || 0)).forEach((chain, index) => {
        chain.order = index + 1;
        chain.steps = chain.steps || [];
        const tr = document.createElement('tr');
        const stepRows = chain.steps.map((step, stepIndex) =>
          '<div class="field-row" style="margin-bottom:6px;">' +
          '<select data-field="chain_step" data-step="' + stepIndex + '">' + configuredModelOptions(step) + '</select>' +
          '<button class="icon" data-action="remove-step" data-step="' + stepIndex + '" title="Remove step">×</button>' +
          '</div>'
        ).join('');
        tr.innerHTML =
          '<td><input data-field="name" value="' + escapeHTML(chain.name || '') + '" placeholder="Coding"></td>' +
          '<td>' + stepRows + '<button data-action="add-step">Add step</button></td>' +
          '<td><button class="danger icon" data-action="delete" title="Delete">×</button></td>';
        tr.addEventListener('input', e => updateChain(index, e.target));
        tr.addEventListener('change', e => updateChain(index, e.target));
        tr.querySelector('[data-action="add-step"]').addEventListener('click', () => {
          const first = state.config.models.find(m => m.provider_id && m.upstream_name);
          chain.steps.push(first ? { provider_id: first.provider_id, upstream_name: first.upstream_name } : { provider_id: '', upstream_name: '' });
          renderChains();
        });
        tr.querySelectorAll('[data-action="remove-step"]').forEach(button => {
          button.addEventListener('click', () => {
            chain.steps.splice(Number(button.dataset.step), 1);
            renderChains();
          });
        });
        tr.querySelector('[data-action="delete"]').addEventListener('click', () => {
          const id = state.config.chains[index].id;
          state.config.chains.splice(index, 1);
          state.config.models.forEach(m => { if (m.chain_id === id) m.chain_id = ''; });
          renderChains();
          renderModels();
        });
        els.chains.appendChild(tr);
      });
    }

    function updateChain(index, target) {
      const field = target.dataset.field;
      if (!field) return;
      const chain = state.config.chains[index];
      if (field === 'chain_step') {
        chain.steps[Number(target.dataset.step)] = parseModelValue(target.value);
      } else {
        chain[field] = target.value;
        if (field === 'name') {
          const oldID = chain.id;
          chain.id = slug(chain.id || target.value);
          if (oldID && oldID !== chain.id) {
            state.config.models.forEach(m => { if (m.chain_id === oldID) m.chain_id = chain.id; });
          }
        }
      }
      renderModels();
    }

    function renderModels() {
      const filter = els.modelFilter.value.trim().toLowerCase();
      els.models.innerHTML = '';
      state.config.models.sort((a, b) => (a.order || 0) - (b.order || 0)).forEach((m, index) => {
        m.order = index + 1;
        const selectedChain = m.chain_id || '';
        const firstStep = selectedChain ? firstChainStep(selectedChain) : null;
        const providerLabel = firstStep ? providerName(firstStep.provider_id) : providerName(m.provider_id);
        const upstreamLabel = firstStep ? firstStep.upstream_name : m.upstream_name;
        const text = [m.public_name, upstreamLabel, providerLabel, selectedChain, chainName(selectedChain)].join(' ').toLowerCase();
        if (filter && !text.includes(filter)) return;
        const tr = document.createElement('tr');
        tr.innerHTML =
          '<td><input class="checkbox" data-field="enabled" type="checkbox" ' + (m.enabled ? 'checked' : '') + '></td>' +
          '<td><input data-field="public_name" value="' + escapeHTML(m.public_name) + '"></td>' +
          '<td><span class="pill">' + escapeHTML(providerLabel) + '</span></td>' +
          '<td><input data-field="upstream_name" value="' + escapeHTML(upstreamLabel || '') + '"' + (selectedChain ? ' disabled' : '') + '></td>' +
          '<td><select data-field="chain_id">' + chainOptions(selectedChain) + '</select></td>' +
          '<td><button class="icon" data-action="up" title="Move up">↑</button> <button class="icon" data-action="down" title="Move down">↓</button></td>' +
          '<td><button class="danger icon" data-action="delete" title="Delete">×</button></td>';
        tr.addEventListener('input', e => updateModel(index, e.target));
        tr.addEventListener('change', e => updateModel(index, e.target));
        tr.querySelector('[data-action="up"]').addEventListener('click', () => moveModel(index, -1));
        tr.querySelector('[data-action="down"]').addEventListener('click', () => moveModel(index, 1));
        tr.querySelector('[data-action="delete"]').addEventListener('click', () => {
          state.config.models.splice(index, 1);
          renderModels();
        });
        els.models.appendChild(tr);
      });
    }

    function updateModel(index, target) {
      const field = target.dataset.field;
      if (!field) return;
      if (field === 'chain_id') {
        const model = state.config.models[index];
        model.chain_id = target.value;
        if (model.chain_id) {
          const step = firstChainStep(model.chain_id);
          if (step) {
            model.provider_id = step.provider_id;
            model.upstream_name = step.upstream_name;
          }
          model.chain = [];
        }
        renderModels();
      } else {
        state.config.models[index][field] = target.type === 'checkbox' ? target.checked : target.value;
      }
    }

    function moveModel(index, delta) {
      const next = index + delta;
      if (next < 0 || next >= state.config.models.length) return;
      const rows = state.config.models;
      [rows[index], rows[next]] = [rows[next], rows[index]];
      rows.forEach((m, i) => m.order = i + 1);
      renderModels();
    }

    function collectConfig() {
      state.config.admin_token = els.adminToken.value.trim();
      state.config.public_api_keys = els.publicKeys.value.split(/\n+/).map(v => v.trim()).filter(Boolean);
      state.config.providers.forEach(p => {
        p.id = slug(p.id || p.name);
        p.name = String(p.name || p.id).trim();
        p.base_url = String(p.base_url || '').trim().replace(/\/+$/, '');
      });
      state.config.chains = (state.config.chains || []).map((c, i) => ({
        id: slug(c.id || c.name),
        name: String(c.name || c.id).trim(),
        steps: (c.steps || []).map(step => ({
          provider_id: slug(step.provider_id),
          upstream_name: String(step.upstream_name || '').trim()
        })).filter(step => step.provider_id && step.upstream_name),
        order: i + 1
      }));
      state.config.models.forEach((m, i) => {
        m.public_name = String(m.public_name || '').trim();
        m.chain_id = slug(m.chain_id);
        if (m.chain_id) {
          const step = firstChainStep(m.chain_id);
          if (step) {
            m.provider_id = step.provider_id;
            m.upstream_name = step.upstream_name;
          }
        }
        m.upstream_name = String(m.upstream_name || m.public_name).trim();
        m.provider_id = slug(m.provider_id);
        m.chain = (m.chain || []).map(step => ({
          provider_id: slug(step.provider_id),
          upstream_name: String(step.upstream_name || '').trim()
        })).filter(step => step.provider_id && step.upstream_name);
        m.order = i + 1;
      });
      return state.config;
    }

    async function load() {
      setStatus('Loading...');
      if (els.authPrompt.style.display !== 'none') setAuthStatus('Checking...');
      try {
        state.config = await api('/admin/api/config');
        hydrateForm();
        setStatus('Loaded', 'ok');
      } catch (err) {
        if (els.authPrompt.style.display !== 'none') {
          const message = String(err.message || '').toLowerCase().includes('wrong') || token()
            ? 'Wrong token'
            : err.message;
          setAuthStatus(message, 'error');
        } else {
          setStatus(err.message, 'error');
        }
      }
    }

    async function save() {
      setStatus('Saving...');
      try {
        const next = collectConfig();
        rememberAdminToken(next.admin_token);
        state.config = await api('/admin/api/config', { method: 'PUT', body: JSON.stringify(next) });
        hydrateForm();
        setStatus('Saved', 'ok');
      } catch (err) {
        setStatus(err.message, 'error');
      }
    }

    async function discoverProvider(providerID) {
      if (!providerID) return;
      setStatus('Discovering ' + providerName(providerID) + '...');
      try {
        const result = await api('/admin/api/discover', { method: 'POST', body: JSON.stringify({ provider_id: providerID }) });
        let added = 0;
        let skipped = 0;
        result.models.forEach(name => {
          if (hasProviderModel(providerID, name)) {
            skipped++;
            return;
          }
          state.config.models.push({
            public_name: uniquePublicName(name, providerID),
            provider_id: providerID,
            upstream_name: name,
            enabled: false,
            order: state.config.models.length + 1
          });
          added++;
        });
        renderChains();
        renderModels();
        setStatus('Discovered ' + result.models.length + ' models from ' + providerName(providerID) + '; added ' + added + ', skipped ' + skipped, 'ok');
      } catch (err) {
        setStatus(err.message, 'error');
      }
    }

    async function activateModels() {
      const providerID = els.activateProvider.value;
      const models = els.activateModels.value.split(/\n+/).map(v => v.trim()).filter(Boolean);
      if (!providerID || models.length === 0) return;
      setStatus('Activating...');
      try {
        const result = await api('/admin/api/activate', {
          method: 'POST',
          body: JSON.stringify({
            provider_id: providerID,
            models,
            pull: els.activatePull.checked,
            enable: els.activateEnabled.checked
          })
        });
        state.config = result.config;
        hydrateForm();
        const failures = result.results.filter(r => !r.ok);
        setStatus(failures.length ? ('Activated with ' + failures.length + ' errors') : ('Activated ' + models.length + ' models'), failures.length ? 'error' : 'ok');
      } catch (err) {
        setStatus(err.message, 'error');
      }
    }

    function togglePassword(id, button) {
      const input = document.getElementById(id);
      if (!input) return;
      input.type = input.type === 'password' ? 'text' : 'password';
      button.textContent = input.type === 'password' ? 'Show' : 'Hide';
    }

    document.getElementById('saveBtn').addEventListener('click', save);
    document.getElementById('reloadBtn').addEventListener('click', load);
    document.getElementById('activateBtn').addEventListener('click', activateModels);
    document.body.addEventListener('click', e => {
      const id = e.target.dataset?.togglePassword;
      if (id) togglePassword(id, e.target);
    });
    document.getElementById('headerAdminTokenBtn').addEventListener('click', () => {
      if (rememberAdminToken(els.headerAdminToken.value)) load();
    });
    els.headerAdminToken.addEventListener('keydown', e => {
      if (e.key === 'Enter') {
        if (rememberAdminToken(els.headerAdminToken.value)) load();
      }
    });
    document.getElementById('generatePublicKeyBtn').addEventListener('click', async () => {
      const lines = els.publicKeys.value.split(/\n+/).map(v => v.trim()).filter(Boolean);
      lines.push(randomKey('rtr'));
      els.publicKeys.value = lines.join('\n');
      await save();
    });
    document.getElementById('addProviderBtn').addEventListener('click', () => {
      const id = 'provider-' + (state.config.providers.length + 1);
      state.config.providers.push({ id, name: '', type: 'openai', base_url: '', api_key: '', enabled: true });
      state.editingProviderID = id;
      hydrateForm();
    });
    document.getElementById('addChainBtn').addEventListener('click', () => {
      const name = 'Chain ' + ((state.config.chains || []).length + 1);
      const first = state.config.models.find(m => m.provider_id && m.upstream_name);
      state.config.chains.push({
        id: uniqueChainID(name),
        name,
        steps: first ? [{ provider_id: first.provider_id, upstream_name: first.upstream_name }] : [],
        order: state.config.chains.length + 1
      });
      renderChains();
      renderModels();
    });
    document.getElementById('addModelBtn').addEventListener('click', () => {
      const provider = els.activateProvider.value || state.config.providers[0]?.id || '';
      state.config.models.push({ public_name: '', provider_id: provider, upstream_name: '', enabled: true, order: state.config.models.length + 1 });
      renderModels();
    });
    els.modelFilter.addEventListener('input', renderModels);
    load();
  </script>
</body>
</html>`
