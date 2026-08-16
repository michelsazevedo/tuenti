package persistence

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/michelsazevedo/tuenti/internal/core/organization/domain"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

const createOrganization = `INSERT INTO organizations(name, trial_starts_at, trial_ends_at, subscription_status, plan_id)
VALUES($1, $2, $3, $4, $5) RETURNING id`

const findOrganizationByID = `SELECT id, name, created_at, updated_at, trial_starts_at, trial_ends_at, subscription_status, plan_id
FROM organizations WHERE id = $1`

const findExpiredTrials = `SELECT id, name, created_at, updated_at, trial_starts_at, trial_ends_at, subscription_status, plan_id
FROM organizations WHERE subscription_status = $1 AND trial_ends_at < $2 ORDER BY trial_ends_at LIMIT $3`

const updateOrganizationSubscriptionStatus = `UPDATE organizations SET subscription_status = $1, updated_at = now() WHERE id = $2`

type OrganizationRepository struct {
	db database.DBTX
}

func NewOrganizationRepository(db database.DBTX) *OrganizationRepository {
	return &OrganizationRepository{db: db}
}

func (r *OrganizationRepository) Create(ctx context.Context, org *domain.Organization) error {
	return r.db.QueryRow(ctx, createOrganization,
		org.Name, org.TrialStartsAt, org.TrialEndsAt, org.SubscriptionStatus, org.PlanID,
	).Scan(&org.Id)
}

func (r *OrganizationRepository) FindByID(ctx context.Context, id pgtype.UUID) (*domain.Organization, error) {
	org := &domain.Organization{}

	err := r.db.QueryRow(ctx, findOrganizationByID, id).Scan(
		&org.Id, &org.Name, &org.CreatedAt, &org.UpdatedAt,
		&org.TrialStartsAt, &org.TrialEndsAt, &org.SubscriptionStatus, &org.PlanID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrOrganizationNotFound
		}

		return nil, err
	}

	return org, nil
}

func (r *OrganizationRepository) FindExpiredTrials(ctx context.Context, now time.Time, limit int) ([]*domain.Organization, error) {
	const defaultExpiredTrialsBatchSize = 500

	if limit <= 0 {
		limit = defaultExpiredTrialsBatchSize
	}

	rows, err := r.db.Query(ctx, findExpiredTrials, domain.Trialing, now, limit)
	if err != nil {
		return nil, err
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.Organization, error) {
		org := &domain.Organization{}

		if err := row.Scan(
			&org.Id, &org.Name, &org.CreatedAt, &org.UpdatedAt,
			&org.TrialStartsAt, &org.TrialEndsAt, &org.SubscriptionStatus, &org.PlanID,
		); err != nil {
			return nil, err
		}

		return org, nil
	})
}

func (r *OrganizationRepository) UpdateSubscriptionStatus(ctx context.Context, id pgtype.UUID, status domain.SubscriptionStatus) error {
	tag, err := r.db.Exec(ctx, updateOrganizationSubscriptionStatus, status, id)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrOrganizationNotFound
	}

	return nil
}
