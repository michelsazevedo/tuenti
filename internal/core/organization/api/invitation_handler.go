package api

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/michelsazevedo/tuenti/internal/core/organization/application"
	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	m "github.com/michelsazevedo/tuenti/internal/http/middleware"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/observability"
)

type InvitationHandler interface {
	Create(c echo.Context) error
	List(c echo.Context) error
	Revoke(c echo.Context) error
	Accept(c echo.Context) error
}

type invitationHandler struct {
	createInvitation application.CreateInvitationUseCase
	acceptInvitation application.AcceptInvitationUseCase
	revokeInvitation application.RevokeInvitationUseCase
	listInvitations  application.ListInvitationsUseCase
}

func NewInvitationHandler(
	createInvitation application.CreateInvitationUseCase,
	acceptInvitation application.AcceptInvitationUseCase,
	revokeInvitation application.RevokeInvitationUseCase,
	listInvitations application.ListInvitationsUseCase,
) InvitationHandler {
	return &invitationHandler{
		createInvitation: createInvitation,
		acceptInvitation: acceptInvitation,
		revokeInvitation: revokeInvitation,
		listInvitations:  listInvitations,
	}
}

func (h *invitationHandler) Create(c echo.Context) error {
	userID, organizationID, err := h.authorizedOrganization(c)
	if err != nil {
		return err
	}

	req := new(CreateInvitationRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	invitation, err := h.createInvitation.CreateInvitation(
		c.Request().Context(), userID, organizationID, req.Email, req.Role,
	)
	if err != nil {
		if isInvitationAuthorizationFailure(err) {
			return echo.NewHTTPError(http.StatusForbidden, msgInvitationForbidden)
		}

		if isInvitationConflict(err) {
			return echo.NewHTTPError(http.StatusConflict, msgInvitationConflict)
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusCreated, NewInvitationResponse(invitation))
}

func (h *invitationHandler) List(c echo.Context) error {
	_, organizationID, err := h.authorizedOrganization(c)
	if err != nil {
		return err
	}

	invitations, err := h.listInvitations.ListInvitations(c.Request().Context(), organizationID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, NewInvitationResponses(invitations))
}

func (h *invitationHandler) Revoke(c echo.Context) error {
	userID, organizationID, err := h.authorizedOrganization(c)
	if err != nil {
		return err
	}

	var invitationID pgtype.UUID
	if err := invitationID.Scan(c.Param("id")); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid invitation id")
	}

	if err := h.revokeInvitation.RevokeInvitation(
		c.Request().Context(), userID, organizationID, invitationID,
	); err != nil {
		if errors.Is(err, domain.ErrInvitationNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, msgInvitationNotFound)
		}

		if isInvitationAuthorizationFailure(err) {
			return echo.NewHTTPError(http.StatusForbidden, msgInvitationForbidden)
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *invitationHandler) Accept(c echo.Context) error {
	req := new(AcceptInvitationRequest)

	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if err := req.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}

	membership, err := h.acceptInvitation.AcceptInvitation(c.Request().Context(), req.Token, req.Name, req.Password)
	if err != nil {
		if errors.Is(err, domain.ErrInvitationNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, msgInvitationNotFound)
		}

		if isAcceptInvitationTerminalFailure(err) {
			return echo.NewHTTPError(http.StatusGone, msgInvitationNoLongerUsable)
		}

		if errors.Is(err, domain.ErrInvitationSignupFieldsRequired) {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, msgInvitationSignupFieldsRequired)
		}

		if errors.Is(err, domain.ErrInvitationEmailAlreadyRegistered) {
			return echo.NewHTTPError(http.StatusConflict, msgInvitationEmailAlreadyRegistered)
		}

		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return c.JSON(http.StatusOK, NewMembershipResponse(membership))
}

// authorizedOrganization resolves the organization a request targets and the caller acting on it.
//
// RequireOrganizationPermission authorizes against the organization in the *token*, not the one in the URL,
// so without this comparison a manager of one organization could aim an authorized request at another
// organization's path. The check belongs here because only the handler knows the path parameter.
func (h *invitationHandler) authorizedOrganization(c echo.Context) (pgtype.UUID, pgtype.UUID, error) {
	var pathOrganizationID pgtype.UUID
	if err := pathOrganizationID.Scan(c.Param("organizationId")); err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, echo.NewHTTPError(http.StatusBadRequest, "invalid organization id")
	}

	logger := observability.Logger(c.Request().Context())

	identity, err := m.IdentityFromContext(c.Request().Context())
	if err != nil {
		logger.Error().Err(err).
			Str("event", "invitation_handler_missing_identity").
			Str("path", c.Path()).
			Msg("invitation handler ran without an authenticated user and organization, RequireAuth must run before it")

		return pgtype.UUID{}, pgtype.UUID{}, echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	if identity.CurrentOrganizationID != pathOrganizationID {
		logger.Warn().
			Str("event", "invitation_handler_organization_mismatch").
			Str("path", c.Path()).
			Msg("request blocked, the token's organization does not match the one in the path")

		return pgtype.UUID{}, pgtype.UUID{}, echo.NewHTTPError(http.StatusForbidden, msgOrganizationMismatch)
	}

	return identity.CurrentUserID, pathOrganizationID, nil
}

const (
	msgOrganizationMismatch = "organization mismatch"

	msgInvitationForbidden = "you do not have permission to manage invitations for this organization"
	msgInvitationNotFound  = "invitation not found"
	msgInvitationConflict  = "this email already has a membership or a pending invitation"

	msgInvitationNoLongerUsable         = "this invitation is no longer valid"
	msgInvitationSignupFieldsRequired   = "name and password are required to accept this invitation"
	msgInvitationEmailAlreadyRegistered = "this email already has an account, sign in before accepting the invitation"
)

// A caller with no membership and a caller whose role is too low collapse into the same response: telling
// them apart would let an outsider probe which organizations they are absent from.
var invitationAuthorizationFailures = []error{
	domain.ErrInvitationForbidden,
	domain.ErrMembershipNotFound,
}

func isInvitationAuthorizationFailure(err error) bool {
	for _, authzErr := range invitationAuthorizationFailures {
		if errors.Is(err, authzErr) {
			return true
		}
	}

	return false
}

var invitationConflicts = []error{
	domain.ErrAlreadyMember,
	domain.ErrDuplicateInvitation,
}

func isInvitationConflict(err error) bool {
	for _, conflictErr := range invitationConflicts {
		if errors.Is(err, conflictErr) {
			return true
		}
	}

	return false
}

// An invitation that expired, was revoked or was already consumed is gone for good: 410 tells the client to
// stop retrying and ask for a fresh invitation instead.
var acceptInvitationTerminalFailures = []error{
	domain.ErrInvitationExpired,
	domain.ErrInvitationRevoked,
	domain.ErrInvitationAlreadyAccepted,
}

func isAcceptInvitationTerminalFailure(err error) bool {
	for _, terminalErr := range acceptInvitationTerminalFailures {
		if errors.Is(err, terminalErr) {
			return true
		}
	}

	return false
}
