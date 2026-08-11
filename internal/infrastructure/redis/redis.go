package redis

import (
	"context"

	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"go.uber.org/fx"

	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
)

func NewClient(conf *config.Config) *goredis.Client {
	return goredis.NewClient(&goredis.Options{
		Addr:     conf.GetRedisAddr(),
		Password: conf.Redis.Password,
		DB:       conf.Redis.DB,
	})
}

func Redis() fx.Option {
	return fx.Module(
		"redis",
		fx.Provide(NewClient),
		fx.Invoke(
			func(lc fx.Lifecycle, client *goredis.Client) {
				lc.Append(fx.Hook{
					OnStart: func(ctx context.Context) error {
						return client.Ping(ctx).Err()
					},
					OnStop: func(ctx context.Context) error {
						log.Info().Msg("Shutting Down Redis connection")
						return client.Close()
					},
				})
			},
		),
	)
}
