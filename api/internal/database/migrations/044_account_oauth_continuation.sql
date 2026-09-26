-- Recoverable, nonsecret target for the private post-OAuth continuation DTO.
CREATE SEQUENCE account_authority_events;
ALTER TABLE accounts ADD COLUMN authority_event bigint NOT NULL DEFAULT nextval('account_authority_events');
ALTER TABLE account_oauth_flows ADD COLUMN authority_event bigint NOT NULL DEFAULT nextval('account_authority_events');
ALTER TABLE account_oauth_flows ADD COLUMN target_email text
    CHECK (octet_length(target_email) BETWEEN 3 AND 254 AND target_email = lower(target_email COLLATE "C") AND target_email = btrim(target_email));
ALTER TABLE account_oauth_flows ADD COLUMN claimed_at timestamptz
    CHECK (claimed_at IS NULL OR (claimed_at >= created_at AND claimed_at < expires_at));
-- A legacy email-change flow has no recoverable target. Revoke rather than infer it.
DELETE FROM account_oauth_flows WHERE action = 'email_change';
ALTER TABLE account_oauth_flows ADD CONSTRAINT account_oauth_target_check
    CHECK ((action = 'email_change' AND target_email IS NOT NULL) OR (action IS DISTINCT FROM 'email_change' AND target_email IS NULL));
