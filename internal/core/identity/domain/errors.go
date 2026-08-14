package domain

import "errors"

var (
	ErrUserAlreadyExists  = errors.New("user already exists")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid credentials")

	ErrRefreshTokenInvalid = errors.New("invalid refresh token")
	ErrRefreshTokenExpired = errors.New("refresh token expired")
	ErrRefreshTokenRevoked = errors.New("refresh token revoked")
	ErrRefreshTokenReused  = errors.New("refresh token reused")

	ErrPasswordResetTokenInvalid = errors.New("invalid password reset token")
	ErrPasswordResetTokenExpired = errors.New("password reset token expired")
	ErrPasswordResetTokenUsed    = errors.New("password reset token already used")

	ErrEmailConfirmationTokenInvalid = errors.New("invalid email confirmation token")
	ErrEmailConfirmationTokenExpired = errors.New("email confirmation token expired")
	ErrEmailConfirmationTokenUsed    = errors.New("email confirmation token already used")
)
