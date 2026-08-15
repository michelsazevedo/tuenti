package api

import (
	"github.com/go-playground/validator/v10"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

var validate = validator.New()

type CreateInvitationRequest struct {
	Email string      `json:"email" validate:"required,email"`
	Role  domain.Role `json:"role" validate:"required,oneof=manager admin member"`
}

func (r *CreateInvitationRequest) Validate() error {
	return validate.Struct(r)
}

type AcceptInvitationRequest struct {
	Token    string  `json:"token" validate:"required"`
	Name     *string `json:"name" validate:"omitempty,min=1"`
	Password *string `json:"password" validate:"omitempty,min=8"`
}

func (r *AcceptInvitationRequest) Validate() error {
	return validate.Struct(r)
}
