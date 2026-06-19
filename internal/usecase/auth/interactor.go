package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"net/http"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid Google ID token")
)

// GoogleUserInfo represents user information from Google OAuth.
type GoogleUserInfo struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// Interactor handles authentication logic.
type Interactor struct {
	userRepo     repository.UserRepository
	clientID     string
	hostEmails   []string
	adminEmails  []string
	sessionStore SessionStore
	clock        Clock
}

// NewInteractor creates a new Auth Interactor.
func NewInteractor(userRepo repository.UserRepository, clientID string, hostEmails, adminEmails []string, sessionStore SessionStore, clock Clock) *Interactor {
	return &Interactor{
		userRepo:     userRepo,
		clientID:     clientID,
		hostEmails:   hostEmails,
		adminEmails:  adminEmails,
		sessionStore: sessionStore,
		clock:        clock,
	}
}

// GetSessionStore returns the configured session store.
func (i *Interactor) GetSessionStore() SessionStore {
	return i.sessionStore
}

// CreateSession creates a new session for the user ID with a 12-hour TTL.
func (i *Interactor) CreateSession(ctx context.Context, userID int) (string, time.Time, error) {
	return i.sessionStore.Create(ctx, userID, 12*time.Hour)
}

// ResolveSession resolves a session token to the user, loading the user from repository.
func (i *Interactor) ResolveSession(ctx context.Context, token string) (*entity.User, error) {
	userID, err := i.sessionStore.Resolve(ctx, token)
	if err != nil {
		return nil, err
	}

	user, err := i.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserByID loads a user by ID from the repository.
func (i *Interactor) GetUserByID(ctx context.Context, id int) (*entity.User, error) {
	return i.userRepo.GetUserByID(ctx, id)
}

// VerifyGoogleToken verifies the Google ID token and returns user info.
func (i *Interactor) VerifyGoogleToken(ctx context.Context, idToken string) (*GoogleUserInfo, error) {
	// Use Google's tokeninfo endpoint to verify the token
	url := fmt.Sprintf("https://oauth2.googleapis.com/tokeninfo?id_token=%s", idToken)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to verify token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, ErrInvalidToken
	}

	var tokenInfo struct {
		Email         string `json:"email"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
		Aud           string `json:"aud"`
		EmailVerified string `json:"email_verified"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenInfo); err != nil {
		return nil, fmt.Errorf("failed to decode token info: %w", err)
	}

	// Verify the token is for our client ID
	if tokenInfo.Aud != i.clientID {
		return nil, ErrInvalidToken
	}

	// Verify email is verified
	if tokenInfo.EmailVerified != "true" {
		return nil, errors.New("email not verified")
	}

	return &GoogleUserInfo{
		Email:   tokenInfo.Email,
		Name:    tokenInfo.Name,
		Picture: tokenInfo.Picture,
	}, nil
}

// LoginWithGoogle creates or updates user from Google account.
func (i *Interactor) LoginWithGoogle(ctx context.Context, idToken string) (*entity.User, error) {
	// 1. Verify Google ID token
	googleUser, err := i.VerifyGoogleToken(ctx, idToken)
	if err != nil {
		return nil, err
	}

	// 1.1 Check if email is in allow domain (optional, can be skipped if we allow any Google account)
	if !strings.HasSuffix(googleUser.Email, "@urekamedia.vn") {
		return nil, errors.New("email not allowed")
	}

	// 2. Check if user exists by email
	user, err := i.userRepo.GetUserByEmail(ctx, googleUser.Email)
	if err != nil {
		// Create new user
		user = &entity.User{
			Email:           googleUser.Email,
			DisplayName:     googleUser.Name,
			ProfilePicture:  googleUser.Picture,
			Role:            entity.RoleGuest, // Default role
			PriorityBalance: 0,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}

		// Check if this email should be host/admin
		if i.isHostEmail(googleUser.Email) {
			user.Role = entity.RoleHost
		} else if i.isAdminEmail(googleUser.Email) {
			user.Role = entity.RoleAdmin
		}

		if err := i.userRepo.CreateUser(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
	} else {
		// Update existing user info
		user.DisplayName = googleUser.Name
		user.ProfilePicture = googleUser.Picture
		user.UpdatedAt = time.Now()

		// Update role if email is in whitelist
		if i.isHostEmail(googleUser.Email) {
			user.Role = entity.RoleHost
		} else if i.isAdminEmail(googleUser.Email) {
			user.Role = entity.RoleAdmin
		}

		if err := i.userRepo.UpdateUser(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to update user: %w", err)
		}
	}

	return user, nil
}

// isHostEmail checks if the email is in the host whitelist.
func (i *Interactor) isHostEmail(email string) bool {
	email = strings.ToLower(email)
	for _, hostEmail := range i.hostEmails {
		if strings.ToLower(hostEmail) == email {
			return true
		}
	}
	return false
}

// isAdminEmail checks if the email is in the admin whitelist.
func (i *Interactor) isAdminEmail(email string) bool {
	email = strings.ToLower(email)
	for _, adminEmail := range i.adminEmails {
		if strings.ToLower(adminEmail) == email {
			return true
		}
	}
	return false
}
