package application

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"

	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

var (
	ErrMissingSigningSecret = errors.New("access token: signing secret is not configured")

	ErrIncompleteTokenClaims = errors.New("access token: subject and organization id are required")
)

const TokenAudience = "tuenti-api"

type AccessTokenClaims struct {
	jwt.RegisteredClaims

	OrganizationID string `json:"organization_id"`
}

func TokenIssuer(environment string) string {
	return "tuenti/" + environment
}

func issueAccessToken(
	ctx context.Context,
	memberships orgdomain.MembershipRepository,
	secret string,
	environment string,
	userID pgtype.UUID,
) (string, error) {
	membership, err := memberships.FindByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, orgdomain.ErrMembershipNotFound) {
			logger := observability.Logger(ctx)
			logger.Error().
				Str("event", "token_issuance_without_membership").
				Str("user_id", userID.String()).
				Msg("user has no organization membership, no access token can be issued")
		}

		return "", err
	}

	return newAccessToken(secret, userID.String(), membership.OrganizationId.String(), environment)
}

func newAccessToken(secret, subject, organizationID, environment string) (string, error) {
	if secret == "" {
		return "", ErrMissingSigningSecret
	}

	if subject == "" || organizationID == "" {
		return "", ErrIncompleteTokenClaims
	}

	now := time.Now()

	claims := AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    TokenIssuer(environment),
			Audience:  jwt.ClaimStrings{TokenAudience},
			Subject:   subject,
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		OrganizationID: organizationID,
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}
