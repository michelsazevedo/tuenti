package domain

import "errors"

var (
	ErrOrganizationNotFound          = errors.New("organization not found")
	ErrMembershipAlreadyExists       = errors.New("membership already exists")
	ErrMembershipNotFound            = errors.New("membership not found")
	ErrInvalidSubscriptionTransition = errors.New("invalid subscription transition")
)
