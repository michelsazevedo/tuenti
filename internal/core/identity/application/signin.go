package application

import (
	"context"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
)

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
}

type SigninUseCase interface {
	SignIn(ctx context.Context, email, password string) (*TokenPair, error)
}

type signin struct {
	users        domain.UserRepository
	memberships  orgdomain.MembershipRepository
	refreshStore domain.RefreshTokenStore
	secret       string
	environment  string
}

func NewSignin(
	users domain.UserRepository,
	memberships orgdomain.MembershipRepository,
	refreshStore domain.RefreshTokenStore,
	conf *config.Config,
) SigninUseCase {
	return &signin{
		users:        users,
		memberships:  memberships,
		refreshStore: refreshStore,
		secret:       conf.Settings.Secret,
		environment:  conf.Settings.Environment,
	}
}

func (s *signin) SignIn(ctx context.Context, email, password string) (*TokenPair, error) {
	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil, domain.ErrInvalidCredentials
		}

		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte(password)); err != nil {
		return nil, domain.ErrInvalidCredentials
	}

	accessToken, err := issueAccessToken(ctx, s.memberships, s.secret, s.environment, user.Id)
	if err != nil {
		if errors.Is(err, orgdomain.ErrMembershipNotFound) {
			return nil, domain.ErrInvalidCredentials
		}

		return nil, err
	}

	refreshToken, err := s.refreshStore.Save(ctx, user.Id.String(), refreshTokenTTL)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(accessTokenTTL.Seconds()),
	}, nil
}
