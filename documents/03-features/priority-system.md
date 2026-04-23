# Priority Queue System

The priority system allows users to earn daily tokens and spend them to move their songs to the front of the queue.

## Overview

**Key Features:**
- Users earn **1 priority token per day** on first login
- Tokens can be spent to **prioritize own songs** (move to front of queue)
- **Transaction logging** tracks all token awards and spending
- **UNIQUE constraint** prevents double-claiming (distributed lock)
- **Balance display** shows current tokens in UI
- **WebSocket events** notify all clients of priority changes

---

## How It Works

### Daily Token Award

**Trigger**: First login of the day (any time after midnight)

**Mechanism**:
1. User logs in with Google OAuth
2. Backend checks `user_sessions` table for today's date
3. If no session exists for today:
   - Award 1 priority token
   - Insert session record with UNIQUE constraint
   - Log transaction
4. If session exists:
   - No token awarded (already claimed today)

**Database Schema**:
```sql
CREATE TABLE user_sessions (
    user_id TEXT NOT NULL,
    session_date TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, session_date)  -- Prevents double-claiming
);
```

**Implementation** (`internal/usecase/priority/interactor.go`):
```go
func (i *Interactor) CheckAndAwardDailyPriority(ctx context.Context, userID string) error {
    today := time.Now().Format("2006-01-02")
    
    // Try to insert session record
    err := i.userRepo.CreateSession(ctx, userID, today)
    if err != nil {
        // Session already exists (UNIQUE constraint violation)
        return nil
    }
    
    // Award 1 token
    err = i.userRepo.UpdatePriorityBalance(ctx, userID, 1)
    if err != nil {
        return err
    }
    
    // Log transaction
    i.userRepo.LogPriorityTransaction(ctx, userID, 1, "daily_award")
    
    return nil
}
```

---

### Prioritizing a Song

**Requirements**:
- User must have at least 1 priority token
- User can only prioritize their own songs
- Song must be in the queue (not currently playing)

**Effect**:
- Song moves to index 0 (front of queue)
- User's balance decreases by 1
- All clients notified via WebSocket

**API Endpoint**: `POST /api/queue/prioritize`

**Request**:
```json
{
  "userId": "user-123",
  "songIndex": 3
}
```

**Response (Success)**:
```json
{
  "success": true,
  "newBalance": 0
}
```

**Response (Error)**:
```json
{
  "error": "insufficient priority balance"
}
```

**Implementation** (`internal/usecase/priority/interactor.go`):
```go
func (i *Interactor) PrioritizeSong(ctx context.Context, userID string, songIndex int) error {
    // 1. Check balance
    balance, err := i.userRepo.GetPriorityBalance(ctx, userID)
    if err != nil || balance < 1 {
        return errors.New("insufficient priority balance")
    }
    
    // 2. Get queue state
    queue, err := i.queueRepo.Load(ctx)
    if err != nil {
        return err
    }
    
    // 3. Validate song index
    if songIndex < 0 || songIndex >= len(queue.Songs) {
        return errors.New("invalid song index")
    }
    
    // 4. Check ownership
    song := queue.Songs[songIndex]
    if song.AddedByID != userID {
        return errors.New("can only prioritize own songs")
    }
    
    // 5. Move song to front
    queue.Songs = append([]Song{song}, append(queue.Songs[:songIndex], queue.Songs[songIndex+1:]...)...)
    
    // 6. Mark as prioritized
    queue.Songs[0].IsPrioritized = true
    
    // 7. Save queue
    err = i.queueRepo.Save(ctx, queue)
    if err != nil {
        return err
    }
    
    // 8. Deduct token
    err = i.userRepo.UpdatePriorityBalance(ctx, userID, -1)
    if err != nil {
        return err
    }
    
    // 9. Log transaction
    i.userRepo.LogPriorityTransaction(ctx, userID, -1, "prioritize_song")
    
    return nil
}
```

---

## Database Schema

### Users Table
```sql
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    profile_picture_url TEXT,
    priority_balance INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### User Sessions Table
```sql
CREATE TABLE user_sessions (
    user_id TEXT NOT NULL,
    session_date TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, session_date),
    FOREIGN KEY(user_id) REFERENCES users(id)
);
```

### Priority Transactions Table
```sql
CREATE TABLE priority_transactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    amount INTEGER NOT NULL,  -- +1 for award, -1 for spend
    transaction_type TEXT NOT NULL,  -- 'daily_award' or 'prioritize_song'
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(user_id) REFERENCES users(id)
);
```

---

## Frontend Integration

### Balance Display

**File**: `frontend/src/components/dashboard/QueueList.vue`

```vue
<template>
  <div class="priority-badge">
    <span class="token-icon">⚡</span>
    <span class="balance">{{ user.priorityBalance }}</span>
    <span class="tooltip">Priority tokens available</span>
  </div>
</template>
```

### Prioritize Button

```vue
<template>
  <div v-for="(song, index) in queue" :key="index" class="song-item">
    <span class="song-title">{{ song.title }}</span>
    
    <!-- Show prioritize button for own songs -->
    <button 
      v-if="song.addedById === user.id && user.priorityBalance > 0"
      @click="prioritizeSong(index)"
      class="prioritize-btn"
    >
      ⚡ Prioritize
    </button>
    
    <!-- Show prioritized badge -->
    <span v-if="song.isPrioritized" class="prioritized-badge">
      ⚡ Prioritized
    </span>
  </div>
</template>

<script setup>
async function prioritizeSong(index) {
  if (!confirm('Spend 1 priority token to move this song to the front?')) {
    return
  }
  
  try {
    await api.prioritizeSong(user.id, index)
    // Balance updated via WebSocket event
  } catch (error) {
    alert(error.message)
  }
}
</script>
```

---

## WebSocket Events

### `priority_balance_updated`

Sent when a user's balance changes (award or spend).

**Payload**:
```json
{
  "type": "priority_balance_updated",
  "sequence": 42,
  "data": {
    "userId": "user-123",
    "newBalance": 0
  }
}
```

**Frontend Handler**:
```javascript
case 'priority_balance_updated':
  if (data.userId === store.user.id) {
    store.user.priorityBalance = data.newBalance
  }
  break
```

### `song_prioritized`

Sent when a song is moved to the front of the queue.

**Payload**:
```json
{
  "type": "song_prioritized",
  "sequence": 43,
  "data": {
    "userId": "user-123",
    "userName": "John Doe",
    "songTitle": "Never Gonna Give You Up",
    "newQueueOrder": [...]
  }
}
```

**Frontend Handler**:
```javascript
case 'song_prioritized':
  store.queue = data.newQueueOrder
  store.addActivity({
    type: 'song_prioritized',
    userName: data.userName,
    songTitle: data.songTitle
  })
  break
```

---

## API Reference

### GET `/api/user/priority-balance`

Get current user's priority balance.

**Headers**:
```
Authorization: Bearer <user-id>
```

**Response**:
```json
{
  "balance": 1
}
```

---

### POST `/api/queue/prioritize`

Prioritize a song (move to front of queue).

**Request**:
```json
{
  "userId": "user-123",
  "songIndex": 3
}
```

**Response (Success)**:
```json
{
  "success": true,
  "newBalance": 0
}
```

**Response (Errors)**:
```json
// Insufficient balance
{
  "error": "insufficient priority balance"
}

// Not own song
{
  "error": "can only prioritize own songs"
}

// Invalid index
{
  "error": "invalid song index"
}
```

---

## Business Rules

### Token Award Rules
1. **One token per day**: Users can only claim once per calendar day
2. **Automatic on login**: No manual claim required
3. **No expiration**: Tokens don't expire
4. **No maximum**: Users can accumulate unlimited tokens

### Prioritization Rules
1. **Own songs only**: Users can only prioritize songs they added
2. **Costs 1 token**: Each prioritization costs exactly 1 token
3. **Moves to front**: Song always moves to index 0
4. **Marks as prioritized**: `isPrioritized` flag set to true
5. **Cannot prioritize current song**: Only songs in upcoming queue

---

## Race Condition Prevention

### Problem
Multiple concurrent logins could award multiple tokens for the same day.

### Solution
Use database UNIQUE constraint as a distributed lock:

```sql
UNIQUE(user_id, session_date)
```

**How it works**:
1. First login attempt: INSERT succeeds → Award token
2. Second login attempt: INSERT fails (UNIQUE violation) → No token awarded
3. Database guarantees atomicity

**Implementation**:
```go
func (r *SQLiteUserRepository) CreateSession(ctx context.Context, userID, date string) error {
    _, err := r.db.ExecContext(ctx,
        "INSERT INTO user_sessions (user_id, session_date) VALUES (?, ?)",
        userID, date,
    )
    
    if err != nil {
        // Check if UNIQUE constraint violation
        if strings.Contains(err.Error(), "UNIQUE constraint failed") {
            return ErrSessionAlreadyExists
        }
        return err
    }
    
    return nil
}
```

---

## Transaction Logging

All priority token changes are logged for audit purposes.

**Transaction Types**:
- `daily_award`: Daily token award on login
- `prioritize_song`: Token spent to prioritize song

**Query Transactions**:
```sql
-- Get user's transaction history
SELECT * FROM priority_transactions 
WHERE user_id = 'user-123' 
ORDER BY timestamp DESC;

-- Get all awards today
SELECT * FROM priority_transactions 
WHERE transaction_type = 'daily_award' 
AND DATE(timestamp) = DATE('now');

-- Get total tokens awarded
SELECT user_id, SUM(amount) as total_awarded
FROM priority_transactions
WHERE transaction_type = 'daily_award'
GROUP BY user_id;
```

---

## Future Enhancements

### Potential Features (Not Implemented)
- ❌ Token purchase with real money
- ❌ Token gifting between users
- ❌ Token expiration (e.g., expire after 30 days)
- ❌ Variable token costs (e.g., 2 tokens for peak hours)
- ❌ Token rewards for contributions (e.g., adding popular songs)
- ❌ Leaderboard showing top token earners
- ❌ Admin ability to grant bonus tokens

---

## Troubleshooting

### User not receiving daily token

**Check**:
1. User logged in today (check `user_sessions` table)
2. Token was awarded (check `priority_transactions` table)
3. Balance updated (check `users` table)

**Debug**:
```sql
-- Check if session exists for today
SELECT * FROM user_sessions 
WHERE user_id = 'user-123' 
AND session_date = DATE('now');

-- Check if token was awarded
SELECT * FROM priority_transactions 
WHERE user_id = 'user-123' 
AND transaction_type = 'daily_award'
AND DATE(timestamp) = DATE('now');

-- Check current balance
SELECT priority_balance FROM users WHERE id = 'user-123';
```

### Prioritize button not showing

**Check**:
1. User has tokens (`priorityBalance > 0`)
2. Song was added by user (`song.addedById === user.id`)
3. Song is in upcoming queue (not currently playing)

**Fix**:
```javascript
// Check in browser console
console.log('User balance:', store.user.priorityBalance)
console.log('Song owner:', song.addedById)
console.log('Current user:', store.user.id)
```

### Balance not updating after prioritization

**Check**:
1. WebSocket connection is active
2. `priority_balance_updated` event received
3. Frontend store is reactive

**Fix**:
```javascript
// Check WebSocket events in console
websocket.addEventListener('message', (event) => {
  console.log('WebSocket event:', JSON.parse(event.data))
})
```

---

## Next Steps

- [Queue Management](queue-management.md) - How queue operations work
- [WebSocket Events](../04-api-reference/websocket-events.md) - All real-time events
- [Activity Tracking](activity-tracking.md) - How actions are logged
