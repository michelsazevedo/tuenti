package resend

import (
	resendgo "github.com/resend/resend-go/v2"
	"go.uber.org/fx"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
)

func NewClient(conf *config.Config) *resendgo.Client {
	return resendgo.NewClient(conf.Resend.APIKey)
}

func Resend() fx.Option {
	return fx.Module(
		"resend",
		fx.Provide(
			NewClient,
			func(client *resendgo.Client, conf *config.Config) domain.PasswordResetMailer {
				return NewMailer(client, conf.Resend.FromEmail)
			},
			func(client *resendgo.Client, conf *config.Config) domain.ConfirmationMailer {
				return NewMailer(client, conf.Resend.FromEmail)
			},
		),
	)
}
