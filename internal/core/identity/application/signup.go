package application

import (
	"context"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

type SignupUseCase interface {
	SignUp(ctx context.Context, user *domain.User) error
}

type signup struct {
	uow *database.UnitOfWork
}

func NewSignup(uow *database.UnitOfWork) SignupUseCase {
	return &signup{uow: uow}
}

func (s *signup) SignUp(ctx context.Context, user *domain.User) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.PasswordDigest = string(hash)
	user.Password = ""

	return s.uow.Do(ctx, func(tx pgx.Tx) error {
		return persistence.NewUserRepository(tx).Create(ctx, user)
	})
}
