package auth

import (
	"errors"
	"local-music-queue/internal/domain/entity"
)

var (
	ErrInvalidPIN = errors.New("invalid PIN code")
)

// Interactor handles authentication logic.
type Interactor struct {
	clientPIN string
	hostPIN   string
}

// NewInteractor creates a new Auth Interactor.
func NewInteractor(clientPIN, hostPIN string) *Interactor {
	return &Interactor{
		clientPIN: clientPIN,
		hostPIN:   hostPIN,
	}
}

// Login verifies the PIN and returns the user entity with the appropriate role.
func (i *Interactor) Login(pin string, displayName string) (*entity.User, error) {
	var role entity.Role

	switch pin {
	case i.hostPIN:
		role = entity.RoleHost
	case i.clientPIN:
		role = entity.RoleGuest
	default:
		return nil, ErrInvalidPIN
	}

	if displayName == "" {
		displayName = "Anonymous"
	}

	return &entity.User{
		DisplayName: displayName,
		Role:        role,
	}, nil
}
