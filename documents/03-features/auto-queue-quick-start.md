# Auto-Queue (Radio Mode) - Quick Start Guide

## What is Radio Mode?

Radio Mode automatically adds related songs to your queue when it drops to 1 song, creating an endless music experience. When enabled, the system fetches related songs from YouTube's radio mix feature, ensuring you never run out of music.

## How to Use

### Enabling Radio Mode

**Via Frontend (Recommended):**
1. Login as Host or Admin
2. Look for the "📻 Radio" button in the top navigation bar
3. Click to toggle on (button will pulse when active)
4. Add songs and let them play - new songs will be added automatically when the queue drops to 1

**Via API:**
```bash
# Enable
curl -X POST http://localhost:1111/api/autoqueue/toggle \
  -H "Content-Type: application/json" \
  -d '{"enabled": true}'

# Check status
curl http://localhost:1111/api/autoqueue/status
```

### Identifying Auto-Added Songs

Songs added by the auto-queue system are marked with:
- **Badge:** `⚡ auto` label in the queue list
- **Added By:** Shows as "system:autoqueue" in the metadata

### Disabling Radio Mode

Simply click the "📻 Radio" button again to turn it off. The queue will stop auto-adding songs.

## How It Works

1. **Trigger:** When your queue has exactly 1 song remaining
2. **Fetch:** System uses yt-dlp to get related songs from YouTube's radio mix
3. **Filter:** Excludes songs already in queue or recently played (last 20 songs)
4. **Add:** Automatically adds 1 related song to the queue
5. **Repeat:** Process continues as long as Radio Mode is enabled

### Fallback Behavior

If yt-dlp fails or all candidates are excluded:
- System falls back to your play history (last 50 songs)
- Picks a random song not currently in the queue
- If no songs available, fails silently (no error shown)

## Requirements

- **Role:** Host or Admin (Guests cannot enable/disable)
- **yt-dlp:** Must be installed on the server
- **Queue:** At least 1 song must be playing for auto-queue to trigger

## Tips

- **Build History:** Play a variety of songs first to build up your play history for better fallback options
- **Mix Manual & Auto:** You can still manually add songs while Radio Mode is active
- **Seamless Experience:** Auto-added songs blend naturally with manually added ones

## Troubleshooting

**Radio Mode not working?**
- Verify you're logged in as Host or Admin
- Check that yt-dlp is installed: `yt-dlp --version`
- Ensure queue has dropped to exactly 1 song
- Check server logs for errors

**Only getting songs from history?**
- yt-dlp might not be installed or accessible
- YouTube radio mix might be unavailable for the current song
- This is normal fallback behavior

**Same songs repeating?**
- Play history is limited to 50 songs
- Add more variety to your queue manually
- System excludes last 20 played songs, but may repeat older ones

## Technical Details

- **Debouncing:** Multiple skip requests won't trigger multiple auto-adds
- **Concurrency Safe:** Uses mutex to prevent race conditions
- **Non-Blocking:** Auto-queue runs in background, doesn't slow down skip/advance
- **History Cap:** Automatically maintains last 50 played songs via database trigger

## API Reference

### Get Status
```
GET /api/autoqueue/status
```
Response:
```json
{
  "enabled": true,
  "strategy": "related"
}
```

### Toggle On/Off
```
POST /api/autoqueue/toggle
```
Request:
```json
{
  "enabled": true
}
```
Response:
```json
{
  "enabled": true,
  "strategy": "related"
}
```

## Database

Auto-queue state is persisted in SQLite:
- **Config:** `.localdb/music_queue.db` → `auto_queue_config` table
- **History:** `.localdb/music_queue.db` → `play_history` table (auto-capped at 50)

To check history:
```bash
sqlite3 .localdb/music_queue.db "SELECT * FROM play_history ORDER BY played_at DESC LIMIT 10;"
```

To reset:
```bash
sqlite3 .localdb/music_queue.db "UPDATE auto_queue_config SET enabled = 0 WHERE id = 1;"
sqlite3 .localdb/music_queue.db "DELETE FROM play_history;"
```

---

**Enjoy endless music with Radio Mode! 🎵**
