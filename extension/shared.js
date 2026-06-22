/**
 * Shared utilities for Local Music Queue browser extension.
 * Pure functions for YouTube URL parsing and video ID validation.
 */

/**
 * Validates a YouTube video ID format.
 * YouTube video IDs are 11 characters using URL-safe Base64 (A-Z, a-z, 0-9, - _).
 * @param {string} id - The video ID to validate.
 * @returns {boolean} True if the ID matches YouTube's format.
 */
function isValidVideoId(id) {
  if (typeof id !== 'string') return false;
  return /^[A-Za-z0-9_-]{11}$/.test(id);
}

/**
 * Extracts a YouTube video ID from various URL formats.
 * Supported formats:
 *   - https://www.youtube.com/watch?v=VIDEO_ID
 *   - https://www.youtube.com/watch?v=VIDEO_ID&t=123
 *   - https://youtu.be/VIDEO_ID
 *   - https://music.youtube.com/watch?v=VIDEO_ID
 *   - https://www.youtube.com/shorts/VIDEO_ID
 *   - https://www.youtube.com/embed/VIDEO_ID
 * @param {string} url - The URL to extract the video ID from.
 * @returns {string|null} The video ID, or null if not found.
 */
function extractVideoId(url) {
  if (typeof url !== 'string' || !url) return null;

  // Standard watch URL: youtube.com/watch?v=ID or music.youtube.com/watch?v=ID
  const watchMatch = url.match(/[?&]v=([A-Za-z0-9_-]{11})(?:[&?]|$)/);
  if (watchMatch) return watchMatch[1];

  // Short URL: youtu.be/ID
  const shortMatch = url.match(/youtu\.be\/([A-Za-z0-9_-]{11})(?:[?&/]|$)/);
  if (shortMatch) return shortMatch[1];

  // Shorts URL: youtube.com/shorts/ID
  const shortsMatch = url.match(/youtube\.com\/shorts\/([A-Za-z0-9_-]{11})(?:[?&/]|$)/);
  if (shortsMatch) return shortsMatch[1];

  // Embed URL: youtube.com/embed/ID
  const embedMatch = url.match(/youtube\.com\/embed\/([A-Za-z0-9_-]{11})(?:[?&/]|$)/);
  if (embedMatch) return embedMatch[1];

  return null;
}

/**
 * Builds a canonical YouTube watch URL from a video ID.
 * @param {string} videoId - The YouTube video ID.
 * @returns {string|null} The canonical watch URL, or null if ID is invalid.
 */
function buildWatchUrl(videoId) {
  if (!isValidVideoId(videoId)) return null;
  return `https://www.youtube.com/watch?v=${videoId}`;
}

// Export for use in both content script (global scope) and potential testing
if (typeof module !== 'undefined' && module.exports) {
  module.exports = { isValidVideoId, extractVideoId, buildWatchUrl };
}
