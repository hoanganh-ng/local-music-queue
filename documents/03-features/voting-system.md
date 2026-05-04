# Community Voting System

## Overview

The community voting system enables Guests and Admins to democratically control playback through two types of votes:

- **Skip Vote**: Vote to skip the currently playing song
- **Priority Vote**: Vote to move a queued song to the front

Votes require a **strict majority** of connected users to pass and automatically expire after **30 seconds**. The Host is excluded from voting since they already have direct playback controls.

## How It Works

### Vote Requirements

- **Eligible Voters**: Guest and Admin roles only (Host excluded)
- **Threshold**: Strict majority = `(connectedUsers / 2) + 1` votes (minimum 2)
- **Time Limit**: 30 seconds per vote session
- **Persistence**: In-memory only (sessions cleared on server restart)

### Vote Types

#### Skip Vote

Allows users to collectively skip the currently playing song.

**Behavior:**
- Creates a vote session for the current song
- When threshold is reached, advances to the next song
- If the current song is the last in queue, sets status to `idle`
- Logs activity: `"vote skipped \"<song title>\""`

**Example:**
- 5 users connected → requires 3 votes to pass
- User A votes → "Skip? 1/3 (30s)"
- User B votes → "Skip? 2/3 (28s)"
- User C votes → Vote passes, song skipped

#### Priority Vote

Allows users to collectively move a queued song to the front.

**Behavior:**
- Creates a vote session for the target song
- When threshold is reached, moves song to position `currentIndex + 1`
- Uses the same prioritization logic as the token system
- Cannot vote on the currently playing song
- Logs activity: `"vote prioritized \"<song title>\""`

**Example:**
- Song at position 5 receives majority votes
- Song moves to position 1 (right after current song)
- Respects existing prioritized songs

### Vote Session Lifecycle

1. **Creation**: First vote creates a session with:
   - Unique ID: `"<type>:<songID>"`
   - Threshold calculated from current connected users
   - 30-second expiration timer

2. **Active**: Additional votes increment the count
   - Real-time updates broadcast to all clients
   - Users who already voted cannot vote again
   - Countdown timer displayed in UI

3. **Resolution**: Session ends when:
   - **Passed**: Threshold reached → action executed, session deleted
   - **Expired**: 30 seconds elapsed → session deleted, no action
   - **Removed**: Song removed from queue → session deleted

### Threshold Calculation

The threshold is computed **once** when the session is created and remains fixed even if users join or leave during the vote. This prevents a "moving goalpost" problem.

**Formula:**
```
threshold = (connectedUsers / 2) + 1
minimum = 2
```

**Examples:**
- 1 user → 2 votes required (prevents self-passing)
- 2 users → 2 votes required
- 3 users → 2 votes required
- 4 users → 3 votes required
- 5 users → 3 votes required
- 10 users → 6 votes required

## User Interface

### Vote Button States

The `VoteButton` component displays different states:

**Inactive (no active vote):**
```
[ Vote to Skip ]
[ Vote to Prioritize ]
```

**Active (vote in progress):**
```
[ Skip? 2/3 (28s) ]
[ Bump? 1/2 (15s) ]
```

**Voted (user already voted):**
```
[ Skip? 2/3 (28s) ] ← disabled, green background
```

### Button Placement

- **Skip Button**: Appears in the "Now Playing" section below the song title
- **Priority Buttons**: Appear next to each song in the "Up Next" queue list
- **Visibility**: Only shown to Guest and Admin users (hidden for Host)

### Real-time Updates

All connected clients see:
- Live vote counts as users vote
- Countdown timer ticking down
- Immediate feedback when vote passes or expires
- Activity feed entries for all vote actions

## API Endpoints

### Cast Skip Vote

```http
POST /api/vote/skip
Content-Type: application/json

{
  "user_id": 123,
  "user_role": "guest"
}
```

**Responses:**
- `204 No Content` - Vote cast successfully
- `403 Forbidden` - User role cannot vote (Host)
- `409 Conflict` - User already voted in this session
- `422 Unprocessable Entity` - Queue is empty
- `500 Internal Server Error` - Server error

### Cast Priority Vote

```http
POST /api/vote/prioritize
Content-Type: application/json

{
  "user_id": 123,
  "user_role": "guest",
  "song_index": 5
}
```

**Responses:**
- `204 No Content` - Vote cast successfully
- `403 Forbidden` - User role cannot vote (Host)
- `409 Conflict` - User already voted in this session
- `422 Unprocessable Entity` - Invalid song index or voting on current song
- `500 Internal Server Error` - Server error

## WebSocket Events

### vote_updated

Broadcast after every vote cast to show live progress.

```json
{
  "type": "vote_updated",
  "data": {
    "session": {
      "id": "skip:abc123",
      "type": "skip",
      "song_id": "abc123",
      "song_title": "Example Song",
      "song_index": 0,
      "voted_by": {
        "1": true,
        "2": true
      },
      "threshold": 3,
      "created_at": "2026-05-04T08:00:00Z",
      "expires_at": "2026-05-04T08:00:30Z"
    },
    "activity": {
      "timestamp": "2026-05-04T08:00:15Z",
      "type": "vote_cast",
      "user": "Alice",
      "description": "voted to skip \"Example Song\" (2/3)"
    }
  }
}
```

### vote_resolved

Broadcast when a vote passes or expires.

```json
{
  "type": "vote_resolved",
  "data": {
    "session_id": "skip:abc123",
    "outcome": "passed",
    "activity": {
      "timestamp": "2026-05-04T08:00:20Z",
      "type": "vote_passed",
      "user": "System",
      "description": "vote to skip \"Example Song\" passed"
    }
  }
}
```

**Outcome values:**
- `"passed"` - Threshold reached, action executed
- `"expired"` - Time limit reached, no action

## Activity Feed

Vote actions are logged to the activity feed:

- **vote_cast**: `"Alice voted to skip \"Song Title\" (2/3)"`
- **vote_passed**: `"vote to skip \"Song Title\" passed"`
- **vote_expired**: `"vote to skip \"Song Title\" expired"`

## Edge Cases

### Song Removed During Vote

If the target song is removed from the queue while a vote is active:
- Skip vote: Session becomes invalid, automatically cleaned up
- Priority vote: When vote passes, checks if song still exists; if not, no action taken

### Queue Changes During Vote

Priority votes handle queue modifications:
1. Vote session stores the original song index
2. When vote passes, re-loads the queue
3. Resolves the current index by song ID (fast path) or linear scan (slow path)
4. If song not found, vote passes but no action taken

### Last Song Skip

When voting to skip the last song in the queue:
- Vote passes normally
- Queue status set to `idle`
- No error returned to user

### Concurrent Votes

Multiple vote sessions can coexist:
- One skip vote per song (keyed by `"skip:<songID>"`)
- One priority vote per song (keyed by `"prioritize:<songID>"`)
- Skip and priority votes for different songs are independent

## Technical Details

### Session Storage

Vote sessions are stored in-memory in the `vote.Interactor`:
```go
sessions map[string]*entity.VoteSession
```

**Key format:** `"<type>:<songID>"`
- Example: `"skip:abc123"`, `"prioritize:xyz789"`

### Expiry Mechanism

A background ticker in the WebSocket hub runs every 5 seconds:
```go
case <-ticker.C:
    voteInteractor.ExpireOldSessions(ctx)
```

Expired sessions are:
1. Removed from the sessions map
2. Logged to the activity feed
3. Broadcast as `vote_resolved` with outcome `"expired"`

### Concurrency Safety

All vote operations are protected by a mutex:
```go
i.mu.Lock()
defer i.mu.Unlock()
```

This ensures:
- Thread-safe session creation and deletion
- Atomic vote counting
- Consistent threshold checks

### Initial Sync

When a client connects via WebSocket:
1. Receives full queue state
2. Receives all active vote sessions as individual `vote_updated` events
3. Can immediately see and participate in ongoing votes

## Constraints

1. **No Persistence**: Vote sessions are never saved to the database. Server restart clears all active votes.

2. **Fixed Threshold**: The vote threshold is calculated once at session creation and never changes, even if users join or leave.

3. **Role Restriction**: Only Guest and Admin roles can vote. Host role is excluded because they have direct controls.

4. **One Vote Per User**: Each user can only vote once per session. Attempting to vote again returns `ErrAlreadyVoted`.

5. **Time Limit**: All vote sessions expire after 30 seconds. This cannot be configured per-vote.

6. **Minimum Threshold**: Even with 1 connected user, the minimum threshold is 2 votes (prevents self-passing).

## Future Enhancements

Potential improvements for future versions:

- **Configurable Expiry**: Allow different time limits per vote type
- **Vote Cancellation**: Allow users to retract their vote
- **Vote History**: Persist vote outcomes for analytics
- **Threshold Modes**: Support different voting thresholds (simple majority, supermajority, unanimous)
- **Vote Notifications**: Push notifications when votes are close to passing
- **Vote Cooldown**: Prevent spam by limiting vote frequency per user
