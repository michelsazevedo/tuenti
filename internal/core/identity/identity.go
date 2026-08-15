package identity

import (
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/fx"

	"github.com/michelsazevedo/tuenti/internal/core/identity/api"
	"github.com/michelsazevedo/tuenti/internal/core/identity/application"
	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

func Identity() fx.Option {
	return fx.Module(
		"identity",
		fx.Provide(
			func(pg *database.PgConn) domain.UserRepository {
				return persistence.NewUserRepository(pg.Pool())
			},
			func(client *goredis.Client) domain.RefreshTokenStore {
				return persistence.NewRefreshTokenStore(client)
			},
			func(pg *database.PgConn) domain.PasswordResetTokenRepository {
				return persistence.NewPasswordResetTokenRepository(pg.Pool())
			},
			func(pg *database.PgConn) domain.EmailConfirmationTokenRepository {
				return persistence.NewEmailConfirmationTokenRepository(pg.Pool())
			},
			application.NewSignup,
			application.NewSignin,
			application.NewRefresh,
			application.NewLogout,
			application.NewRequestPasswordReset,
			application.NewConfirmPasswordReset,
			application.NewConfirmEmail,
			application.NewResendConfirmation,
			fx.Annotate(api.NewAuthzHandler, fx.As(new(api.AuthzHandler))),
		),
	)
}
