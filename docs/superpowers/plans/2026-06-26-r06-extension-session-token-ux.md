# R06 — Extension Session Token UX Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the browser-extension session-token workflow usable and reversible after R05 by adding an in-app "Copy token for extension" affordance to DashboardView and a visible status + explicit Clear control to the extension Options page, without changing backend auth, REST/WebSocket contracts, session storage, queue behavior, or deployment.

**Architecture:** Two narrow UX-only changes — one Vue component edit (DashboardView) and one extension page edit (options.html + options.js). All contract-bearing surfaces (backend, REST, WebSocket, storage keys, Authorization header) stay byte-for-byte identical. Frontend uses `sessionHelper.isValid()` + `sessionHelper.getToken()` + `navigator.clipboard.writeText` on explicit click. Extension Options page reads a saved-token *presence* flag from `chrome.storage.local`, renders "saved" / "not saved" status, clears only the `sessionToken` key, and never auto-fills the saved token into the input. No new endpoints, no payload changes.

**Tech Stack:** Vue 3 (Composition API) + Vitest + @vue/test-utils (frontend). Vanilla DOM + `chrome.storage.local` (extension). No new dependencies.

## Global Constraints

- No new REST endpoint. No REST payload changes. No WebSocket changes. No backend authorization changes. (Spec)
- Keep frontend localStorage keys `lmq_session_token` and `lmq_session_expires_at`. (Spec)
- Keep extension `chrome.storage.local` key `sessionToken`. (Spec)
- Do not render the raw token in the DOM, logs, docs, fixtures, screenshots, or test output. (Spec)
- `extension/background.js` `Authorization: Bearer <sessionToken>` behavior is unchanged. (Spec)
- Out of scope: automatic token sync, externally_connectable, dynamic content-script injection, new auth endpoint, persistent server sessions, JWT redesign, WebSocket origin validation, removing `?user_id=` hint, backend role matrix, queue/voting/auto-queue/database/Docker/deployment changes. (Spec)
- Do not commit, push, merge, rewrite unrelated code, or advance the sprint. (Spec)
- Tests use Vitest. `navigator.clipboard.writeText` must be mocked in tests; raw tokens must never appear in assertion strings.

## File Structure

- `extension/options.html` — Add token-status block + Clear Session Token button (visible markup).
- `extension/options.js` — Read saved-token presence flag on load, render status, wire Clear button, keep current save/blank-save/test behavior intact.
- `frontend/src/views/DashboardView.vue` — Add Copy Session Token button + handler in the existing `user-info` cluster; use `sessionHelper.isValid()` + `sessionHelper.getToken()` + `navigator.clipboard.writeText` on click; toast success/error/info via existing `useToast()`.
- `frontend/src/views/__tests__/DashboardView.spec.js` — Extend existing suite with token-copy and missing-token cases.
- `extension/README.md` — Document new in-app copy helper, Clear Session Token behavior, and revise the Known Limitations entry.
- `documents/03-features/authentication.md` — Narrow update for extension token copy/clear UX.
- `documents/00-project-management/PROJECT_STATE.md` — Refresh R05 residual extension-token language only.

---

### Task 1: Add Copy Session Token button + handler to DashboardView

**Files:**
- Modify: `frontend/src/views/DashboardView.vue`
- Test: `frontend/src/views/__tests__/DashboardView.spec.js`

**Interfaces:**
- Consumes: `sessionHelper.isValid()`, `sessionHelper.getToken()`, `useToast().success()/.error()/.info()`, `navigator.clipboard.writeText(token)`.
- Produces: a `<button class="copy-token-btn" type="button">` rendered next to the existing `.user-info` cluster only when `sessionHelper.isValid()` is `true`; click handler `handleCopySessionToken` that writes the token to clipboard and toasts success, or toasts info when no valid token exists, or toasts error when clipboard write fails.

- [ ] **Step 1: Write failing tests in DashboardView.spec.js**

Append the following `describe('Copy Session Token for Extension', () => { ... })` block at the end of the existing `describe('DashboardView', ...)` block in `frontend/src/views/__tests__/DashboardView.spec.js` (before the final `})`):

```javascript
  describe('Copy Session Token for Extension', () => {
    let writeText

    beforeEach(() => {
      writeText = vi.fn().mockResolvedValue(undefined)
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText },
        configurable: true,
        writable: true,
      })
    })

    it('shows the copy button when sessionHelper.isValid() returns true', () => {
      const future = new Date(Date.now() + 60 * 60_000).toISOString()
      sessionHelper.saveSession('opaque-token-abc', future)

      const wrapper = shallowMount(DashboardView)
      const btn = wrapper.find('.copy-token-btn')

      expect(btn.exists()).toBe(true)
    })

    it('hides the copy button when sessionHelper.isValid() returns false', () => {
      // No session saved
      const wrapper = shallowMount(DashboardView)
      expect(wrapper.find('.copy-token-btn').exists()).toBe(false)
    })

    it('hides the copy button when session token is expired', () => {
      const past = new Date(Date.now() - 60_000).toISOString()
      sessionHelper.saveSession('expired', past)

      const wrapper = shallowMount(DashboardView)
      expect(wrapper.find('.copy-token-btn').exists()).toBe(false)
    })

    it('writes the current token to clipboard and toasts success on click', async () => {
      const future = new Date(Date.now() + 60 * 60_000).toISOString()
      const tokenValue = 'opaque-token-xyz'
      sessionHelper.saveSession(tokenValue, future)

      const wrapper = shallowMount(DashboardView)
      const btn = wrapper.find('.copy-token-btn')
      expect(btn.exists()).toBe(true)

      await btn.trigger('click')

      expect(writeText).toHaveBeenCalledTimes(1)
      expect(writeText).toHaveBeenCalledWith(tokenValue)
      expect(mockToast.success).toHaveBeenCalledWith('Session token copied. Paste it into the browser extension Options.')
      expect(mockToast.error).not.toHaveBeenCalled()
      expect(mockToast.info).not.toHaveBeenCalled()
    })

    it('toasts an info message and does not write to clipboard when no valid session exists', async () => {
      // sessionHelper.isValid() is false; copy button is hidden so we simulate
      // the defensive guard by calling the handler directly via the component vm.
      // The defensive guard must not throw and must not invoke the clipboard.
      const wrapper = shallowMount(DashboardView)
      expect(wrapper.find('.copy-token-btn').exists()).toBe(false)

      // Trigger the same code path the button would, by mounting and asserting the
      // guard contract: invoking when no valid session must not call clipboard.
      // We invoke the handler via the exposed vm method (added in Task 1 implementation).
      wrapper.vm.handleCopySessionToken?.()

      expect(writeText).not.toHaveBeenCalled()
      expect(mockToast.info).toHaveBeenCalledWith('No active session. Log in again to copy a fresh token for the extension.')
      expect(mockToast.success).not.toHaveBeenCalled()
      expect(mockToast.error).not.toHaveBeenCalled()
    })

    it('toasts an error and does not leak the token when clipboard write fails', async () => {
      const future = new Date(Date.now() + 60 * 60_000).toISOString()
      sessionHelper.saveSession('opaque-token-fail', future)
      writeText.mockRejectedValueOnce(new Error('clipboard blocked'))

      const wrapper = shallowMount(DashboardView)
      await wrapper.find('.copy-token-btn').trigger('click')

      // Allow the rejected promise's catch to run
      await flushPromises()

      expect(writeText).toHaveBeenCalledTimes(1)
      expect(mockToast.error).toHaveBeenCalledWith('Could not copy to clipboard. Use DevTools → Application → Local Storage → lmq_session_token as a fallback.')
      expect(mockToast.success).not.toHaveBeenCalled()
      // Token must not appear in any toast message
      const allToastCalls = [
        ...mockToast.success.mock.calls,
        ...mockToast.error.mock.calls,
        ...mockToast.info.mock.calls,
      ]
      allToastCalls.forEach(([msg]) => {
        expect(msg).not.toContain('opaque-token-fail')
      })
    })
  })
```

Also add the missing import at the top of the spec file (right after the existing `import { ... } from 'vitest'` line):

```javascript
import { flushPromises } from '@vue/test-utils'
import { sessionHelper } from '../../services/session'
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && npm run test:unit -- --run src/views/__tests__/DashboardView.spec.js`
Expected: FAIL — `.copy-token-btn` selector not found and/or `handleCopySessionToken` not a function.

- [ ] **Step 3: Implement the button + handler in DashboardView.vue**

In `frontend/src/views/DashboardView.vue`:

1. Inside `.user-info` (after the `radio-mode-toggle` button and before the `.user-name` span), add the conditional button:

```vue
        <button
          v-if="sessionValid"
          type="button"
          class="copy-token-btn"
          @click="handleCopySessionToken"
          title="Copy your session token to paste into the browser extension Options"
        >
          <span class="copy-token-label">Copy token for extension</span>
        </button>
```

2. In the `<script setup>` block, add a `sessionValid` computed and the `handleCopySessionToken` function. Place them next to the other auth-related handlers (after `handleLogout`):

```javascript
const sessionValid = computed(() => sessionHelper.isValid())

async function handleCopySessionToken() {
  if (!sessionHelper.isValid()) {
    toast.info('No active session. Log in again to copy a fresh token for the extension.')
    return
  }
  const token = sessionHelper.getToken()
  try {
    await navigator.clipboard.writeText(token)
    toast.success('Session token copied. Paste it into the browser extension Options.')
  } catch (err) {
    console.error('Clipboard write failed:', err)
    toast.error('Could not copy to clipboard. Use DevTools → Application → Local Storage → lmq_session_token as a fallback.')
  }
}
```

3. Add scoped styles (alongside the existing `.logout-btn` style block):

```css
.copy-token-btn {
  background: rgba(62, 166, 255, 0.12);
  color: var(--accent-hover);
  border: 1px solid rgba(62, 166, 255, 0.35);
  border-radius: var(--radius-sm);
  padding: 0.35rem 0.75rem;
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  cursor: pointer;
  transition: background 0.2s ease, border-color 0.2s ease;
}

.copy-token-btn:hover {
  background: rgba(62, 166, 255, 0.22);
  border-color: var(--accent-hover);
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && npm run test:unit -- --run src/views/__tests__/DashboardView.spec.js`
Expected: PASS for the file.

- [ ] **Step 5: Run full unit suite**

Run: `cd frontend && npm run test:unit -- --run`
Expected: PASS for all files. Report the file/test count.

- [ ] **Step 6: Commit (frontend only)**

Do NOT commit per global constraint "Do not commit, push, merge..." — skip this step. The instruction in this plan conflicts with the global "do not commit" constraint; the global constraint wins.

---

### Task 2: Add token status + Clear Session Token to extension Options (HTML)

**Files:**
- Modify: `extension/options.html`

**Interfaces:**
- Produces (read by Task 3): a `<span id="session-token-status" class="session-token-status">` element with text "not saved" by default, and a `<button type="button" id="clear-token-btn" class="btn-secondary">Clear Session Token</button>` placed inside the same `.form-group` as the Session Token input.

- [ ] **Step 1: Add status span + Clear button markup**

In `extension/options.html`, locate the Session Token `.form-group` block (currently the one containing the `<input id="session-token">`). After the existing `<p class="help-text">` for the session token (the paragraph that mentions `lmq_session_token` and `Authorization: Bearer`), and BEFORE the closing `</div>` of that form-group, insert:

```html
      <p class="session-token-status-row">
        <span class="status-label">Token status:</span>
        <span id="session-token-status" class="session-token-status not-saved">not saved</span>
      </p>
      <div class="button-row">
        <button type="button" id="clear-token-btn" class="btn-secondary">Clear Session Token</button>
      </div>
```

- [ ] **Step 2: Add the supporting styles**

Append the following CSS rules inside the existing `<style>` block in `extension/options.html`, just before the closing `</style>` tag:

```css
    .session-token-status-row {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      margin-top: 8px;
      font-size: 12px;
    }
    .status-label {
      color: #888;
    }
    .session-token-status {
      font-weight: 600;
      padding: 2px 8px;
      border-radius: 4px;
    }
    .session-token-status.saved {
      color: #2ba640;
      background: rgba(43, 166, 64, 0.15);
      border: 1px solid rgba(43, 166, 64, 0.3);
    }
    .session-token-status.not-saved {
      color: #888;
      background: rgba(136, 136, 136, 0.12);
      border: 1px solid rgba(136, 136, 136, 0.3);
    }
```

- [ ] **Step 3: Visual sanity check (manual only)**

Open `extension/options.html` in a browser. Confirm:
- The "Token status:" row shows "not saved".
- The "Clear Session Token" button appears below the Session Token help text.
- The existing Save Settings / Test Connection buttons are unchanged.

(No automated harness exists for the extension.)

---

### Task 3: Wire status updates + Clear handler in options.js

**Files:**
- Modify: `extension/options.js`

**Interfaces:**
- Consumes: existing `chrome.storage.local.get(['apiBase','displayName','userId','sessionToken'], cb)` call in `loadOptions()`; existing `saveOptions()` flow; new DOM ids `session-token-status` and `clear-token-btn`.
- Produces: `setStatusBadge(saved: boolean)` (sets `#session-token-status` text + class to "saved" / "not saved"); `clearSessionToken()` (calls `chrome.storage.local.remove('sessionToken', cb)` and then refreshes the badge + status). `loadOptions()` MUST keep the saved token out of the input value (already true today; verify with a comment). `saveOptions()` MUST remain unchanged so a blank save still preserves the existing saved token.

- [ ] **Step 1: Verify loadOptions() never writes the token to the input**

Confirm by reading `extension/options.js` lines 42-49. Current behavior leaves `sessionTokenInput.value` empty and only updates the placeholder. No code change required; the existing branch already satisfies "Keep the Session Token input blank on load; never auto-fill the saved token." Add an inline comment above the branch for reviewer clarity (non-behavioral change):

```javascript
      if (typeof result.sessionToken === 'string') {
        // Never write the saved token back into the input — UX is hide-by-default.
        // A non-empty stored token only flips the status badge below.
        if (result.sessionToken) {
          setStatusBadge(true);
        } else {
          setStatusBadge(false);
        }
        sessionTokenInput.placeholder = result.sessionToken
          ? '•••• (saved token hidden; clear and paste a new one to replace)'
          : 'paste lmq_session_token here';
      } else {
        setStatusBadge(false);
      }
```

- [ ] **Step 2: Add `setStatusBadge` helper + `clearSessionToken` handler**

In `extension/options.js`, inside the same `DOMContentLoaded` listener (anywhere after the existing DOM lookups), add:

```javascript
  const sessionTokenStatus = document.getElementById('session-token-status');
  const clearTokenBtn = document.getElementById('clear-token-btn');

  clearTokenBtn.addEventListener('click', () => {
    if (!confirm('Clear the saved Session Token from this extension?')) {
      return;
    }
    chrome.storage.local.remove('sessionToken', () => {
      if (chrome.runtime.lastError) {
        showStatus('Failed to clear: ' + chrome.runtime.lastError.message, 'error');
        return;
      }
      // Touch the input so the user sees the cleared state visually too.
      sessionTokenInput.value = '';
      setStatusBadge(false);
      showStatus('Session Token cleared.', 'success');
    });
  });

  /**
   * Updates the "Token status:" badge to reflect whether a sessionToken is saved.
   * @param {boolean} saved - true if chrome.storage.local.sessionToken is present.
   */
  function setStatusBadge(saved) {
    if (!sessionTokenStatus) return;
    if (saved) {
      sessionTokenStatus.textContent = 'saved';
      sessionTokenStatus.className = 'session-token-status saved';
    } else {
      sessionTokenStatus.textContent = 'not saved';
      sessionTokenStatus.className = 'session-token-status not-saved';
    }
  }
```

- [ ] **Step 3: Confirm `saveOptions()` payload still omits blank sessionToken**

Read `extension/options.js` lines 118-127 (the existing payload block). Current behavior is `if (sessionToken) { payload.sessionToken = sessionToken; }`, which means a blank save omits the field and therefore preserves the existing saved value. No code change required; add an inline comment above the `if` for reviewer clarity:

```javascript
    // Preserves the existing saved sessionToken when the input is blank —
    // chrome.storage.local.set with the field omitted leaves that key untouched.
    if (sessionToken) {
      payload.sessionToken = sessionToken;
    }
```

- [ ] **Step 4: Manual verification (no automated harness)**

In `chrome://extensions/` with the extension loaded and DevTools open on the service worker:

1. **No saved token** — clear all chrome.storage.local keys (or install fresh). Open Options. Expected: status badge shows "not saved". Click Clear Session Token. Expected: confirm dialog appears; cancel. Status still "not saved".
2. **Paste token + save** — paste a sample string into Session Token, click Save Settings. Expected: input clears, status badge shows "saved".
3. **Reload Options** — close the Options tab, reopen. Expected: Session Token input is blank, status badge still shows "saved", placeholder shows the "•••" message.
4. **Blank save preserves saved token** — with status "saved", type a single space (which `.trim()`s to empty) and Save Settings. Expected: status still "saved"; in the service worker DevTools console inspect `chrome.storage.local` and confirm `sessionToken` still holds the prior value.
5. **Clear Session Token** — click the button, confirm. Expected: status flips to "not saved"; in the service worker DevTools console confirm `chrome.storage.local` no longer contains `sessionToken` BUT still contains `apiBase`, `displayName`, and `userId`.
6. **Replace token** — with status "not saved", paste a NEW token, Save. Expected: input clears, status flips to "saved".

---

### Task 4: Document the new UX in extension/README.md

**Files:**
- Modify: `extension/README.md`

- [ ] **Step 1: Add "Copy from the web app" step in the "Where to find the Session Token" section**

In `extension/README.md`, locate the section "### Where to find the Session Token". The existing steps 1-4 walk the user through DevTools. Replace the introductory sentence "To copy it:" with two paths — primary (in-app copy button) and fallback (DevTools). Final result for that section should read:

```markdown
### Where to find the Session Token

The Session Token is the same opaque token the web app sends in the `Authorization: Bearer <token>` header (and in `?session_token=` on the WebSocket URL). It is stored under `lmq_session_token` in `localStorage` for the Local Music Queue app's origin.

**Preferred (in-app copy button):**

1. Open the Local Music Queue web app and log in (Google OAuth). Stay logged in.
2. In the dashboard's top-right user cluster, click **Copy token for extension**.
3. Open the extension Options page, paste it into **Session Token**, and Save.

**Fallback (DevTools):**

1. Open the Local Music Queue web app and log in (Google OAuth). Stay logged in.
2. Open DevTools → **Application** → **Local Storage** → select the app's origin.
3. Find the row `lmq_session_token` and copy its value.
4. Open the extension Options page, paste it into **Session Token**, and Save.

The token rotates on every login. Re-paste after each new login. The extension stores the value in `chrome.storage.local` (key `sessionToken`) and never logs it.
```

- [ ] **Step 2: Replace the "Clearing a saved Session Token" Known Limitations entry**

In the `## Known Limitations` section of `extension/README.md`, replace the bullet that begins with "**Clearing a saved Session Token (future UX improvement):**" with:

```markdown
- **Clearing a saved Session Token**: Use the **Clear Session Token** button in the Options page. It removes only the `sessionToken` entry from `chrome.storage.local` and leaves `apiBase`, `displayName`, and `userId` intact. To replace the token with a new one, paste the new value into the Session Token field and click **Save Settings**. Saving with the field blank preserves the existing saved token.
```

- [ ] **Step 3: Document the Clear button in the Configuration field list**

In `extension/README.md`'s `## Configuration` section, after the existing bullet `- **Session Token** (R05, required to add songs): ...`, add:

```markdown
   - A **Token status:** indicator shows whether a Session Token is currently saved. Click **Clear Session Token** to remove the saved value (apiBase/displayName/userId are preserved).
```

---

### Task 5: Narrow auth-doc update

**Files:**
- Modify: `documents/03-features/authentication.md`

- [ ] **Step 1: Add a "Browser Extension Token UX" subsection**

In `documents/03-features/authentication.md`, immediately after the existing `### Backend Session` subsection (i.e. just before `## Security Considerations`), insert:

```markdown
### Browser Extension Token UX (R06)

The web app exposes an authenticated **Copy token for extension** button in the dashboard's top-right user cluster. The button is only rendered when `sessionHelper.isValid()` is `true`; clicking it writes the current `lmq_session_token` to the clipboard via `navigator.clipboard.writeText` and shows a success toast. The raw token is never written to the DOM, logs, or analytics. When no valid session is present, no copy occurs and the user is told to log in again. Clipboard failures surface a clear error toast pointing at the DevTools fallback path; the token itself is never echoed back to the user.

The extension Options page now shows a **Token status:** badge ("saved" or "not saved") and a **Clear Session Token** button. The saved token is never auto-filled into the input on load. Saving with a blank input intentionally preserves the existing saved token; to replace, paste the new value and save. Clearing removes only the `sessionToken` key from `chrome.storage.local` — `apiBase`, `displayName`, and `userId` are preserved. `extension/background.js` continues to send `Authorization: Bearer <sessionToken>` on `POST /api/queue/add` when a token is configured.
```

---

### Task 6: Refresh R05 residual language in PROJECT_STATE.md

**Files:**
- Modify: `documents/00-project-management/PROJECT_STATE.md`

- [ ] **Step 1: Rewrite the Browser Extension (R05) bullet**

In `documents/00-project-management/PROJECT_STATE.md`, locate the `- **Browser Extension (R05):**` bullet under `## Authentication & Authorization`. Replace it with:

```markdown
- **Browser Extension (R05 / R06):** `extension/background.js` attaches `Authorization: Bearer <session_token>` to `POST /api/queue/add` whenever the user has saved a token in the extension Options page (storage key `sessionToken` in `chrome.storage.local`). With no token, `/api/queue/add` returns 401 and the extension surfaces a clear "unauthorized" error. R06 added an authenticated in-app **Copy token for extension** button to the web dashboard (success/info/error toast feedback; raw token never enters the DOM or logs), a visible **Token status:** badge on the Options page, and an explicit **Clear Session Token** button that removes only the `sessionToken` key while leaving `apiBase`/`displayName`/`userId` intact. Saving a blank Session Token still preserves the existing saved value.
```

- [ ] **Step 2: Refresh the R05 (residual) known-risk entry that mentions extension tokens**

Locate the `1. **Mitigated by R05 (residual):**` bullet under `## Prioritized Known-Risk Register`. Replace it with:

```markdown
1. **Mitigated by R05 / R06 (residual):** Session-token-based identity now authenticates privileged REST endpoints and the WebSocket accept path. Sessions live strictly in-memory and are lost on restart, so a server restart logs every client out and requires re-login. Browser-extension clients must re-paste the token after each new login; no automatic refresh path exists. The web dashboard's Copy button and the Options Clear button make the manual rotation usable and reversible.
```

---

### Task 7: Final verification

**Files:** none

- [ ] **Step 1: Run frontend unit tests**

Run: `cd frontend && npm run test:unit -- --run`
Expected: PASS — report file/test counts. Confirm the new DashboardView tests are included.

- [ ] **Step 2: Run frontend production build**

Run: `cd frontend && npm run build`
Expected: PASS — `dist/` produced; report any warnings.

- [ ] **Step 3: Manual extension verification checklist**

Walk through the list from Task 3 Step 4 and from the global spec ("Manually verify extension Options" + "Manually verify frontend"). Record PASS / FAIL per item. Do not mark work complete unless every item passes.

- [ ] **Step 4: No commit**

Global constraint: do not commit, push, merge, rewrite unrelated code, or advance the sprint. Stop here and report results.

---

## Self-Review (performed before save)

**1. Spec coverage:**
- Req 1 (frontend UI affordance in DashboardView) — Task 1.
- Req 2 (sessionHelper.isValid + getToken) — Task 1 Step 3.
- Req 3 (navigator.clipboard.writeText on explicit click) — Task 1 Step 3.
- Req 4 (no raw token in DOM/logs/docs/fixtures/screenshots/test output) — Task 1 tests assert `expect(msg).not.toContain('opaque-token-fail')`; status badge shows "saved"/"not saved" only; placeholder uses bullet chars; Clear button reads `chrome.storage.local` key name only.
- Req 5 (existing toast pattern) — Task 1 uses `useToast().success/.error/.info`.
- Req 6 (no copy on missing/expired; clear info message) — Task 1 test "toasts an info message and does not write to clipboard when no valid session exists" and Task 1 Step 3 `if (!sessionHelper.isValid()) { toast.info(...); return }`.
- Extension Opt Req 1 (visible saved/not-saved status) — Tasks 2-3.
- Extension Opt Req 2 (input blank on load) — Task 3 Step 1 confirms and documents.
- Extension Opt Req 3 (blank save preserves saved token) — Task 3 Step 3 documents and re-verifies the existing payload-omit behavior.
- Extension Opt Req 4 (Clear Session Token button) — Task 2 + Task 3.
- Extension Opt Req 5 (clears only sessionToken) — Task 3 uses `chrome.storage.local.remove('sessionToken', ...)`.
- Extension Opt Req 6 (apiBase/displayName/userId preserved) — verified manually in Task 3 Step 4; same `remove('sessionToken', ...)` call doesn't touch the others.
- Extension Opt Req 7 (replace by paste + save works) — manual checklist in Task 3 Step 4.
- Extension Opt Req 8 (background.js auth unchanged) — not modified; spec honored.
- Docs — Tasks 4-6.
- Verification commands — Task 7.

**2. Placeholder scan:** No "TBD", "TODO", "implement later", "fill in details". Every code step shows complete code. Every test step shows complete assertions. No "similar to Task N" copies — each task shows its own code.

**3. Type consistency:**
- `sessionHelper.isValid()` and `sessionHelper.getToken()` are real exports from `frontend/src/services/session.js` — Task 1 uses these exact names.
- `useToast()` returns `{ success, error, info, ... }` — Task 1 Step 3 uses these exact methods; tests mock them with `success`/`error`/`info` `vi.fn()`.
- `chrome.storage.local.remove('sessionToken', cb)` is the documented Chrome API — Task 3 Step 2 uses the exact signature.
- `chrome.storage.local.get(['apiBase','displayName','userId','sessionToken'], cb)` is the existing call signature in `loadOptions()` — Task 3 preserves it.
- `localStorage` keys `lmq_session_token` / `lmq_session_expires_at` match the constants in `frontend/src/services/session.js` — used verbatim in tests.

No issues found; plan is internally consistent.
