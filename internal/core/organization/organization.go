package organization

import (
	"go.uber.org/fx"

	"github.com/michelsazevedo/tuenti/internal/core/organization/api"
	"github.com/michelsazevedo/tuenti/internal/core/organization/application"
	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/core/organization/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/kafka"
)

func Organization() fx.Option {
	return fx.Module(
		"organization",
		fx.Provide(
			func(pg *database.PgConn) domain.OrganizationRepository {
				return persistence.NewOrganizationRepository(pg.Pool())
			},
			func(pg *database.PgConn) domain.IndustryRepository {
				return persistence.NewIndustryRepository(pg.Pool())
			},
			func(pg *database.PgConn) domain.MembershipRepository {
				return persistence.NewMembershipRepository(pg.Pool())
			},
			func(pg *database.PgConn) domain.InvitationRepository {
				return persistence.NewInvitationRepository(pg.Pool())
			},
			func(producer *kafka.Producer) domain.InvitationEventPublisher {
				return persistence.NewEventPublisher(producer)
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
