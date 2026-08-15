-- organizations trial & subscription columns
ALTER TABLE organizations
    ADD COLUMN trial_starts_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN trial_ends_at         TIMESTAMPTZ NOT NULL DEFAULT now() + interval '14 days',
    ADD COLUMN subscription_status   TEXT NOT NULL DEFAULT 'trialing',
    ADD COLUMN plan_id               UUID,
    ADD CONSTRAINT organizations_subscription_status_check
        CHECK (subscription_status IN ('trialing', 'active', 'past_due', 'suspended', 'canceled'));

-- expired trial scan index
CREATE INDEX organizations_subscription_status_trial_ends_at_idx ON organizations(subscription_status, trial_ends_at);
