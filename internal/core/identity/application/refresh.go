package application

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
)

type RefreshUseCase interface {
	Refresh(ctx context.Context, refreshToken string) (*TokenPair, error)
}

type refresh struct {
	refreshStore domain.RefreshTokenStore
	memberships  orgdomain.MembershipRepository
	secret       string
	environment  string
}

func NewRefresh(
	refreshStore domain.RefreshTokenStore,
	memberships orgdomain.MembershipRepository,
	conf *config.Config,
) RefreshUseCase {
	return &refresh{
		refreshStore: refreshStore,
		memberships:  memberships,
		secret:       conf.Settings.Secret,
		environment:  conf.Settings.Environment,
	}
}

func (s *refresh) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	rotatedToken, userID, err := s.refreshStore.Rotate(ctx, refreshToken, refreshTokenTTL)
	if err != nil {
		return nil, err
	}

	var id pgtype.UUID
	if err := id.Scan(userID); err != nil {
		return nil, fmt.Errorf("refresh: stored subject %q is not a uuid: %w", userID, err)
	}

	accessToken, err := issueAccessToken(ctx, s.memberships, s.secret, s.environment, id)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: rotatedToken,
		ExpiresIn:    int64(accessTokenTTL.Seconds()),
	}, nil
}
