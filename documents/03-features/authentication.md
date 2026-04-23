# Google OAuth Authentication

Local Music Queue uses Google OAuth 2.0 for secure, email-based authentication with role assignment.

## Overview

**Authentication Flow:**
1. User clicks "Sign in with Google" button
2. Google OAuth popup opens
3. User selects Google account
4. Frontend receives ID token
5. Backend verifies token with Google
6. Backend assigns role based on email
7. User session created with JWT

**Role Assignment:**
- **Host**: Email in `HOST_EMAILS` → Full control + player hosting
- **Admin**: Email in `ADMIN_EMAILS` → Remote playback control
- **Guest**: All other authenticated users → Add songs, use priority

---

## Features

### Implemented
- ✅ Google Sign-In button integration
- ✅ ID token verification via Google's tokeninfo endpoint
- ✅ Email-based role assignment (Host/Admin/Guest)
- ✅ Email domain restriction (`@urekamedia.vn` by default)
- ✅ User profile pictures from Google accounts
- ✅ Session persistence in localStorage
- ✅ Automatic token refresh
- ✅ Role-based UI rendering

### Security
- ✅ Server-side token verification (not client-side only)
- ✅ Email domain whitelist
- ✅ Role-based access control (RBAC)
- ✅ No password storage required
- ✅ Leverages Google's security infrastructure

---

## Setup Guide

### 1. Create Google Cloud Project

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Click **Select a project** → **New Project**
3. Enter project name: "Local Music Queue"
4. Click **Create**

### 2. Enable Required APIs

1. Navigate to **APIs & Services** → **Library**
2. Search for "Google+ API" or "People API"
3. Click **Enable**

### 3. Create OAuth 2.0 Credentials

1. Go to **APIs & Services** → **Credentials**
2. Click **Create Credentials** → **OAuth client ID**
3. If prompted, configure OAuth consent screen:
   - **User Type**: Internal (for organization) or External (for public)
   - **App name**: Local Music Queue
   - **User support email**: Your email
   - **Developer contact**: Your email
   - **Scopes**: Add `email` and `profile`
   - Click **Save and Continue**

4. Create OAuth Client ID:
   - **Application type**: Web application
   - **Name**: Local Music Queue Web Client
   - **Authorized JavaScript origins**:
     ```
     http://localhost:5173
     http://localhost
     http://localhost:8011
     https://yourapp.duckdns.org
     ```
   - **Authorized redirect URIs**:
     ```
     http://localhost:5173
     http://localhost
     http://localhost:8011
     https://yourapp.duckdns.org
     ```
   - Click **Create**

5. Copy the **Client ID** (format: `xxxxx.apps.googleusercontent.com`)

### 4. Configure Environment Variables

**Backend `.env`:**
```bash
GOOGLE_CLIENT_ID=123456789-abcdefghijklmnop.apps.googleusercontent.com
HOST_EMAILS=host@urekamedia.vn
ADMIN_EMAILS=admin@urekamedia.vn
```

**Frontend `.env`:**
```bash
VITE_GOOGLE_CLIENT_ID=123456789-abcdefghijklmnop.apps.googleusercontent.com
```

**Note**: Both must use the same Client ID.

---

## Authentication Flow Details

### Frontend Flow

**File**: `frontend/src/views/AuthView.vue`

```javascript
// 1. Load Google Sign-In library
<script src="https://accounts.google.com/gsi/client" async defer></script>

// 2. Initialize Google Sign-In button
google.accounts.id.initialize({
  client_id: import.meta.env.VITE_GOOGLE_CLIENT_ID,
  callback: handleCredentialResponse
})

// 3. Handle credential response
async function handleCredentialResponse(response) {
  const idToken = response.credential
  
  // 4. Send token to backend
  const result = await api.loginWithGoogle(idToken)
  
  // 5. Store session
  store.setUser(result.user)
  
  // 6. Redirect to dashboard
  router.push('/')
}
```

### Backend Flow

**File**: `internal/delivery/http/handlers.go`

```go
func (h *Handler) HandleGoogleLogin(w http.ResponseWriter, r *http.Request) {
    // 1. Extract ID token from request
    var req struct {
        IDToken string `json:"idToken"`
    }
    json.NewDecoder(r.Body).Decode(&req)
    
    // 2. Verify token with Google
    user, err := h.authUsecase.LoginWithGoogle(r.Context(), req.IDToken)
    
    // 3. Return user with assigned role
    json.NewEncoder(w).Encode(user)
}
```

**File**: `internal/usecase/auth/interactor.go`

```go
func (i *Interactor) LoginWithGoogle(ctx context.Context, idToken string) (*entity.User, error) {
    // 1. Verify token with Google's tokeninfo endpoint
    tokenInfo, err := i.VerifyGoogleToken(ctx, idToken)
    
    // 2. Extract email and profile
    email := tokenInfo.Email
    name := tokenInfo.Name
    picture := tokenInfo.Picture
    
    // 3. Check email domain
    if !strings.HasSuffix(email, "@urekamedia.vn") {
        return nil, errors.New("email domain not allowed")
    }
    
    // 4. Assign role based on email
    role := entity.RoleGuest
    if contains(i.hostEmails, email) {
        role = entity.RoleHost
    } else if contains(i.adminEmails, email) {
        role = entity.RoleAdmin
    }
    
    // 5. Create or update user in database
    user := &entity.User{
        Email:             email,
        Name:              name,
        Role:              role,
        ProfilePictureURL: picture,
        PriorityBalance:   0, // Will be set by daily award
    }
    
    // 6. Award daily priority token
    i.priorityUsecase.CheckAndAwardDailyPriority(ctx, user.ID)
    
    return user, nil
}
```

### Token Verification

**File**: `internal/usecase/auth/interactor.go`

```go
func (i *Interactor) VerifyGoogleToken(ctx context.Context, idToken string) (*TokenInfo, error) {
    // Call Google's tokeninfo endpoint
    url := fmt.Sprintf("https://oauth2.googleapis.com/tokeninfo?id_token=%s", idToken)
    
    resp, err := http.Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != 200 {
        return nil, errors.New("invalid token")
    }
    
    var tokenInfo TokenInfo
    json.NewDecoder(resp.Body).Decode(&tokenInfo)
    
    // Verify audience (client ID)
    if tokenInfo.Aud != i.googleClientID {
        return nil, errors.New("token audience mismatch")
    }
    
    return &tokenInfo, nil
}
```

---

## Role-Based Access Control

### Role Capabilities

| Feature | Host | Admin | Guest |
|---------|------|-------|-------|
| Add songs | ✅ | ✅ | ✅ |
| Use priority tokens | ✅ | ✅ | ✅ |
| View queue | ✅ | ✅ | ✅ |
| View activity log | ✅ | ✅ | ✅ |
| Play/Pause | ✅ | ✅ | ❌ |
| Skip/Previous | ✅ | ✅ | ❌ |
| Remove songs | ✅ | ✅ | ❌ |
| Clear queue | ✅ | ✅ | ❌ |
| Volume control | ✅ | ✅ | ❌ |
| Host YouTube player | ✅ | ❌ | ❌ |

### Implementation

**Backend** (`internal/domain/entity/user.go`):
```go
func (u *User) CanControlPlayback() bool {
    return u.Role == RoleHost || u.Role == RoleAdmin
}

func (u *User) CanHostPlayer() bool {
    return u.Role == RoleHost
}
```

**Frontend** (`src/components/dashboard/NowPlaying.vue`):
```vue
<template>
  <!-- Only show player for Host -->
  <div v-if="user.role === 'Host'" class="youtube-player">
    <iframe :src="youtubeEmbedUrl"></iframe>
  </div>
  
  <!-- Show controls for Host and Admin -->
  <div v-if="canControlPlayback" class="controls">
    <button @click="play">Play</button>
    <button @click="pause">Pause</button>
    <button @click="skip">Skip</button>
  </div>
</template>

<script setup>
const canControlPlayback = computed(() => {
  return user.role === 'Host' || user.role === 'Admin'
})
</script>
```

---

## Email Domain Restriction

By default, only `@urekamedia.vn` emails can authenticate.

### Change Allowed Domain

**Edit** `internal/usecase/auth/interactor.go`:
```go
// Line ~50
allowedDomain := "yourcompany.com"  // Change this
```

### Allow Multiple Domains

```go
allowedDomains := []string{"company1.com", "company2.com"}

domainAllowed := false
for _, domain := range allowedDomains {
    if strings.HasSuffix(email, "@" + domain) {
        domainAllowed = true
        break
    }
}

if !domainAllowed {
    return nil, errors.New("email domain not allowed")
}
```

### Remove Domain Restriction

```go
// Comment out the domain check
// if !strings.HasSuffix(email, "@" + allowedDomain) {
//     return nil, fmt.Errorf("email domain not allowed")
// }
```

---

## Session Management

### Frontend Session Storage

**File**: `frontend/src/store/index.js`

```javascript
// Store user in localStorage
function setUser(user) {
  state.user = user
  localStorage.setItem('user', JSON.stringify(user))
}

// Load user on app start
function loadSession() {
  const stored = localStorage.getItem('user')
  if (stored) {
    state.user = JSON.parse(stored)
  }
}

// Clear session on logout
function logout() {
  state.user = null
  localStorage.removeItem('user')
  router.push('/auth')
}
```

### Backend Session (Stateless)

The backend is **stateless** - no server-side sessions. Each request includes user info from the frontend, and the backend verifies it via Google token validation.

---

## Security Considerations

### Token Verification
- ✅ Always verify tokens server-side
- ✅ Check token audience matches your Client ID
- ✅ Verify token hasn't expired
- ❌ Never trust client-side token validation alone

### Email Whitelisting
- ✅ Use specific emails, not wildcards
- ✅ Store in environment variables, not code
- ❌ Don't use public domains (gmail.com, yahoo.com)

### HTTPS in Production
- ✅ Always use HTTPS for OAuth in production
- ✅ Configure authorized origins with HTTPS URLs
- ❌ Don't use HTTP for production OAuth

---

## Troubleshooting

### "Sign in with Google" button not showing

**Check:**
1. `VITE_GOOGLE_CLIENT_ID` is set in frontend `.env`
2. Google Sign-In library is loaded (check browser console)
3. Authorized JavaScript origins include your domain

**Fix:**
```bash
# Verify env var
echo $VITE_GOOGLE_CLIENT_ID

# Rebuild frontend
cd frontend && npm run build
```

### "Invalid token" error

**Check:**
1. Client ID matches between frontend and backend
2. Token hasn't expired (tokens expire after 1 hour)
3. Backend can reach `oauth2.googleapis.com`

**Fix:**
```bash
# Test token verification manually
curl "https://oauth2.googleapis.com/tokeninfo?id_token=YOUR_TOKEN"
```

### "Email domain not allowed" error

**Check:**
1. Your email domain matches `allowedDomain` in code
2. Email is verified in Google account

**Fix:**
```go
// Edit internal/usecase/auth/interactor.go
allowedDomain := "yourdomain.com"  // Change this
```

### User assigned wrong role

**Check:**
1. `HOST_EMAILS` and `ADMIN_EMAILS` are set correctly
2. Email matches exactly (case-sensitive)
3. No extra whitespace in environment variables

**Fix:**
```bash
# Check env vars
echo $HOST_EMAILS
echo $ADMIN_EMAILS

# Restart backend
docker-compose restart backend
```

---

## Migration from PIN Authentication

The old PIN-based authentication is deprecated but still present in the code.

### Remove PIN Authentication

**1. Remove from backend:**
```go
// Delete internal/delivery/http/handlers.go: HandlePINLogin
// Remove CLIENT_PIN and HOST_PIN from config
```

**2. Remove from frontend:**
```vue
<!-- Delete PIN input form from AuthView.vue -->
```

**3. Update environment:**
```bash
# Remove from .env
# CLIENT_PIN=5555
# HOST_PIN=6666
```

---

## API Reference

### POST `/api/auth/google`

**Request:**
```json
{
  "idToken": "eyJhbGciOiJSUzI1NiIsImtpZCI6..."
}
```

**Response (Success):**
```json
{
  "user": {
    "id": "123",
    "email": "user@urekamedia.vn",
    "name": "John Doe",
    "role": "Host",
    "profilePictureUrl": "https://lh3.googleusercontent.com/...",
    "priorityBalance": 1
  }
}
```

**Response (Error):**
```json
{
  "error": "email domain not allowed"
}
```

---

## Next Steps

- [Permissions Guide](permissions.md) - Detailed role capabilities
- [Priority System](priority-system.md) - Daily token awards
- [API Reference](../04-api-reference/rest-endpoints.md) - All endpoints
