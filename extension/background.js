/**
 * Background service worker for Local Music Queue browser extension.
 * Handles API communication with the Local Music Queue server.
 * Relies on server CORS (Access-Control-Allow-Origin: *) or optional host
 * permission granted by the user during configuration.
 */

// Import shared utilities (available via manifest content_scripts registration)
// Note: In service workers, we can't use content script globals directly.
// We re-implement the minimal needed functions here.

/**
 * Validates a YouTube video ID format.
 * @param {string} id - The video ID to validate.
 * @returns {boolean} True if valid.
 */
function isValidVideoId(id) {
  if (typeof id !== 'string') return false;
  return /^[A-Za-z0-9_-]{11}$/.test(id);
}

/**
 * Builds a canonical YouTube watch URL from a video ID.
 * @param {string} videoId - The YouTube video ID.
 * @returns {string|null} The canonical URL or null if invalid.
 */
function buildWatchUrl(videoId) {
  if (!isValidVideoId(videoId)) return null;
  return `https://www.youtube.com/watch?v=${videoId}`;
}

/**
 * Loads extension configuration from chrome.storage.local.
 * @returns {Promise<{apiBase: string, displayName: string, userId: number, sessionToken: string}|null>}
 */
async function loadConfig() {
  return new Promise((resolve) => {
    chrome.storage.local.get(['apiBase', 'displayName', 'userId', 'sessionToken'], (result) => {
      if (!result.apiBase || !result.displayName) {
        resolve(null);
        return;
      }
      resolve({
        apiBase: result.apiBase.replace(/\/+$/, ''), // Remove trailing slashes
        displayName: result.displayName,
        userId: typeof result.userId === 'number' ? result.userId : 0,
        // sessionToken is optional: when set, it is sent as Bearer auth.
        // When unset, the server rejects /api/queue/add (R05 auth gate).
        sessionToken: typeof result.sessionToken === 'string' ? result.sessionToken : ''
      });
    });
  });
}

/**
 * Sends a song to the Local Music Queue API.
 * @param {string} videoId - The YouTube video ID.
 * @param {string} url - The canonical watch URL.
 * @returns {Promise<{success: boolean, error?: string, message?: string, data?: object}>}
 */
async function addSongToQueue(videoId, url) {
  const config = await loadConfig();

  if (!config) {
    return {
      success: false,
      error: 'not_configured',
      message: 'Extension not configured. Open extension options to set API base URL and display name.'
    };
  }

  if (!isValidVideoId(videoId)) {
    return {
      success: false,
      error: 'invalid_video_id',
      message: 'Invalid or missing video ID.'
    };
  }

  // R05: the server now requires Authorization on /api/queue/add. The token
  // is read from chrome.storage.local (key: sessionToken) and sent as a
  // Bearer header when present. Without it the server returns 401 and the
  // song is NOT added. added_by / added_by_id remain in the body for
  // display compatibility only — they are NOT consulted for auth/identity
  // by the R05 route gate.
  const endpoint = `${config.apiBase}/api/queue/add`;
  const body = {
    url: url,
    added_by: config.displayName,
    added_by_id: config.userId
  };

  const headers = {
    'Content-Type': 'application/json'
  };
  if (config.sessionToken) {
    headers['Authorization'] = `Bearer ${config.sessionToken}`;
  }

  try {
    const response = await fetch(endpoint, {
      method: 'POST',
      headers: headers,
      body: JSON.stringify(body)
    });

    if (response.ok) {
      const data = await response.json().catch(() => null);
      return {
        success: true,
        message: 'Song added to queue',
        data: data
      };
    }

    // Handle known error statuses
    if (response.status === 409) {
      const text = await response.text().catch(() => 'Song already in queue');
      return {
        success: false,
        error: 'duplicate',
        message: text || 'Song already in queue'
      };
    }

    if (response.status === 400) {
      const text = await response.text().catch(() => 'Invalid request');
      return {
        success: false,
        error: 'bad_request',
        message: text || 'Invalid request'
      };
    }

    if (response.status === 401) {
      return {
        success: false,
        error: 'unauthorized',
        message: 'Server requires a session token (R05). Open extension options and paste your current lmq_session_token.'
      };
    }

    if (response.status >= 500) {
      const text = await response.text().catch(() => 'Server error');
      return {
        success: false,
        error: 'server',
        message: `Server error: ${text || response.statusText}`
      };
    }

    // Other unexpected status
    const text = await response.text().catch(() => `HTTP ${response.status}`);
    return {
      success: false,
      error: 'http',
      message: text || `HTTP ${response.status}`
    };

  } catch (err) {
    // Network error, CORS issue, or server unreachable
    return {
      success: false,
      error: 'network',
      message: `Network error: ${err.message}. Check your API base URL and server status.`
    };
  }
}

// Listen for messages from content scripts
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.action === 'addSong') {
    // Handle async operation
    addSongToQueue(message.videoId, message.url)
      .then(sendResponse)
      .catch((err) => {
        sendResponse({
          success: false,
          error: 'unknown',
          message: err.message || 'Unknown error'
        });
      });
    // Return true to indicate async response
    return true;
  }

  // Unknown action
  sendResponse({ success: false, error: 'unknown_action', message: 'Unknown action' });
  return false;
});

// Log when service worker starts (helpful for debugging)
console.log('[Local Music Queue] Background service worker loaded');
