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
4. Click **Save Settings**

You can also click **Test Connection** to verify that your server is reachable.

## Supported Sites

- **YouTube** (`https://www.youtube.com/*`): Adds "Add to Local Queue" buttons near video menus in:
  - Video lists (search results, recommendations, playlists)
  - The currently playing video's menu
- **YouTube Music** (`https://music.youtube.com/*`): Adds buttons near song menus in:
  - Song lists and search results
  - The player bar (currently playing song)

## Usage

1. Browse YouTube or YouTube Music
2. Look for the blue "Add to Local Queue" button near video/song menus
3. Click the button
4. The button will show feedback:
   - **Adding...** - Request in progress
   - **Added!** (green) - Song successfully added to the queue
   - **Duplicate** (yellow) - Song is already in the queue
   - **Error** (red) - Something went wrong (hover for details)

## Known Limitations

- **YouTube DOM instability**: YouTube and YouTube Music frequently update their DOM structure. The extension uses multiple selector fallbacks, but buttons may not appear if YouTube changes their HTML. File an issue if buttons stop appearing.
- **No playlist support**: The extension adds individual songs only. Playlist-level operations are not supported.
- **No Firefox/Safari support**: This extension is built for Chrome/Chromium using Manifest V3. Firefox and Safari are not supported.
- **HTTPS recommended**: When adding songs from HTTPS YouTube pages to an HTTP local server, the extension's service worker handles the request. However, for production use, configure your Local Music Queue server with HTTPS.
- **Identity is client-supplied**: The extension uses the configured display name and user ID without server-side authentication. This matches the existing Local Music Queue behavior for the add-song endpoint.

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

2. **Background Service Worker** (`background.js`): Receives messages from the content script, loads configuration from `chrome.storage.local`, and makes the API request to the Local Music Queue server. In Manifest V3, service worker `fetch()` bypasses CORS restrictions.

3. **Shared Utilities** (`shared.js`): Pure functions for extracting YouTube video IDs from various URL formats. Used by both the content script and background worker.

4. **Options Page** (`options.html`, `options.js`): Simple configuration form for setting the API base URL, display name, and user ID.

## Manual Verification

To verify the extension works correctly:

1. **Install the extension** following the instructions above
2. **Configure** the API base URL and display name
3. **Test from YouTube**:
   - Navigate to `https://www.youtube.com`
   - Find a video in the recommendations or search results
   - Click "Add to Local Queue"
   - Verify the song appears in your Local Music Queue app
4. **Test from YouTube Music**:
   - Navigate to `https://music.youtube.com`
   - Find a song
   - Click "Add to Local Queue"
   - Verify the song appears in your Local Music Queue app
5. **Test duplicate detection**:
   - Try adding the same song again
   - Verify the button shows "Duplicate" feedback
6. **Test error handling**:
   - Stop your Local Music Queue server
   - Try adding a song
   - Verify the button shows an error with a helpful message
7. **Test missing configuration**:
   - Clear the extension's settings (or install fresh)
   - Try adding a song without configuring
   - Verify the button shows a configuration error

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

### Mixed content warnings

- If your Local Music Queue server uses HTTP while YouTube uses HTTPS, the extension handles this via the service worker. If you see errors, configure your server with HTTPS.

## Development

To regenerate the icons (requires Python 3 with Pillow):

```bash
cd extension
python3 generate_icons.py
```

## License

Same as the parent Local Music Queue project.
