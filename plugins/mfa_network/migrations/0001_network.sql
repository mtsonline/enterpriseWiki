-- IP addresses and networks (CIDR, normalized) a user may sign in from.
CREATE TABLE plugin_mfa_network (
    user_id  bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    networks text[] NOT NULL CHECK (cardinality(networks) BETWEEN 1 AND 50)
);
