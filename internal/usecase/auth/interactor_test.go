package auth

import (
	"local-music-queue/internal/domain/entity"
	"testing"
)

func TestLogin_HostPIN(t *testing.T) {
	interactor := NewInteractor("1234", "5678", "9999")

	user, err := interactor.Login("5678", "HostUser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Role != entity.RoleHost {
		t.Errorf("expected role %s, got %s", entity.RoleHost, user.Role)
	}
	if user.DisplayName != "HostUser" {
		t.Errorf("expected display name 'HostUser', got '%s'", user.DisplayName)
	}
}

func TestLogin_ClientPIN(t *testing.T) {
	interactor := NewInteractor("1234", "5678", "9999")

	user, err := interactor.Login("1234", "GuestUser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Role != entity.RoleGuest {
		t.Errorf("expected role %s, got %s", entity.RoleGuest, user.Role)
	}
}

func TestLogin_InvalidPIN(t *testing.T) {
	interactor := NewInteractor("1234", "5678", "9999")

	_, err := interactor.Login("0000", "User")
	if err != ErrInvalidPIN {
		t.Errorf("expected ErrInvalidPIN, got %v", err)
	}
}

func TestLogin_EmptyDisplayName(t *testing.T) {
	interactor := NewInteractor("1234", "5678", "9999")

	user, err := interactor.Login("1234", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.DisplayName != "Anonymous" {
		t.Errorf("expected 'Anonymous', got '%s'", user.DisplayName)
	}
}

func TestLogin_AdminPIN(t *testing.T) {
	interactor := NewInteractor("1234", "5678", "9999")

	user, err := interactor.Login("9999", "AdminUser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Role != entity.RoleAdmin {
		t.Errorf("expected role %s, got %s", entity.RoleAdmin, user.Role)
	}
	if user.DisplayName != "AdminUser" {
		t.Errorf("expected display name 'AdminUser', got '%s'", user.DisplayName)
	}
}
