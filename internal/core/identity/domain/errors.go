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
)
