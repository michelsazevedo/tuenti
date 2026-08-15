-- expired trial scan index
DROP INDEX IF EXISTS organizations_subscription_status_trial_ends_at_idx;

-- organizations trial & subscription columns
ALTER TABLE organizations
    DROP CONSTRAINT IF EXISTS organizations_subscription_status_check,
    DROP COLUMN IF EXISTS plan_id,
    DROP COLUMN IF EXISTS subscription_status,
    DROP COLUMN IF EXISTS trial_ends_at,
    DROP COLUMN IF EXISTS trial_starts_at;
