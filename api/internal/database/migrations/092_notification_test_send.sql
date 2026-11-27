-- User-requested test notifications: a token-less outbox purpose and its own
-- per-account quota.
ALTER TABLE account_mail_outbox DROP CONSTRAINT account_mail_outbox_purpose_check;
ALTER TABLE account_mail_outbox ADD CONSTRAINT account_mail_outbox_purpose_check CHECK (
    purpose IN ('verification', 'password_reset', 'email_change', 'email_step_up', 'security_notification', 'notification_digest', 'notification_test')
);

ALTER TABLE account_rate_limits DROP CONSTRAINT account_rate_limits_purpose_check;
ALTER TABLE account_rate_limits ADD CONSTRAINT account_rate_limits_purpose_check CHECK (
    purpose IN ('login', 'verification_send', 'reset_send', 'step_up', 'google_start', 'token_confirm', 'username', 'email_change', 'avatar_write', 'avatar_import', 'watchlist_write', 'watchlist_search', 'watchlist_import', 'watchlist_release_fetch', 'notification_test')
);
