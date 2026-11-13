-- S/MIME certificates: of the organisation (user_id NULL, for the senders of
-- the wiki) and personal ones (for e-mails sent in the name of a person).
CREATE TABLE plugin_smime_certs (
    id            bigserial PRIMARY KEY,
    user_id       bigint REFERENCES users(id) ON DELETE CASCADE,
    label         text NOT NULL DEFAULT '',
    emails        text[] NOT NULL,            -- lower-case addresses of the certificate
    subject       text NOT NULL,
    issuer        text NOT NULL,
    serial        text NOT NULL,
    fingerprint   text NOT NULL,              -- hex SHA-256 of the certificate
    not_before    timestamptz NOT NULL,
    not_after     timestamptz NOT NULL,
    trusted       boolean NOT NULL,           -- chain verified against the system roots at upload
    chain         text NOT NULL,              -- PEM: the certificate first, then the intermediates
    key           bytea NOT NULL,             -- PKCS#8 private key, sealed (secrets.mode)
    sign_mail     boolean NOT NULL DEFAULT true,
    seal          boolean NOT NULL DEFAULT false, -- organisation only: also seal signature evidence
    warned_days   int,                        -- smallest warning threshold already sent (days)
    expired_noted boolean NOT NULL DEFAULT false,
    created_by    bigint,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (user_id IS NULL OR NOT seal)
);
CREATE UNIQUE INDEX plugin_smime_certs_unique ON plugin_smime_certs(fingerprint, COALESCE(user_id, 0));
CREATE INDEX plugin_smime_certs_emails ON plugin_smime_certs USING gin(emails);
CREATE INDEX plugin_smime_certs_user ON plugin_smime_certs(user_id);
