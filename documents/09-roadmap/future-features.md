# Future Features

This document outlines potential features and enhancements for Local Music Queue that are not yet implemented.

## High Priority

### 1. Full Sync Request on Sequence Gap Detection
**Status**: ❌ Not Implemented  
**Current Behavior**: Only logs warning when sequence gap detected  
**Proposed Behavior**: Automatically request full state sync from server

**Implementation**:
```javascript
// frontend/src/services/websocket.js
if (data.sequence !== expectedSequence + 1) {
  console.warn('Sequence gap detected, requesting full sync')
  websocket.send(JSON.stringify({ type: 'request_full_sync' }))
}
```

**Benefits**:
- Prevents state desynchronization
- Improves reliability on unstable connections
- Automatic recovery from missed messages

**Effort**: Low (1-2 days)

---

### 2. Volume Slider with Visual Feedback
**Status**: ❌ Not Implemented  
**Current Behavior**: ±10% volume buttons only

**Proposed Features**:
- Draggable volume slider (0-100%)
- Visual volume level indicator
- Mute/unmute toggle
- Volume percentage display
- Smooth volume transitions

**UI Mockup**:
```
┌─────────────────────────┐
│  Volume: 75%            │
│  ├────────●────┤  🔊    │
│  [Mute]                 │
└─────────────────────────┘
```

**Benefits**:
- More precise volume control
- Better user experience
- Visual feedback

**Effort**: Medium (3-5 days)

---

### 3. Mute/Unmute Toggle
**Status**: ❌ Not Implemented  
**Current Behavior**: No mute functionality

**Proposed Features**:
- Single-click mute/unmute
- Remember volume level before mute
- Visual indicator (🔊/🔇)
- Keyboard shortcut (M key)

**Benefits**:
- Quick silence without losing volume setting
- Common user expectation
- Accessibility improvement

**Effort**: Low (1 day)

---

## Medium Priority

### 4. Dark/Light Theme Toggle
**Status**: ❌ Not Implemented  
**Current Behavior**: Dark theme only

**Proposed Features**:
- Light theme variant
- Theme toggle button in UI
- System preference detection
- Theme persistence in localStorage
- Smooth theme transitions

**Implementation**:
```javascript
// Detect system preference
const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches

// Toggle theme
function toggleTheme() {
  document.body.classList.toggle('light-theme')
  localStorage.setItem('theme', currentTheme)
}
```

**Benefits**:
- Accessibility (some users prefer light themes)
- User preference support
- Modern UX expectation

**Effort**: Medium (3-5 days)

---

### 5. Playlist Save/Load Functionality
**Status**: ❌ Not Implemented

**Proposed Features**:
- Save current queue as named playlist
- Load saved playlists
- Playlist management (rename, delete)
- Share playlists via export/import
- Playlist history

**Database Schema**:
```sql
CREATE TABLE playlists (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    songs_json TEXT NOT NULL,
    FOREIGN KEY(created_by) REFERENCES users(id)
);
```

**API Endpoints**:
- `POST /api/playlists` - Create playlist
- `GET /api/playlists` - List playlists
- `GET /api/playlists/:id` - Get playlist
- `PUT /api/playlists/:id` - Update playlist
- `DELETE /api/playlists/:id` - Delete playlist
- `POST /api/playlists/:id/load` - Load playlist to queue

**Benefits**:
- Reuse favorite song collections
- Event planning (pre-build playlists)
- Collaboration (share playlists)

**Effort**: High (1-2 weeks)

---

### 6. User Avatars (Beyond Google Profile Pictures)
**Status**: ⚠️ Partially Implemented (Google profile pictures only)

**Proposed Features**:
- Custom avatar upload
- Avatar selection from preset library
- Gravatar integration
- Avatar display in activity log
- Avatar display in queue (song added by)

**Benefits**:
- Better user identification
- Personalization
- Visual appeal

**Effort**: Medium (3-5 days)

---

### 7. Enhanced Search Filters
**Status**: ❌ Not Implemented  
**Current Behavior**: Basic YouTube search only

**Proposed Features**:
- Filter by duration (short/medium/long)
- Filter by upload date (today/week/month/year)
- Filter by view count
- Sort by relevance/date/views
- Search history

**UI Mockup**:
```
┌─────────────────────────────────────┐
│ Search: [____________] [🔍]         │
│                                     │
│ Filters:                            │
│ Duration: [Any ▼] [Short/Med/Long] │
│ Date: [Any ▼] [Today/Week/Month]   │
│ Sort: [Relevance ▼]                 │
└─────────────────────────────────────┘
```

**Benefits**:
- Find specific content faster
- Better search experience
- More control over results

**Effort**: Medium (5-7 days)

---

## Low Priority

### 8. Offline Mode with Service Worker
**Status**: ❌ Not Implemented

**Proposed Features**:
- Service worker for offline caching
- Offline queue viewing
- Sync when connection restored
- Offline indicator in UI
- Background sync API

**Benefits**:
- Works during network interruptions
- Progressive Web App (PWA) capability
- Better mobile experience

**Effort**: High (1-2 weeks)

---

### 9. Mobile App (React Native / Flutter)
**Status**: ❌ Not Implemented

**Proposed Features**:
- Native iOS and Android apps
- Push notifications for queue updates
- Native media controls
- Background playback
- App Store / Play Store distribution

**Benefits**:
- Better mobile experience
- Native device integration
- Offline capabilities
- Push notifications

**Effort**: Very High (2-3 months)

---

### 10. Advanced Priority Features
**Status**: ❌ Not Implemented  
**Current Behavior**: Fixed 1 token per day, 1 token per prioritization

**Proposed Features**:
- **Token Purchase**: Buy tokens with real money
- **Token Gifting**: Transfer tokens to other users
- **Token Expiration**: Tokens expire after 30 days
- **Variable Costs**: Peak hours cost more tokens
- **Bonus Rewards**: Earn tokens for contributions
- **Leaderboard**: Show top token earners
- **Admin Grants**: Admins can grant bonus tokens

**Monetization Potential**:
```
Token Packages:
- 5 tokens: $0.99
- 10 tokens: $1.49
- 25 tokens: $2.99
```

**Benefits**:
- Revenue generation
- Increased engagement
- Gamification

**Effort**: Very High (3-4 weeks)

---

### 11. Song Voting System
**Status**: ❌ Not Implemented

**Proposed Features**:
- Upvote/downvote songs in queue
- Auto-reorder by vote count
- Vote weight based on user role
- Vote history tracking
- Prevent vote manipulation

**UI Mockup**:
```
┌─────────────────────────────────┐
│ 1. Song Title          ↑ 5 ↓ 1 │
│ 2. Another Song        ↑ 3 ↓ 0 │
│ 3. Third Song          ↑ 1 ↓ 2 │
└─────────────────────────────────┘
```

**Benefits**:
- Democratic queue management
- Community engagement
- Reduces host workload

**Effort**: High (1-2 weeks)

---

### 12. Song History and Statistics
**Status**: ❌ Not Implemented

**Proposed Features**:
- Track all played songs
- Most played songs (all-time, weekly)
- User statistics (songs added, tokens used)
- Popular artists/genres
- Play count per song
- Export statistics as CSV

**Database Schema**:
```sql
CREATE TABLE song_history (
    id INTEGER PRIMARY KEY,
    song_id TEXT NOT NULL,
    title TEXT NOT NULL,
    artist TEXT,
    played_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    added_by TEXT NOT NULL,
    FOREIGN KEY(added_by) REFERENCES users(id)
);
```

**Benefits**:
- Discover popular songs
- User engagement metrics
- Data-driven insights

**Effort**: Medium (5-7 days)

---

### 13. Queue Shuffle and Repeat
**Status**: ❌ Not Implemented

**Proposed Features**:
- Shuffle queue order
- Repeat modes:
  - Repeat all (loop queue)
  - Repeat one (loop current song)
  - No repeat (default)
- Visual indicators for active modes

**Benefits**:
- More playback options
- Common music player feature
- User expectation

**Effort**: Low (2-3 days)

---

### 14. Collaborative Playlists
**Status**: ❌ Not Implemented

**Proposed Features**:
- Multiple users can edit same playlist
- Real-time collaborative editing
- Playlist permissions (owner, editor, viewer)
- Playlist comments/chat
- Version history

**Benefits**:
- Team playlist building
- Event planning
- Social features

**Effort**: Very High (3-4 weeks)

---

### 15. Integration with Music Services
**Status**: ❌ Not Implemented

**Proposed Features**:
- Spotify integration (search, import playlists)
- Apple Music integration
- SoundCloud support
- Bandcamp support
- Multi-source queue (YouTube + Spotify + etc.)

**Challenges**:
- API rate limits
- Authentication complexity
- Licensing considerations
- Playback restrictions

**Benefits**:
- Broader music catalog
- User convenience
- Platform flexibility

**Effort**: Very High (2-3 months per platform)

---

## Technical Improvements

### 16. Horizontal Scaling Support
**Status**: ❌ Not Implemented  
**Current Limitation**: Single server instance only

**Proposed Features**:
- Replace SQLite with PostgreSQL
- Redis for WebSocket pub/sub
- Load balancer support
- Session affinity
- Distributed queue state

**Benefits**:
- Support >20 concurrent users
- High availability
- Better performance

**Effort**: Very High (1-2 months)

---

### 17. Metrics and Monitoring
**Status**: ❌ Not Implemented

**Proposed Features**:
- Prometheus metrics export
- Grafana dashboards
- Error tracking (Sentry)
- Performance monitoring
- User analytics

**Metrics to Track**:
- Active users
- Songs added per hour
- WebSocket connections
- API response times
- Error rates

**Benefits**:
- Production monitoring
- Performance insights
- Proactive issue detection

**Effort**: Medium (1 week)

---

### 18. API Rate Limiting
**Status**: ❌ Not Implemented

**Proposed Features**:
- Per-user rate limits
- Per-IP rate limits
- Configurable limits
- Rate limit headers
- Graceful degradation

**Implementation**:
```go
// 10 requests per second per user
limiter := rate.NewLimiter(10, 20)
```

**Benefits**:
- Prevent abuse
- Fair resource allocation
- DDoS protection

**Effort**: Low (2-3 days)

---

## Community Features

### 19. User Profiles
**Status**: ❌ Not Implemented

**Proposed Features**:
- Public user profiles
- Bio and social links
- Favorite songs/artists
- Activity history
- Follow other users

**Benefits**:
- Social features
- User engagement
- Community building

**Effort**: High (2-3 weeks)

---

### 20. Chat/Comments
**Status**: ❌ Not Implemented

**Proposed Features**:
- Real-time chat alongside queue
- Song comments
- Emoji reactions
- Mention users (@username)
- Chat moderation

**Benefits**:
- Social interaction
- Community engagement
- Feedback mechanism

**Effort**: High (2-3 weeks)

---

## Prioritization Matrix

| Feature | Priority | Effort | Impact | Status |
|---------|----------|--------|--------|--------|
| Full sync on gap | High | Low | High | ❌ |
| Volume slider | High | Medium | Medium | ❌ |
| Mute toggle | High | Low | Medium | ❌ |
| Theme toggle | Medium | Medium | Medium | ❌ |
| Playlist save/load | Medium | High | High | ❌ |
| User avatars | Medium | Medium | Low | ⚠️ |
| Search filters | Medium | Medium | Medium | ❌ |
| Offline mode | Low | High | Low | ❌ |
| Mobile app | Low | Very High | High | ❌ |
| Advanced priority | Low | Very High | Medium | ❌ |
| Song voting | Low | High | Medium | ❌ |
| Statistics | Low | Medium | Low | ❌ |
| Shuffle/repeat | Low | Low | Low | ❌ |
| Collaborative playlists | Low | Very High | Medium | ❌ |
| Music service integration | Low | Very High | High | ❌ |
| Horizontal scaling | Tech | Very High | High | ❌ |
| Monitoring | Tech | Medium | High | ❌ |
| Rate limiting | Tech | Low | Medium | ❌ |
| User profiles | Community | High | Medium | ❌ |
| Chat/comments | Community | High | Medium | ❌ |

---

## Contribution Guidelines

Want to implement one of these features? See [Contributing Guide](../08-development/contributing.md) for development guidelines.

**Before starting:**
1. Check if feature is already in progress
2. Discuss approach in GitHub issues
3. Follow existing code patterns
4. Write tests for new features
5. Update documentation

---

## Feedback

Have ideas for new features? Open an issue on GitHub or contact the maintainers.
