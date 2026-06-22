/**
 * Content script for Local Music Queue browser extension.
 * Injects "Add to Local Queue" buttons into YouTube and YouTube Music menus.
 * Uses MutationObserver to handle SPA navigation and dynamic DOM updates.
 */

(function () {
  'use strict';

  // Prevent double injection
  if (window.__lmqContentScriptLoaded) return;
  window.__lmqContentScriptLoaded = true;

  const INJECTED_ATTR = 'data-lmq-injected';
  const BUTTON_CLASS = 'lmq-add-button';
  const DEBOUNCE_MS = 250;

  // Detect which site we're on
  const isYouTubeMusic = window.location.hostname === 'music.youtube.com';

  // ─── SVG Icons ──────────────────────────────────────────────────────────────
  // Each icon is a 20x20 inline SVG. We swap them based on feedback state.

  const ICON_QUEUE_ADD = `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="currentColor">` +
    `<path d="M15 6H3v2h12V6zm0 4H3v2h12v-2zM3 16h8v-2H3v2zM17 6v8.18c-.31-.11-.65-.18-1-.18-1.66 0-3 1.34-3 3s1.34 3 3 3 3-1.34 3-3V8h3V6h-5z"/>` +
    `</svg>`;

  const ICON_CHECK = `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="currentColor">` +
    `<path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41L9 16.17z"/>` +
    `</svg>`;

  const ICON_ERROR = `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="currentColor">` +
    `<path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z"/>` +
    `</svg>`;

  const ICON_DUPLICATE = `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="currentColor">` +
    `<path d="M1 21h22L12 2 1 21zm12-3h-2v-2h2v2zm0-4h-2v-4h2v4z"/>` +
    `</svg>`;

  // ─── Styles ───────────────────────────────────────────────────────────────
  const style = document.createElement('style');
  style.textContent = `
    .${BUTTON_CLASS} {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 28px;
      height: 28px;
      padding: 0;
      border: none;
      border-radius: 50%;
      background: transparent;
      color: #909090;
      cursor: pointer;
      transition: background 0.2s, color 0.2s, opacity 0.2s;
      line-height: 1;
      vertical-align: middle;
      flex-shrink: 0;
    }
    .${BUTTON_CLASS}:hover {
      background: rgba(255, 255, 255, 0.1);
      color: #3ea6ff;
    }
    .${BUTTON_CLASS}:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }
    .${BUTTON_CLASS}.lmq-loading {
      color: #aaa;
    }
    .${BUTTON_CLASS}.lmq-loading svg {
      animation: lmq-spin 0.8s linear infinite;
    }
    @keyframes lmq-spin {
      from { transform: rotate(0deg); }
      to { transform: rotate(360deg); }
    }
    .${BUTTON_CLASS}.lmq-success {
      color: #2ba640;
    }
    .${BUTTON_CLASS}.lmq-error {
      color: #d93025;
    }
    .${BUTTON_CLASS}.lmq-duplicate {
      color: #f9ab00;
    }
    .lmq-menu-wrapper {
      display: inline-flex;
      align-items: center;
      margin-left: 4px;
    }
  `;
  document.head.appendChild(style);

  // ─── Helpers ──────────────────────────────────────────────────────────────

  /**
   * Creates the compact "Add to Local Queue" icon button element.
   * @returns {HTMLButtonElement} The button element.
   */
  function createButton() {
    const btn = document.createElement('button');
    btn.className = BUTTON_CLASS;
    btn.type = 'button';
    btn.innerHTML = ICON_QUEUE_ADD;
    btn.title = 'Add to Local Queue';
    btn.setAttribute('aria-label', 'Add to Local Queue');
    return btn;
  }

  /**
   * Sets button visual state using compact icon feedback.
   * @param {HTMLButtonElement} btn - The button element.
   * @param {'idle'|'loading'|'success'|'error'|'duplicate'} state - The state.
   * @param {string} [message] - Optional tooltip message.
   */
  function setButtonState(btn, state, message) {
    btn.classList.remove('lmq-loading', 'lmq-success', 'lmq-error', 'lmq-duplicate');
    btn.disabled = false;

    switch (state) {
      case 'loading':
        btn.classList.add('lmq-loading');
        btn.innerHTML = ICON_QUEUE_ADD;
        btn.title = 'Adding...';
        btn.setAttribute('aria-label', 'Adding to queue');
        btn.disabled = true;
        break;
      case 'success':
        btn.classList.add('lmq-success');
        btn.innerHTML = ICON_CHECK;
        btn.title = 'Added to Local Music Queue';
        btn.setAttribute('aria-label', 'Added to queue');
        break;
      case 'error':
        btn.classList.add('lmq-error');
        btn.innerHTML = ICON_ERROR;
        btn.title = message || 'Failed to add to Local Music Queue';
        btn.setAttribute('aria-label', 'Error adding to queue');
        break;
      case 'duplicate':
        btn.classList.add('lmq-duplicate');
        btn.innerHTML = ICON_DUPLICATE;
        btn.title = message || 'Song already in queue';
        btn.setAttribute('aria-label', 'Song already in queue');
        break;
      case 'idle':
      default:
        btn.innerHTML = ICON_QUEUE_ADD;
        btn.title = 'Add to Local Queue';
        btn.setAttribute('aria-label', 'Add to Local Queue');
        break;
    }
  }

  // ─── YouTube Regular ─────────────────────────────────────────────────────

  /**
   * Extracts video ID from a YouTube regular renderer element.
   * Checks multiple sources for resilience.
   * @param {Element} renderer - The renderer element.
   * @returns {string|null} The video ID or null.
   */
  function extractVideoIdFromRenderer(renderer) {
    // Try data attributes first
    if (renderer.videoId) return renderer.videoId;
    const attrId = renderer.getAttribute('video-id') || renderer.getAttribute('data-video-id');
    if (attrId && isValidVideoId(attrId)) return attrId;

    // Try finding a link with a video ID
    const links = renderer.querySelectorAll('a[href*="watch?v="], a[href*="/shorts/"]');
    for (const link of links) {
      const href = link.getAttribute('href');
      if (href) {
        const fullUrl = href.startsWith('/') ? `https://www.youtube.com${href}` : href;
        const id = extractVideoId(fullUrl);
        if (id) return id;
      }
    }

    // Try ytd-thumbnail link
    const thumbLink = renderer.querySelector('ytd-thumbnail a, a#thumbnail');
    if (thumbLink) {
      const href = thumbLink.getAttribute('href');
      if (href) {
        const fullUrl = href.startsWith('/') ? `https://www.youtube.com${href}` : href;
        const id = extractVideoId(fullUrl);
        if (id) return id;
      }
    }

    return null;
  }

  /**
   * Processes YouTube regular site menus.
   * Finds menu renderers in video items and injects the add button.
   */
  function processYouTubeMenus() {
    // Target various renderer types that contain menus
    const renderers = document.querySelectorAll(`
      ytd-compact-video-renderer:not([${INJECTED_ATTR}]),
      ytd-rich-item-renderer:not([${INJECTED_ATTR}]),
      ytd-video-renderer:not([${INJECTED_ATTR}]),
      ytd-playlist-video-renderer:not([${INJECTED_ATTR}]),
      ytd-grid-video-renderer:not([${INJECTED_ATTR}])
    `);

    renderers.forEach(renderer => {
      const menu = renderer.querySelector('ytd-menu-renderer');
      if (!menu) return;

      // Check if we already injected into this specific menu
      if (menu.hasAttribute(INJECTED_ATTR)) {
        renderer.setAttribute(INJECTED_ATTR, 'true');
        return;
      }

      const videoId = extractVideoIdFromRenderer(renderer);
      if (!videoId) return;

      const wrapper = document.createElement('div');
      wrapper.className = 'lmq-menu-wrapper';
      wrapper.setAttribute(INJECTED_ATTR, 'true');

      const btn = createButton();
      btn.addEventListener('click', (e) => {
        e.preventDefault();
        e.stopPropagation();
        handleAddClick(btn, videoId);
      });

      wrapper.appendChild(btn);
      menu.parentElement.insertBefore(wrapper, menu.nextSibling);

      renderer.setAttribute(INJECTED_ATTR, 'true');
      menu.setAttribute(INJECTED_ATTR, 'true');
    });

    // Also handle the watch page top-level menu (video being watched)
    processWatchPageMenu();
  }

  /**
   * Handles the watch page's primary video menu (the video currently playing).
   */
  function processWatchPageMenu() {
    const watchMenu = document.querySelector('ytd-watch-metadata ytd-menu-renderer');
    if (!watchMenu || watchMenu.hasAttribute(INJECTED_ATTR)) return;

    // Extract video ID from the page URL
    const videoId = extractVideoId(window.location.href);
    if (!videoId) return;

    const wrapper = document.createElement('div');
    wrapper.className = 'lmq-menu-wrapper';
    wrapper.setAttribute(INJECTED_ATTR, 'true');

    const btn = createButton();
    btn.addEventListener('click', (e) => {
      e.preventDefault();
      e.stopPropagation();
      handleAddClick(btn, videoId);
    });

    wrapper.appendChild(btn);
    watchMenu.parentElement.insertBefore(wrapper, watchMenu.nextSibling);
    watchMenu.setAttribute(INJECTED_ATTR, 'true');
  }

  // ─── YouTube Music ────────────────────────────────────────────────────────

  /**
   * Extracts video ID from a YouTube Music renderer element.
   * @param {Element} renderer - The renderer element.
   * @returns {string|null} The video ID or null.
   */
  function extractVideoIdFromMusicRenderer(renderer) {
    // Try data properties
    if (renderer.videoId) return renderer.videoId;
    const attrId = renderer.getAttribute('video-id') || renderer.getAttribute('data-video-id');
    if (attrId && isValidVideoId(attrId)) return attrId;

    // Try finding links
    const links = renderer.querySelectorAll('a[href*="watch?v="]');
    for (const link of links) {
      const href = link.getAttribute('href');
      if (href) {
        const fullUrl = href.startsWith('/') ? `https://music.youtube.com${href}` : href;
        const id = extractVideoId(fullUrl);
        if (id) return id;
      }
    }

    // Try the thumbnail overlay link
    const thumbLink = renderer.querySelector('ytmusic-thumbnail-renderer a, a.ytmusic-thumbnail');
    if (thumbLink) {
      const href = thumbLink.getAttribute('href');
      if (href) {
        const fullUrl = href.startsWith('/') ? `https://music.youtube.com${href}` : href;
        const id = extractVideoId(fullUrl);
        if (id) return id;
      }
    }

    return null;
  }

  /**
   * Processes YouTube Music menus.
   */
  function processYouTubeMusicMenus() {
    // Target list item renderers and song rows
    const renderers = document.querySelectorAll(`
      ytmusic-responsive-list-item-renderer:not([${INJECTED_ATTR}]),
      ytmusic-list-item-renderer:not([${INJECTED_ATTR}]),
      ytmusic-grid-renderer ytmusic-two-column-item-renderer:not([${INJECTED_ATTR}])
    `);

    renderers.forEach(renderer => {
      const menu = renderer.querySelector('ytmusic-menu-renderer');
      if (!menu) return;

      if (menu.hasAttribute(INJECTED_ATTR)) {
        renderer.setAttribute(INJECTED_ATTR, 'true');
        return;
      }

      const videoId = extractVideoIdFromMusicRenderer(renderer);
      if (!videoId) return;

      const wrapper = document.createElement('div');
      wrapper.className = 'lmq-menu-wrapper';
      wrapper.setAttribute(INJECTED_ATTR, 'true');

      const btn = createButton();
      btn.addEventListener('click', (e) => {
        e.preventDefault();
        e.stopPropagation();
        handleAddClick(btn, videoId);
      });

      wrapper.appendChild(btn);
      menu.parentElement.insertBefore(wrapper, menu.nextSibling);

      renderer.setAttribute(INJECTED_ATTR, 'true');
      menu.setAttribute(INJECTED_ATTR, 'true');
    });

    // Handle YTM player bar (currently playing song)
    processYouTubeMusicPlayerBar();
  }

  /**
   * Handles YouTube Music player bar (bottom bar with currently playing song).
   */
  function processYouTubeMusicPlayerBar() {
    const playerBar = document.querySelector('ytmusic-player-bar');
    if (!playerBar || playerBar.hasAttribute(INJECTED_ATTR)) return;

    const menu = playerBar.querySelector('ytmusic-menu-renderer');
    if (!menu || menu.hasAttribute(INJECTED_ATTR)) return;

    // Try to get video ID from player bar data
    let videoId = null;

    // Check the player bar's song link
    const songLink = playerBar.querySelector('a.ytmusic-player-bar[href*="watch?v="]');
    if (songLink) {
      const href = songLink.getAttribute('href');
      if (href) {
        videoId = extractVideoId(href.startsWith('/') ? `https://music.youtube.com${href}` : href);
      }
    }

    // Fallback: try the page URL if we're on a watch page
    if (!videoId) {
      videoId = extractVideoId(window.location.href);
    }

    if (!videoId) return;

    const wrapper = document.createElement('div');
    wrapper.className = 'lmq-menu-wrapper';
    wrapper.setAttribute(INJECTED_ATTR, 'true');

    const btn = createButton();
    btn.addEventListener('click', (e) => {
      e.preventDefault();
      e.stopPropagation();
      handleAddClick(btn, videoId);
    });

    wrapper.appendChild(btn);
    menu.parentElement.insertBefore(wrapper, menu.nextSibling);

    playerBar.setAttribute(INJECTED_ATTR, 'true');
    menu.setAttribute(INJECTED_ATTR, 'true');
  }

  // ─── API Communication ────────────────────────────────────────────────────

  /**
   * Handles the add button click. Sends message to background script.
   * @param {HTMLButtonElement} btn - The clicked button.
   * @param {string} videoId - The YouTube video ID.
   */
  async function handleAddClick(btn, videoId) {
    setButtonState(btn, 'loading');

    try {
      const response = await chrome.runtime.sendMessage({
        action: 'addSong',
        videoId: videoId,
        url: buildWatchUrl(videoId)
      });

      if (!response) {
        setButtonState(btn, 'error', 'No response from extension');
        resetButtonAfterDelay(btn);
        return;
      }

      if (response.success) {
        setButtonState(btn, 'success');
        resetButtonAfterDelay(btn);
      } else {
        switch (response.error) {
          case 'not_configured':
            setButtonState(btn, 'error', 'Extension not configured. Right-click extension icon > Options.');
            break;
          case 'duplicate':
            setButtonState(btn, 'duplicate', response.message || 'Song already in queue');
            break;
          case 'network':
            setButtonState(btn, 'error', response.message || 'Network error. Check API base URL.');
            break;
          default:
            setButtonState(btn, 'error', response.message || 'Failed to add song');
        }
        resetButtonAfterDelay(btn);
      }
    } catch (err) {
      setButtonState(btn, 'error', err.message || 'Extension communication error');
      resetButtonAfterDelay(btn);
    }
  }

  /**
   * Resets button to idle state after a delay.
   * @param {HTMLButtonElement} btn - The button to reset.
   */
  function resetButtonAfterDelay(btn) {
    setTimeout(() => {
      if (btn.isConnected) {
        setButtonState(btn, 'idle');
      }
    }, 3000);
  }

  // ─── MutationObserver ─────────────────────────────────────────────────────

  let debounceTimer = null;

  /**
   * Debounced scan for new menus.
   */
  function debouncedScan() {
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      if (isYouTubeMusic) {
        processYouTubeMusicMenus();
      } else {
        processYouTubeMenus();
      }
    }, DEBOUNCE_MS);
  }

  // Observe DOM changes for dynamic content
  const observer = new MutationObserver((mutations) => {
    // Check if any mutation added relevant elements
    let shouldScan = false;
    for (const mutation of mutations) {
      if (mutation.type === 'childList' && mutation.addedNodes.length > 0) {
        for (const node of mutation.addedNodes) {
          if (node.nodeType === Node.ELEMENT_NODE) {
            // Check if the added node or its descendants contain menu renderers
            if (
              node.tagName?.startsWith('YTD-') ||
              node.tagName?.startsWith('YTMUSIC-') ||
              node.querySelector?.('ytd-menu-renderer, ytmusic-menu-renderer')
            ) {
              shouldScan = true;
              break;
            }
          }
        }
      }
      if (shouldScan) break;
    }

    if (shouldScan) {
      debouncedScan();
    }
  });

  // Start observing after initial page load
  function init() {
    // Initial scan
    debouncedScan();

    // Start observing for dynamic changes
    observer.observe(document.body, {
      childList: true,
      subtree: true
    });

    // Also re-scan on navigation events (YouTube SPA uses popstate)
    window.addEventListener('yt-navigate-finish', debouncedScan);
    window.addEventListener('yt-page-data-updated', debouncedScan);

    // YouTube Music navigation events
    window.addEventListener('yt-navigate-finish', debouncedScan);
  }

  // Wait for DOM to be ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
