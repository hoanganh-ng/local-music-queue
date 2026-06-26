/**
 * Options page script for Local Music Queue browser extension.
 * Handles configuration storage and connection testing.
 */

document.addEventListener('DOMContentLoaded', () => {
  const form = document.getElementById('options-form');
  const statusEl = document.getElementById('status');
  const testBtn = document.getElementById('test-btn');

  const apiBaseInput = document.getElementById('api-base');
  const displayNameInput = document.getElementById('display-name');
  const userIdInput = document.getElementById('user-id');
  const sessionTokenInput = document.getElementById('session-token');

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

  // Load saved options
  loadOptions();

  // Handle form submission
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    saveOptions();
  });

  // Handle test connection
  testBtn.addEventListener('click', testConnection);

  /**
   * Loads saved options from chrome.storage.local.
   */
  function loadOptions() {
    chrome.storage.local.get(['apiBase', 'displayName', 'userId', 'sessionToken'], (result) => {
      if (result.apiBase) {
        apiBaseInput.value = result.apiBase;
      }
      if (result.displayName) {
        displayNameInput.value = result.displayName;
      }
      if (typeof result.userId === 'number') {
        userIdInput.value = result.userId;
      }
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
    });
  }

  /**
   * Requests host permission for the API base URL.
   * Must be called from a user gesture (e.g., button click).
   * @param {string} apiBase - The API base URL.
   * @returns {Promise<boolean>} True if permission is granted or already held.
   */
  async function requestHostPermission(apiBase) {
    try {
      const url = new URL(apiBase);
      const origin = `${url.protocol}//${url.host}/*`;

      // Check if we already have permission
      const hasPermission = await chrome.permissions.contains({
        origins: [origin]
      });
      if (hasPermission) return true;

      // Request permission (user must approve via Chrome dialog)
      const granted = await chrome.permissions.request({
        origins: [origin]
      });

      if (!granted) {
        showStatus(
          'Host permission denied. The extension may not be able to reach your server. ' +
          'CORS with Access-Control-Allow-Origin: * may still work.',
          'error'
        );
      }
      return granted;
    } catch {
      // If permission request fails, rely on CORS
      return false;
    }
  }

  /**
   * Saves options to chrome.storage.local.
   */
  async function saveOptions() {
    const apiBase = apiBaseInput.value.trim().replace(/\/+$/, '');
    const displayName = displayNameInput.value.trim();
    const userId = parseInt(userIdInput.value, 10) || 0;
    const sessionToken = sessionTokenInput.value.trim();

    if (!apiBase) {
      showStatus('API Base URL is required.', 'error');
      return;
    }

    if (!displayName) {
      showStatus('Display Name is required.', 'error');
      return;
    }

    // Validate URL format
    try {
      new URL(apiBase);
    } catch {
      showStatus('API Base URL is not a valid URL.', 'error');
      return;
    }

    // Request host permission for the API base URL (non-blocking if denied)
    await requestHostPermission(apiBase);

    const payload = {
      apiBase: apiBase,
      displayName: displayName,
      userId: userId
    };
    // Preserves the existing saved sessionToken when the input is blank —
    // chrome.storage.local.set with the field omitted leaves that key untouched.
    if (sessionToken) {
      payload.sessionToken = sessionToken;
    }

    chrome.storage.local.set(payload, () => {
      if (chrome.runtime.lastError) {
        showStatus('Failed to save: ' + chrome.runtime.lastError.message, 'error');
        return;
      }
      // Clear the field after save so the token is not left visible.
      sessionTokenInput.value = '';
      showStatus('Settings saved successfully.', 'success');
    });
  }

  /**
   * Tests the connection to the Local Music Queue server.
   */
  async function testConnection() {
    const apiBase = apiBaseInput.value.trim().replace(/\/+$/, '');

    if (!apiBase) {
      showStatus('Enter an API Base URL first.', 'error');
      return;
    }

    testBtn.disabled = true;
    testBtn.textContent = 'Testing...';

    try {
      const response = await fetch(`${apiBase}/api/queue`, {
        method: 'GET',
        headers: {
          'Accept': 'application/json'
        }
      });

      if (response.ok) {
        showStatus('Connection successful! Server is reachable.', 'success');
      } else {
        showStatus(`Server responded with HTTP ${response.status}. Check your server configuration.`, 'error');
      }
    } catch (err) {
      showStatus(`Connection failed: ${err.message}. Check the URL and server status.`, 'error');
    } finally {
      testBtn.disabled = false;
      testBtn.textContent = 'Test Connection';
    }
  }

  /**
   * Shows a status message.
   * @param {string} message - The message to display.
   * @param {'success'|'error'} type - The message type.
   */
  function showStatus(message, type) {
    statusEl.textContent = message;
    statusEl.className = `status ${type}`;

    // Auto-hide after 5 seconds
    setTimeout(() => {
      statusEl.className = 'status';
    }, 5000);
  }
});
