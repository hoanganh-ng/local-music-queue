# Environment Variables Reference

Complete reference for all environment variables used in Local Music Queue.

## Backend Environment Variables

### Server Configuration

#### `PORT`
- **Description**: HTTP server port
- **Default**: `1111`
- **Example**: `PORT=8080`
- **Required**: No

#### `CERT_FILE`
- **Description**: Path to TLS certificate file for HTTPS
- **Default**: None (HTTP mode)
- **Example**: `CERT_FILE=/app/certs/cert.pem`
- **Required**: No (required for HTTPS)

#### `KEY_FILE`
- **Description**: Path to TLS private key file for HTTPS
- **Default**: None (HTTP mode)
- **Example**: `KEY_FILE=/app/certs/key.pem`
- **Required**: No (required for HTTPS)

---

### Authentication

#### `GOOGLE_CLIENT_ID`
- **Description**: Google OAuth 2.0 Client ID
- **Default**: None
- **Example**: `GOOGLE_CLIENT_ID=123456789-abc.apps.googleusercontent.com`
- **Required**: Yes
- **How to get**: [Google Cloud Console](https://console.cloud.google.com/) → APIs & Services → Credentials

#### `HOST_EMAILS`
- **Description**: Comma-separated list of emails with Host privileges
- **Default**: None
- **Example**: `HOST_EMAILS=host1@urekamedia.vn,host2@urekamedia.vn`
- **Required**: Yes
- **Privileges**: Full control (playback, queue, host YouTube player)

#### `ADMIN_EMAILS`
- **Description**: Comma-separated list of emails with Admin privileges
- **Default**: None
- **Example**: `ADMIN_EMAILS=admin1@urekamedia.vn,admin2@urekamedia.vn`
- **Required**: No
- **Privileges**: Remote playback control (cannot host player)

#### `CLIENT_PIN` (Deprecated)
- **Description**: Guest PIN for legacy PIN authentication
- **Default**: `5555`
- **Example**: `CLIENT_PIN=1234`
- **Required**: No (deprecated, use Google OAuth)
- **Status**: Kept for backward compatibility

#### `HOST_PIN` (Deprecated)
- **Description**: Host PIN for legacy PIN authentication
- **Default**: `6666`
- **Example**: `HOST_PIN=9999`
- **Required**: No (deprecated, use Google OAuth)
- **Status**: Kept for backward compatibility

---

### Database

#### `DB_PATH`
- **Description**: Path to SQLite database file
- **Default**: `./.localdb/music_queue.db`
- **Example**: `DB_PATH=/app/data/music_queue.db`
- **Required**: No
- **Note**: Directory must exist and be writable

---

### YouTube Integration

#### `YTDLP_PATH`
- **Description**: Path to yt-dlp executable
- **Default**: `yt-dlp` (searches in PATH)
- **Example**: `YTDLP_PATH=/usr/local/bin/yt-dlp`
- **Required**: No
- **Note**: yt-dlp must be installed and accessible

---

## Frontend Environment Variables

### API Configuration

#### `VITE_API_BASE_URL`
- **Description**: Backend API base URL
- **Default**: `http://localhost:1111`
- **Example**: `VITE_API_BASE_URL=https://api.yourdomain.com`
- **Required**: Yes
- **Note**: Used for REST API and WebSocket connections

#### `VITE_GOOGLE_CLIENT_ID`
- **Description**: Google OAuth 2.0 Client ID (same as backend)
- **Default**: None
- **Example**: `VITE_GOOGLE_CLIENT_ID=123456789-abc.apps.googleusercontent.com`
- **Required**: Yes
- **Note**: Must match backend `GOOGLE_CLIENT_ID`

---

## Docker Compose Environment Variables

### Port Configuration

#### `FRONTEND_HTTP_PORT`
- **Description**: Frontend HTTP port (host machine)
- **Default**: `8011`
- **Example**: `FRONTEND_HTTP_PORT=80`
- **Required**: No

#### `FRONTEND_HTTPS_PORT`
- **Description**: Frontend HTTPS port (host machine)
- **Default**: `8012`
- **Example**: `FRONTEND_HTTPS_PORT=443`
- **Required**: No

#### `BACKEND_PORT`
- **Description**: Backend API port (host machine)
- **Default**: `1111`
- **Example**: `BACKEND_PORT=8080`
- **Required**: No

---

### HTTPS Configuration (DuckDNS + Let's Encrypt)

#### `DUCKDNS_DOMAIN`
- **Description**: DuckDNS subdomain for dynamic DNS
- **Default**: None
- **Example**: `DUCKDNS_DOMAIN=myapp.duckdns.org`
- **Required**: No (required for HTTPS)
- **How to get**: Register at [DuckDNS](https://www.duckdns.org/)

#### `DUCKDNS_TOKEN`
- **Description**: DuckDNS authentication token
- **Default**: None
- **Example**: `DUCKDNS_TOKEN=12345678-1234-1234-1234-123456789abc`
- **Required**: No (required for HTTPS)
- **How to get**: Available after DuckDNS registration

#### `LETSENCRYPT_EMAIL`
- **Description**: Email for Let's Encrypt certificate notifications
- **Default**: None
- **Example**: `LETSENCRYPT_EMAIL=admin@yourdomain.com`
- **Required**: No (required for HTTPS)
- **Note**: Used for certificate expiration warnings

---

## Configuration Examples

### Development (Local)

**Backend `.env`:**
```bash
PORT=1111
DB_PATH=./.localdb/music_queue.db
YTDLP_PATH=yt-dlp

GOOGLE_CLIENT_ID=123456789-abc.apps.googleusercontent.com
HOST_EMAILS=dev@urekamedia.vn
ADMIN_EMAILS=admin@urekamedia.vn
```

**Frontend `.env`:**
```bash
VITE_API_BASE_URL=http://localhost:1111
VITE_GOOGLE_CLIENT_ID=123456789-abc.apps.googleusercontent.com
```

---

### Production (Docker, HTTP)

**`.env` (Docker Compose):**
```bash
# Ports
FRONTEND_HTTP_PORT=80
BACKEND_PORT=1111

# Authentication
GOOGLE_CLIENT_ID=123456789-abc.apps.googleusercontent.com
HOST_EMAILS=host@company.com
ADMIN_EMAILS=admin@company.com
```

---

### Production (Docker, HTTPS)

**`.env` (Docker Compose):**
```bash
# Ports
FRONTEND_HTTP_PORT=80
FRONTEND_HTTPS_PORT=443
BACKEND_PORT=1111

# Authentication
GOOGLE_CLIENT_ID=123456789-abc.apps.googleusercontent.com
HOST_EMAILS=host@company.com
ADMIN_EMAILS=admin@company.com

# HTTPS
DUCKDNS_DOMAIN=myapp.duckdns.org
DUCKDNS_TOKEN=12345678-1234-1234-1234-123456789abc
LETSENCRYPT_EMAIL=admin@company.com
```

---

## Email Domain Restriction

By default, only `@urekamedia.vn` emails can authenticate. To change this:

**Edit `internal/usecase/auth/interactor.go`:**
```go
// Line ~50
allowedDomain := "yourdomain.com"  // Change domain
```

**Or remove restriction:**
```go
// Comment out domain check
// if !strings.HasSuffix(email, "@" + allowedDomain) {
//     return nil, fmt.Errorf("email domain not allowed")
// }
```

---

## Role Assignment Logic

Users are assigned roles based on email matching:

1. **Host**: Email in `HOST_EMAILS` → Full control + player hosting
2. **Admin**: Email in `ADMIN_EMAILS` → Remote playback control
3. **Guest**: All other authenticated users → Add songs, use priority

**Example:**
```bash
HOST_EMAILS=alice@company.com,bob@company.com
ADMIN_EMAILS=charlie@company.com

# alice@company.com → Host
# bob@company.com → Host
# charlie@company.com → Admin
# dave@company.com → Guest
```

---

## Security Best Practices

### 1. Protect Sensitive Variables

Never commit `.env` files to version control:

```bash
# .gitignore
.env
.env.local
.env.production
```

### 2. Use Strong Tokens

```bash
# Generate secure DuckDNS token
# Use the token provided by DuckDNS (UUID format)

# Generate secure Google Client ID
# Use the ID provided by Google Cloud Console
```

### 3. Restrict Email Access

```bash
# Only add trusted users
HOST_EMAILS=trusted-host@company.com
ADMIN_EMAILS=trusted-admin@company.com

# Don't use wildcards or public domains
# ❌ HOST_EMAILS=*@gmail.com
# ✅ HOST_EMAILS=specific-user@company.com
```

### 4. Use HTTPS in Production

```bash
# Always configure HTTPS for production
DUCKDNS_DOMAIN=myapp.duckdns.org
DUCKDNS_TOKEN=your-token
LETSENCRYPT_EMAIL=admin@company.com
```

---

## Validation

The backend validates configuration on startup:

```go
// Checks performed:
// 1. yt-dlp executable exists
// 2. Database directory is writable
// 3. Google Client ID is set
// 4. At least one HOST_EMAIL is configured
```

**Startup errors:**
```
FATAL: yt-dlp not found at path: /usr/bin/yt-dlp
FATAL: database directory not writable: /app/data
FATAL: GOOGLE_CLIENT_ID not set
FATAL: HOST_EMAILS not configured
```

---

## Troubleshooting

### Backend won't start

```bash
# Check required variables are set
echo $GOOGLE_CLIENT_ID
echo $HOST_EMAILS

# Verify yt-dlp path
which yt-dlp

# Check database directory
ls -la .localdb/
```

### Frontend can't connect

```bash
# Verify API URL
echo $VITE_API_BASE_URL

# Check backend is running
curl http://localhost:1111/api/queue

# Verify Google Client ID matches
echo $VITE_GOOGLE_CLIENT_ID  # Should match backend
```

### Google OAuth not working

```bash
# Verify Client ID format
# Should be: xxxxxxxxx-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx.apps.googleusercontent.com

# Check authorized origins in Google Cloud Console
# Must include: http://localhost:5173 (dev) or your production domain

# Verify email domain restriction
# Check internal/usecase/auth/interactor.go line ~50
```

---

## Next Steps

- [First Run Guide](first-run.md) - Initial configuration walkthrough
- [HTTPS Setup](../07-deployment/https-setup.md) - Configure Let's Encrypt
- [Google OAuth Setup](../03-features/authentication.md) - Detailed OAuth configuration
