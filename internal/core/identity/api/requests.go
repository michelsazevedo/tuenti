package api

import (
	"github.com/go-playground/validator/v10"

	"github.com/michelsazevedo/tuenti/internal/core/identity/domain"
)

var validate = validator.New()

type SignupRequest struct {
	Name             string `json:"name" validate:"required"`
	Email            string `json:"email" validate:"required,email"`
	Password         string `json:"password" validate:"required,min=8"`
	OrganizationName string `json:"organization_name" validate:"required"`
}

func (r *SignupRequest) Validate() error {
	return validate.Struct(r)
}

func (r *SignupRequest) ToDomain() *domain.User {
	return &domain.User{
		Name:     r.Name,
		Email:    r.Email,
		Password: r.Password,
	}
}

type SigninRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

func (r *SigninRequest) Validate() error {
	return validate.Struct(r)
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

func (r *RefreshRequest) Validate() error {
	return validate.Struct(r)
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

func (r *LogoutRequest) Validate() error {
	return validate.Struct(r)
}

type RequestPasswordResetRequest struct {
	Email string `json:"email" validate:"required,email"`
}

func (r *RequestPasswordResetRequest) Validate() error {
	return validate.Struct(r)
}

type ConfirmPasswordResetRequest struct {
	Token       string `json:"token" validate:"required"`
	NewPassword string `json:"new_password" validate:"required,min=8"`
}

func (r *ConfirmPasswordResetRequest) Validate() error {
	return validate.Struct(r)
}

type ResendConfirmationRequest struct {
	Email string `json:"email" validate:"required,email"`
}

func (r *ResendConfirmationRequest) Validate() error {
	return validate.Struct(r)
}
