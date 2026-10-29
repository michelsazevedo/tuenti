package customer

import (
	"go.uber.org/fx"

	"github.com/michelsazevedo/tuenti/internal/core/customer/api"
	"github.com/michelsazevedo/tuenti/internal/core/customer/application"
	"github.com/michelsazevedo/tuenti/internal/core/customer/domain"
	"github.com/michelsazevedo/tuenti/internal/core/customer/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

func Customer() fx.Option {
	return fx.Module(
		"customer",
		fx.Provide(
			func(pg *database.PgConn) domain.CustomerRepository {
				return persistence.NewCustomerRepository(pg.Pool())
			},
			application.NewCreateCustomer,
			application.NewGetCustomer,
			application.NewListCustomers,
			application.NewUpdateCustomer,
			application.NewDeleteCustomer,
			fx.Annotate(api.NewCustomerHandler, fx.As(new(api.CustomerHandler))),
		),
	)
}
