package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michelsazevedo/tuenti/internal/core/organization/application"
	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
)

const organizationId = "0f8fad5b-d9cb-469f-a165-708677289501"

type fakeGetOrganizationByIDUseCase struct {
	org *domain.Organization
	err error

	calls     int
	requested pgtype.UUID
}

func (f *fakeGetOrganizationByIDUseCase) GetByID(_ context.Context, id pgtype.UUID) (*domain.Organization, error) {
	f.calls++
	f.requested = id

	if f.err != nil {
		return nil, f.err
	}

	return f.org, nil
}

func mustUUID(t *testing.T, raw string) pgtype.UUID {
	t.Helper()

	var id pgtype.UUID
	require.NoError(t, id.Scan(raw))

	return id
}

func callGetByID(t *testing.T, usecase application.GetOrganizationByIDUseCase, id string) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/organizations/lookup", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/organizations/:id")
	c.SetParamNames("id")
	c.SetParamValues(id)

	handler := NewOrganizationHandler(usecase)

	if err := handler.GetByID(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}

	return rec
}

func TestGetOrganizationByIDReturnsTheOrganization(t *testing.T) {
	usecase := &fakeGetOrganizationByIDUseCase{
		org: &domain.Organization{
			Id:        mustUUID(t, organizationId),
			Name:      "Tuenti",
			CreatedAt: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
			UpdatedAt: time.Date(2026, time.March, 9, 10, 11, 12, 0, time.UTC),
		},
	}

	rec := callGetByID(t, usecase, organizationId)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{
		"id": "0f8fad5b-d9cb-469f-a165-708677289501",
		"name": "Tuenti",
		"created_at": "2026-01-02T03:04:05Z"
	}`, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "updated_at", "the response must expose only the OrganizationResponse fields")

	assert.Equal(t, 1, usecase.calls)
	assert.Equal(t, mustUUID(t, organizationId), usecase.requested, "the path id must reach the use case verbatim")
}

func TestGetOrganizationByIDRejectsMalformedIds(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "not a uuid at all", id: "not-a-uuid"},
		{name: "empty id", id: ""},
		{name: "truncated uuid", id: "0f8fad5b-d9cb-469f-a165-70867728950"},
		{name: "overlong uuid", id: organizationId + "-0f8fad5b"},
		{name: "non hex characters", id: "0f8fad5bd9cb469fa1657086772895zz"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeGetOrganizationByIDUseCase{}

			rec := callGetByID(t, usecase, test.id)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, `{"message":"invalid organization id"}`, rec.Body.String())
			assert.Zero(t, usecase.calls, "a rejected id must never reach the use case")
		})
	}
}

func TestGetOrganizationByIDForwardsEveryParsableId(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want pgtype.UUID
	}{
		{name: "canonical", id: organizationId, want: mustUUID(t, organizationId)},
		{name: "upper cased", id: "0F8FAD5B-D9CB-469F-A165-708677289501", want: mustUUID(t, organizationId)},
		{name: "dashes stripped", id: "0f8fad5bd9cb469fa165708677289501", want: mustUUID(t, organizationId)},
		{name: "non canonical separators", id: "0f8fad5b_d9cb_469f_a165_708677289501", want: mustUUID(t, organizationId)},
		{name: "nil uuid", id: "00000000-0000-0000-0000-000000000000", want: mustUUID(t, "00000000-0000-0000-0000-000000000000")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usecase := &fakeGetOrganizationByIDUseCase{err: domain.ErrOrganizationNotFound}

			rec := callGetByID(t, usecase, test.id)

			require.Equal(t, http.StatusNotFound, rec.Code)
			require.Equal(t, 1, usecase.calls)
			assert.Equal(t, test.want, usecase.requested)
		})
	}
}

func TestGetOrganizationByIDMapsNotFoundTo404(t *testing.T) {
	usecase := &fakeGetOrganizationByIDUseCase{
		err: fmt.Errorf("organization repository: %w", domain.ErrOrganizationNotFound),
	}

	rec := callGetByID(t, usecase, organizationId)

	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"message":"organization not found"}`, rec.Body.String())
	assert.Equal(t, 1, usecase.calls)
}

func TestGetOrganizationByIDMapsInfrastructureFailuresTo500(t *testing.T) {
	usecase := &fakeGetOrganizationByIDUseCase{err: errors.New("db down: dial tcp 10.0.0.7:5432")}

	rec := callGetByID(t, usecase, organizationId)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"message":"internal server error"}`, rec.Body.String())

	body := rec.Body.String()
	assert.NotContains(t, body, "db down", "infrastructure detail must not reach the client")
	assert.NotContains(t, body, "10.0.0.7")
}
