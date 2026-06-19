package auth

import (
	"context"
	"errors"
	"local-music-queue/internal/domain/entity"
	"testing"
	"time"
)

type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time {
	return m.now
}

type mockSessionStore struct {
	sessions map[string]int
	token    string
	err      error
}

func (m *mockSessionStore) Create(ctx context.Context, userID int, ttl time.Duration) (string, time.Time, error) {
	if m.err != nil {
		return "", time.Time{}, m.err
	}
	m.sessions[m.token] = userID
	return m.token, time.Now().Add(ttl), nil
}

func (m *mockSessionStore) Resolve(ctx context.Context, token string) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	id, ok := m.sessions[token]
	if !ok {
		return 0, errors.New("not found")
	}
	return id, nil
}

func (m *mockClock) nowFunc() time.Time {
	return m.now
}

var ErrNotFound = errors.New("not found")

// Mock UserRepository for testing
type mockUserRepo struct {
	users       map[string]*entity.User
	createErr   error
	getErr      error
	updateErr   error
	nextID      int
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users:  make(map[string]*entity.User),
		nextID: 1,
	}
}

func (m *mockUserRepo) CreateUser(ctx context.Context, user *entity.User) error {
	if m.createErr != nil {
		return m.createErr
	}
	user.ID = m.nextID
	m.nextID++
	m.users[user.Email] = user
	return nil
}

func (m *mockUserRepo) GetUserByEmail(ctx context.Context, email string) (*entity.User, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	user, ok := m.users[email]
	if !ok {
		return nil, ErrNotFound
	}
	return user, nil
}

func (m *mockUserRepo) GetUserByID(ctx context.Context, id int) (*entity.User, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for _, user := range m.users {
		if user.ID == id {
			return user, nil
		}
	}
	return nil, ErrNotFound
}

func (m *mockUserRepo) UpdateUser(ctx context.Context, user *entity.User) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.users[user.Email] = user
	return nil
}

func (m *mockUserRepo) DecrementPriority(ctx context.Context, userID int) error {
	return nil
}

func (m *mockUserRepo) IncrementPriority(ctx context.Context, userID int) error {
	return nil
}

func (m *mockUserRepo) RecordSession(ctx context.Context, userID int, sessionDate time.Time) error {
	return nil
}

func (m *mockUserRepo) GetLastSessionDate(ctx context.Context, userID int) (*time.Time, error) {
	return nil, nil
}

func (m *mockUserRepo) LogPriorityTransaction(ctx context.Context, userID int, songID, songTitle, txType string, amount, balanceAfter int) error {
	return nil
}

func TestNewInteractor(t *testing.T) {
	repo := newMockUserRepo()
	clientID := "test-client-id"
	hostEmails := []string{"host@example.com"}
	adminEmails := []string{"admin@example.com"}

	clock := &mockClock{now: time.Now()}
	store := &mockSessionStore{sessions: make(map[string]int), token: "test-token"}
	interactor := NewInteractor(repo, clientID, hostEmails, adminEmails, store, clock)

	if interactor == nil {
		t.Fatal("expected non-nil interactor")
	}
	if interactor.clientID != clientID {
		t.Errorf("expected clientID %s, got %s", clientID, interactor.clientID)
	}
}

func TestIsHostEmail(t *testing.T) {
	repo := newMockUserRepo()
	clock := &mockClock{now: time.Now()}
	store := &mockSessionStore{sessions: make(map[string]int), token: "test-token"}
	interactor := NewInteractor(repo, "client-id", []string{"host@example.com", "Host2@Example.com"}, []string{}, store, clock)

	tests := []struct {
		email    string
		expected bool
	}{
		{"host@example.com", true},
		{"HOST@EXAMPLE.COM", true},
		{"host2@example.com", true},
		{"guest@example.com", false},
		{"admin@example.com", false},
	}

	for _, tt := range tests {
		result := interactor.isHostEmail(tt.email)
		if result != tt.expected {
			t.Errorf("isHostEmail(%s) = %v, expected %v", tt.email, result, tt.expected)
		}
	}
}

func TestIsAdminEmail(t *testing.T) {
	repo := newMockUserRepo()
	clock := &mockClock{now: time.Now()}
	store := &mockSessionStore{sessions: make(map[string]int), token: "test-token"}
	interactor := NewInteractor(repo, "client-id", []string{}, []string{"admin@example.com", "Admin2@Example.com"}, store, clock)

	tests := []struct {
		email    string
		expected bool
	}{
		{"admin@example.com", true},
		{"ADMIN@EXAMPLE.COM", true},
		{"admin2@example.com", true},
		{"guest@example.com", false},
		{"host@example.com", false},
	}

	for _, tt := range tests {
		result := interactor.isAdminEmail(tt.email)
		if result != tt.expected {
			t.Errorf("isAdminEmail(%s) = %v, expected %v", tt.email, result, tt.expected)
		}
	}
}

// Note: Testing LoginWithGoogle and VerifyGoogleToken would require mocking HTTP calls
// to Google's tokeninfo endpoint, which is beyond the scope of unit tests.
// These should be tested with integration tests or by mocking the HTTP client.
