-- Opt-in email digest of inbox entries. One digest setting per account; each
-- alert chooses whether its entries are included.
ALTER TABLE account_notification_rules ADD COLUMN email boolean NOT NULL DEFAULT false;

CREATE TABLE account_notification_digests (
    account_id bigint PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    frequency text NOT NULL CHECK (frequency IN ('off', 'daily', 'weekly')),
    last_notification_id bigint NOT NULL DEFAULT 0 CHECK (last_notification_id >= 0),
    next_due_at timestamptz,
    last_sent_at timestamptz,
    updated_at timestamptz NOT NULL,
    CHECK ((frequency = 'off') = (next_due_at IS NULL))
);
CREATE INDEX account_notification_digests_due_idx ON account_notification_digests (next_due_at) WHERE next_due_at IS NOT NULL;

ALTER TABLE account_mail_outbox DROP CONSTRAINT account_mail_outbox_purpose_check;
ALTER TABLE account_mail_outbox ADD CONSTRAINT account_mail_outbox_purpose_check CHECK (
    purpose IN ('verification', 'password_reset', 'email_change', 'email_step_up', 'security_notification', 'notification_digest')
);
