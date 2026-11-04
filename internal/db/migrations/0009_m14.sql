-- one node of a cluster restarts at a time (rolling restart after plugin builds)
CREATE TABLE cluster_lease (
	name   text PRIMARY KEY,
	holder text NOT NULL,
	until  timestamptz NOT NULL
);
