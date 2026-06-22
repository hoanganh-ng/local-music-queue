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
    chrome.storage.local.get(['apiBase', 'displayName', 'userId'], (result) => {
      if (result.apiBase) {
        apiBaseInput.value = result.apiBase;
      }
      if (result.displayName) {
        displayNameInput.value = result.displayName;
      }
      if (typeof result.userId === 'number') {
        userIdInput.value = result.userId;
      }
    });
  }

  /**
   * Saves options to chrome.storage.local.
   */
  function saveOptions() {
    const apiBase = apiBaseInput.value.trim().replace(/\/+$/, '');
    const displayName = displayNameInput.value.trim();
    const userId = parseInt(userIdInput.value, 10) || 0;

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

    chrome.storage.local.set({
      apiBase: apiBase,
      displayName: displayName,
      userId: userId
    }, () => {
      if (chrome.runtime.lastError) {
        showStatus('Failed to save: ' + chrome.runtime.lastError.message, 'error');
        return;
      }
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
