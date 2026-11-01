-- Key pair of the SAML service provider (generated on first use).
CREATE TABLE plugin_shib_sp (
    id         int PRIMARY KEY CHECK (id = 1),
    key_pem    text NOT NULL,
    cert_pem   text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
