# Local Music Queue - Browser Extension

A Chrome/Chromium Manifest V3 browser extension that adds an "Add to Local Queue" button to YouTube and YouTube Music pages, letting you quickly add songs to your Local Music Queue without leaving the source site.

## Installation

1. Open Chrome/Chromium and navigate to `chrome://extensions/`
2. Enable **Developer mode** (toggle in the top right corner)
3. Click **Load unpacked**
4. Select the `extension/` directory from this repository

The extension will appear in your extensions list with a music note icon.

## Configuration

Before using the extension, you must configure it:

1. Right-click the extension icon in the Chrome toolbar
2. Select **Options** (or click the extension icon and select "Options")
3. Fill in the required fields:
   - **API Base URL**: The full URL to your Local Music Queue server (e.g., `https://your-server:443` or `http://192.168.1.100:8080`)
   - **Display Name**: The name that will appear as "added by" when songs are added from this extension
   - **User ID** (optional): Your numeric user ID in the Local Music Queue app (defaults to 0)
   - **Session Token** (R05, required to add songs): Paste the value of `lmq_session_token` from your browser's `localStorage` on the Local Music Queue app (see below). Without it, the server returns 401 to `/api/queue/add` and songs cannot be added.
4. Click **Save Settings**

You can also click **Test Connection** to verify that your server is reachable.

### Where to find the Session Token

The Session Token is the same opaque token the web app sends in the `Authorization: Bearer <token>` header (and in `?session_token=` on the WebSocket URL). It is stored under `lmq_session_token` in `localStorage` for the Local Music Queue app's origin.

To copy it:

1. Open the Local Music Queue web app and log in (Google OAuth). Stay logged in.
2. Open DevTools → **Application** → **Local Storage** → select the app's origin.
3. Find the row `lmq_session_token` and copy its value.
4. Open the extension Options page, paste it into **Session Token**, and Save.

The token rotates on every login. Re-paste after each new login. The extension stores the value in `chrome.storage.local` (key `sessionToken`) and never logs it.

## Supported Sites

- **YouTube** (`https://www.youtube.com/*`): Adds a compact queue icon button near video menus in:
  - Video lists (search results, recommendations, playlists)
  - The currently playing video's menu
- **YouTube Music** (`https://music.youtube.com/*`): Adds a compact queue icon button near song menus in:
  - Song lists and search results
  - The player bar (currently playing song)

## Usage

1. Browse YouTube or YouTube Music
2. Look for the small queue icon button (♫ with +) next to video/song menus — it appears as a subtle grey icon
3. Click the icon button
4. The icon provides visual feedback:
   - **Spinning icon** (grey) - Request in progress
   - **Checkmark** (green) - Song successfully added to the queue
   - **Warning triangle** (yellow) - Song is already in the queue
   - **Error circle** (red) - Something went wrong (hover for details)

## Known Limitations

- **YouTube DOM instability**: YouTube and YouTube Music frequently update their DOM structure. The extension uses multiple selector fallbacks, but buttons may not appear if YouTube changes their HTML. File an issue if buttons stop appearing.
- **No playlist support**: The extension adds individual songs only. Playlist-level operations are not supported.
- **No Firefox/Safari support**: This extension is built for Chrome/Chromium using Manifest V3. Firefox and Safari are not supported.
- **HTTPS with valid certificate required**: The extension's service worker `fetch()` requires the Local Music Queue server to have a valid TLS certificate if using HTTPS. Self-signed certificates will cause the request to fail. For local/development use, HTTP is acceptable. The server's `Access-Control-Allow-Origin: *` CORS header is required unless the user grants the extension optional host permission for the API host during configuration.
- **Identity is client-supplied for display only**: The extension sends `added_by`/`added_by_id` in the JSON body for display/audit, but the server no longer uses them for authorization. Auth/identity comes from the **Session Token** (R05).
- **Clearing a saved Session Token (future UX improvement)**: The Options page currently has no dedicated "Clear Session Token" button. Today, clearing the saved token requires either reinstalling the extension, clearing the `sessionToken` entry from `chrome.storage.local` via DevTools (Extensions → service worker → Storage → `chrome.storage.local`), or saving an empty/blank value into the Session Token field and clicking Save Settings. A explicit in-UI clear-token control is a planned future UX improvement.

## Architecture

```
extension/
├── manifest.json      # Manifest V3 configuration
├── background.js      # Service worker: handles API requests
├── content.js         # Content script: injects buttons into YouTube pages
├── shared.js          # Shared utilities: URL parsing, video ID extraction
├── options.html       # Options page: configuration UI
├── options.js         # Options page logic
├── icons/             # Extension icons
│   ├── icon16.png
│   ├── icon48.png
│   └── icon128.png
└── README.md          # This file
```

### How It Works

1. **Content Script** (`content.js`): Runs on YouTube and YouTube Music pages. Uses `MutationObserver` to detect dynamically loaded menus and inject "Add to Local Queue" buttons. Handles button clicks and displays feedback.

2. **Background Service Worker** (`background.js`): Receives messages from the content script, loads configuration from `chrome.storage.local`, and makes the API request to the Local Music Queue server. The service worker `fetch()` relies on the server's `Access-Control-Allow-Origin: *` CORS header, or on an optional host permission granted by the user during configuration.

3. **Shared Utilities** (`shared.js`): Pure functions for extracting YouTube video IDs from various URL formats. Used by both the content script and background worker.

4. **Options Page** (`options.html`, `options.js`): Simple configuration form for setting the API base URL, display name, and user ID.

## Manual Verification

To verify the extension works correctly:

1. **Install the extension** following the instructions above
2. **Configure** the API base URL and display name
3. **Test from YouTube**:
   - Navigate to `https://www.youtube.com`
   - Find a video in the recommendations or search results
   - Click the queue icon button next to the video menu
   - Verify the icon turns green (checkmark) and the song appears in your Local Music Queue app
4. **Test from YouTube Music**:
   - Navigate to `https://music.youtube.com`
   - Find a song
   - Click the queue icon button next to the song menu
   - Verify the icon turns green (checkmark) and the song appears in your Local Music Queue app
5. **Test duplicate detection**:
   - Try adding the same song again
   - Verify the icon turns yellow (warning triangle) indicating duplicate
6. **Test error handling**:
   - Stop your Local Music Queue server
   - Try adding a song
   - Verify the icon turns red (error circle); hover for tooltip details
7. **Test missing configuration**:
   - Clear the extension's settings (or install fresh)
   - Try adding a song without configuring
   - Verify the icon turns red; hover for configuration error tooltip

## Troubleshooting

### Buttons not appearing

- YouTube may have updated their DOM structure. Check for extension updates or file an issue.
- Try refreshing the YouTube page.
- Check the browser console for errors.

### Connection errors

- Verify your API base URL is correct (including protocol and port).
- Ensure your Local Music Queue server is running and reachable.
- Check firewall settings if accessing a remote server.
- Use the "Test Connection" button on the options page.

### Mixed content or connection errors

- The extension's service worker makes the API call directly (not from the YouTube page context), so mixed content (HTTP API from HTTPS YouTube) is not blocked by the browser's mixed-content policy.
- However, if using HTTPS, the server must have a **valid** TLS certificate. Self-signed or expired certificates will cause the request to fail.
- When saving your configuration, the extension may prompt you to grant host permission for your API base URL. If denied, the request relies on the server's CORS header (`Access-Control-Allow-Origin: *`).

## Development

To regenerate the icons (requires Python 3 with Pillow):

```bash
cd extension
python3 generate_icons.py
```

## Distribution

### Load Unpacked (Development)

For local development and testing:

1. Navigate to `chrome://extensions/`
2. Enable **Developer mode**
3. Click **Load unpacked**
4. Select the `extension/` directory

Changes to the extension files require clicking the reload icon on the extension card or reloading the extension page.

### GitHub Release ZIP (Internal Users)

For distributing to technical team members via a GitHub release:

1. Create a ZIP of the `extension/` directory contents (not the parent directory):
   ```bash
   cd extension
   zip -r ../lmq-extension-v1.0.0.zip . -x "generate_icons.py" "README.md"
   ```
2. Attach the ZIP to a GitHub release.
3. Recipients download the ZIP, extract it, and load unpacked from the extracted directory.

**Note**: Recipients still need Developer mode enabled and must load unpacked manually.

### Unlisted Chrome Web Store (Recommended for Non-Technical Users)

For the easiest installation experience for non-technical users:

1. Package the extension into a ZIP (same as above).
2. Go to the [Chrome Web Store Developer Dashboard](https://chrome.google.com/webstore/devconsole).
3. Pay the one-time $5 developer registration fee (if not already paid).
4. Upload the ZIP as a new item.
5. Set the visibility to **Unlisted** (anyone with the link can install, not publicly searchable).
6. Submit for review (typically takes 1-3 business days).

**Benefits of unlisted distribution:**
- Users install via a simple link, no Developer mode required.
- Automatic updates when you publish new versions.
- No public visibility in the Chrome Web Store.

**Considerations:**
- Each version update requires re-packaging and re-submitting for review.
- Chrome Web Store policies apply (even for unlisted items).
- The extension must comply with Chrome Web Store developer program policies.

## Authentication Status (R05)

R05 server-authenticates `POST /api/queue/add` via the same bearer token the frontend uses on its REST + WebSocket layer. The extension now attaches `Authorization: Bearer <session_token>` whenever a token is configured in Options.

- **How the token reaches the extension:** the user pastes the current `lmq_session_token` (read from the web app's `localStorage`) into the Options page. The extension stores it in `chrome.storage.local` under `sessionToken` and sends it on every `POST /api/queue/add`. The token is never logged.
- **What happens with no token:** the server returns 401 to `/api/queue/add` and the extension surfaces a clear "session token required" error to the user.
- **Token rotation:** the token rotates on each frontend login (per the existing auth flow). The extension has no automatic refresh path; the user must re-paste the new value into Options after each login. This is the documented v1 limitation — a future sprint can auto-sync from the web app's `localStorage` via the content script on the app's origin.
- **Display identity vs auth identity:** `added_by` / `added_by_id` remain in the JSON body for backwards-compatible display, but are not consulted by the R05 authorization gate.

### Manual verification (extension + R05)

There is no automated harness for the extension. After saving the session token in Options:

1. **Unconfigured** — clear Options, click the button on a YouTube video → expect a "not configured" tooltip.
2. **Configured, no token** — fill API Base URL + Display Name only, leave Session Token blank, click the button → expect a red error and tooltip mentioning session token / 401.
3. **Configured, valid token** — paste a valid `lmq_session_token` from a logged-in frontend session, click the button → expect the green checkmark and the song appears at the bottom of the queue.
4. **Expired token** — paste an old/expired token, click the button → expect 401 and the same "unauthorized" tooltip.
5. **Network/server down** — point API Base at an unreachable host → expect the existing "network" error path.
6. **Round-trip server log** — confirm the request lands on the server with `Authorization: Bearer ...` and a 204/200 response (not 401).

## License

Same as the parent Local Music Queue project.
