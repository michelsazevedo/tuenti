package organization

import (
	"go.uber.org/fx"

	"github.com/michelsazevedo/tuenti/internal/core/organization/api"
	"github.com/michelsazevedo/tuenti/internal/core/organization/application"
	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/core/organization/repository"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

func Organization() fx.Option {
	return fx.Module(
		"organization",
		fx.Provide(
			func(pg *database.PgConn) domain.OrganizationRepository {
				return repository.NewOrganizationRepository(pg.Pool())
			},
			func(pg *database.PgConn) domain.MembershipRepository {
				return repository.NewMembershipRepository(pg.Pool())
			},
			func(pg *database.PgConn) domain.InvitationRepository {
				return repository.NewInvitationRepository(pg.Pool())
			},
			application.NewGetOrganizationByID,
			application.NewMembershipAuthorizationService,
			application.NewSuspendExpiredTrials,
			application.NewCreateInvitation,
			application.NewAcceptInvitation,
			application.NewRevokeInvitation,
			application.NewListInvitations,
			fx.Annotate(api.NewOrganizationHandler, fx.As(new(api.OrganizationHandler))),
			fx.Annotate(api.NewInvitationHandler, fx.As(new(api.InvitationHandler))),
		),
	)
}
