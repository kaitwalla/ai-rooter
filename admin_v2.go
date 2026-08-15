package main

import "strings"

func mustReplace(html, old, new string, n int) string {
	result := strings.Replace(html, old, new, n)
	if result == html {
		panic("mustReplace: anchor not found: " + old[:min(len(old), 50)])
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var adminHTMLV2 = func() string {
	html := adminHTML
	oldAccess := `      <section>
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
      </section>`
	newAccess := `      <section>
        <div class="toolbar"><h2>API keys</h2><button id="refreshAPIKeysBtn" type="button">Refresh</button></div>
        <input id="adminToken" type="hidden"><textarea id="publicKeys" class="hidden"></textarea><button id="generatePublicKeyBtn" class="hidden" type="button"></button>
        <div class="muted" style="margin-bottom:12px;">Keys are stored as hashes. The full key is shown only once when created.</div>
        <div id="apiKeyCards" class="stack" style="gap:8px;"></div>
        <div style="border-top:1px solid var(--line);margin-top:14px;padding-top:12px;">
          <label>Name</label><input id="newAPIKeyName" placeholder="Hermes, CI, Admin laptop…">
          <label>Permissions</label>
          <div id="apiKeyPermissions" class="grid-2" style="margin-bottom:10px;">
            <label><input class="checkbox" type="checkbox" value="chat" checked>Chat</label>
            <label><input class="checkbox" type="checkbox" value="responses" checked>Responses</label>
            <label><input class="checkbox" type="checkbox" value="embeddings" checked>Embeddings</label>
            <label><input class="checkbox" type="checkbox" value="models.read" checked>Models read</label>
            <label><input class="checkbox" type="checkbox" value="models.write">Models write</label>
            <label><input class="checkbox" type="checkbox" value="providers.read">Providers read</label>
            <label><input class="checkbox" type="checkbox" value="providers.write">Providers write</label>
            <label><input class="checkbox" type="checkbox" value="chains.read">Chains read</label>
            <label><input class="checkbox" type="checkbox" value="chains.write">Chains write</label>
            <label><input class="checkbox" type="checkbox" value="keys.read">Keys read</label>
            <label><input class="checkbox" type="checkbox" value="keys.write">Keys write</label>
            <label><input class="checkbox" type="checkbox" value="config.read">Config read</label>
            <label><input class="checkbox" type="checkbox" value="config.write">Config write</label>
          </div>
          <label>Expires (optional)</label><input id="newAPIKeyExpires" type="datetime-local">
          <div class="actions" style="margin-top:10px;"><button id="createAPIKeyBtn" class="primary" type="button">Create key</button></div>
          <div id="newAPIKeySecret" class="hidden" style="margin-top:12px;padding:10px;border:1px solid var(--line);border-radius:6px;"></div>
        </div>
      </section>`
	html = strings.Replace(html, oldAccess, newAccess, 1)
	html = strings.ReplaceAll(html, "Admin UI token", "API key")
	html = strings.ReplaceAll(html, "Use token", "Use key")
	html = mustReplace(html, "      els.adminToken.value = state.config.admin_token || '';", "      els.adminToken.value = '';", 1)
	html = mustReplace(html, "      els.publicKeys.value = (state.config.public_api_keys || []).join('\\n');", "      els.publicKeys.value = '';", 1)
	html = mustReplace(html, "      state.config.admin_token = els.adminToken.value.trim();\n      state.config.public_api_keys = els.publicKeys.value.split(/\\n+/).map(v => v.trim()).filter(Boolean);", "      state.config.admin_token = '';\n      state.config.public_api_keys = [];", 1)
	html = mustReplace(html, "        rememberAdminToken(next.admin_token);\n", "", 1)
	html = strings.Replace(html, "        hydrateForm();\n        setStatus('Loaded', 'ok');", "        hydrateForm();\n        if (typeof refreshAPIKeys === 'function') refreshAPIKeys();\n        setStatus('Loaded', 'ok');", 1)

	extra := `<script>
async function refreshAPIKeys() {
  const box = document.getElementById('apiKeyCards');
  if (!box) return;
  try {
    const result = await api('/admin/api/api-keys');
    box.innerHTML = '';
    (result.data || []).forEach(key => {
      const card = document.createElement('div');
      card.className = 'provider';
      const used = key.last_used_at ? new Date(key.last_used_at).toLocaleString() : 'Never';
      const expires = key.expires_at ? new Date(key.expires_at).toLocaleString() : 'Never';
      card.innerHTML = '<div class="provider-summary"><strong>' + escapeHTML(key.name) + '</strong>' +
        '<span class="muted">' + escapeHTML(key.prefix) + '… · ' + (key.enabled ? 'Enabled' : 'Disabled') + '</span>' +
        '<span class="muted">' + escapeHTML((key.permissions || []).join(', ')) + '</span>' +
        '<span class="muted">Last used: ' + escapeHTML(used) + ' · Expires: ' + escapeHTML(expires) + '</span></div>' +
        '<div class="provider-actions"><button class="danger" type="button">Revoke</button></div>';
      card.querySelector('button').addEventListener('click', async () => {
        if (!confirm('Revoke ' + key.name + '?')) return;
        const res = await fetch('/admin/api/api-keys/' + encodeURIComponent(key.id), { method:'DELETE', headers:headers() });
        if (!res.ok) { setStatus(await res.text(), 'error'); return; }
        await refreshAPIKeys();
      });
      box.appendChild(card);
    });
    if (!(result.data || []).length) box.innerHTML = '<div class="muted">No API keys configured.</div>';
  } catch (err) { setStatus(err.message, 'error'); }
}

document.getElementById('refreshAPIKeysBtn')?.addEventListener('click', refreshAPIKeys);
document.getElementById('createAPIKeyBtn')?.addEventListener('click', async () => {
  const permissions = Array.from(document.querySelectorAll('#apiKeyPermissions input:checked')).map(el => el.value);
  const expiresRaw = document.getElementById('newAPIKeyExpires').value;
  const body = { name: document.getElementById('newAPIKeyName').value.trim(), permissions };
  if (expiresRaw) body.expires_at = new Date(expiresRaw).toISOString();
  try {
    const created = await api('/admin/api/api-keys', { method:'POST', body:JSON.stringify(body) });
    const secret = document.getElementById('newAPIKeySecret');
    secret.classList.remove('hidden');
    secret.innerHTML = '<strong>Copy this key now. It will not be shown again.</strong><div class="field-row" style="margin-top:8px;"></div>';
    const fieldRow = secret.querySelector('.field-row');
    const input = document.createElement('input');
    input.id = 'createdAPIKeyValue';
    input.readOnly = true;
    input.value = created.key;
    fieldRow.appendChild(input);
    const copyBtn = document.createElement('button');
    copyBtn.type = 'button';
    copyBtn.id = 'copyCreatedAPIKey';
    copyBtn.textContent = 'Copy';
    copyBtn.addEventListener('click', () => navigator.clipboard.writeText(created.key));
    fieldRow.appendChild(copyBtn);
    document.getElementById('newAPIKeyName').value = '';
    await refreshAPIKeys();
  } catch (err) { setStatus(err.message, 'error'); }
});
setTimeout(refreshAPIKeys, 0);
</script>`
	return strings.Replace(html, "</body>", extra+"\n</body>", 1)
}()
