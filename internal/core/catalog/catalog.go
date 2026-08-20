package catalog

import (
	"go.uber.org/fx"

	"github.com/michelsazevedo/tuenti/internal/core/catalog/api"
	"github.com/michelsazevedo/tuenti/internal/core/catalog/application"
	"github.com/michelsazevedo/tuenti/internal/core/catalog/domain"
	"github.com/michelsazevedo/tuenti/internal/core/catalog/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

func Catalog() fx.Option {
	return fx.Module(
		"catalog",
		fx.Provide(
			func(pg *database.PgConn) domain.ItemRepository {
				return persistence.NewItemRepository(pg.Pool())
			},
			application.NewCreateItem,
			application.NewGetItem,
			application.NewListItems,
			application.NewUpdateItem,
			application.NewDeleteItem,
			fx.Annotate(api.NewItemHandler, fx.As(new(api.ItemHandler))),
		),
	)
}
