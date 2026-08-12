package application

import (
	"context"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
	"github.com/michelsazevedo/tuenti/internal/core/identity/persistence"
	orgdomain "github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	orgrepository "github.com/michelsazevedo/tuenti/internal/core/organization/repository"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

type SignupUseCase interface {
	SignUp(ctx context.Context, user *domain.User, organizationName string) error
}

type signup struct {
	uow *database.UnitOfWork
}

func NewSignup(uow *database.UnitOfWork) SignupUseCase {
	return &signup{uow: uow}
}

func (s *signup) SignUp(ctx context.Context, user *domain.User, organizationName string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.PasswordDigest = string(hash)
	user.Password = ""

	return s.uow.Do(ctx, func(tx pgx.Tx) error {
		if err := persistence.NewUserRepository(tx).Create(ctx, user); err != nil {
			return err
		}

		org := &orgdomain.Organization{Name: organizationName}
		if err := orgrepository.NewOrganizationRepository(tx).Create(ctx, org); err != nil {
			return err
		}

		membership := &orgdomain.Membership{
			OrganizationId: org.Id,
			UserId:         user.Id,
			Role:           orgdomain.RoleOwner,
		}

		return orgrepository.NewMembershipRepository(tx).Create(ctx, membership)
	})
}
