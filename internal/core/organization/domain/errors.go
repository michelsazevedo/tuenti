package domain

import "errors"

var (
	ErrOrganizationNotFound          = errors.New("organization not found")
	ErrIndustryNotFound              = errors.New("industry not found")
	ErrMembershipAlreadyExists       = errors.New("membership already exists")
	ErrMembershipNotFound            = errors.New("membership not found")
	ErrInvalidSubscriptionTransition = errors.New("invalid subscription transition")

	ErrInvitationNotFound               = errors.New("invitation not found")
	ErrInvitationExpired                = errors.New("invitation expired")
	ErrInvitationRevoked                = errors.New("invitation revoked")
	ErrInvitationAlreadyAccepted        = errors.New("invitation already accepted")
	ErrDuplicateInvitation              = errors.New("duplicate invitation")
	ErrAlreadyMember                    = errors.New("already member")
	ErrInvitationForbidden              = errors.New("invitation forbidden")
	ErrInvitationEmailAlreadyRegistered = errors.New("invitation email already registered")
	ErrInvitationSignupFieldsRequired   = errors.New("invitation signup fields required")
)
