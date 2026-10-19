-- +goose Up
CREATE TABLE control_meta (id integer PRIMARY KEY CHECK(id=1), payload jsonb NOT NULL);
INSERT INTO control_meta VALUES(1,'{"policy_revision":0}');
CREATE TABLE admins (id text PRIMARY KEY, payload jsonb NOT NULL, username text GENERATED ALWAYS AS (payload->>'username') STORED UNIQUE);
CREATE TABLE admin_sessions (id text PRIMARY KEY, payload jsonb NOT NULL, admin_id text GENERATED ALWAYS AS (payload->>'admin_id') STORED REFERENCES admins(id));
CREATE TABLE clients (id text PRIMARY KEY, payload jsonb NOT NULL, public_key text GENERATED ALWAYS AS (payload->>'public_key') STORED UNIQUE);
CREATE TABLE enrollments (id text PRIMARY KEY, payload jsonb NOT NULL, request_id text GENERATED ALWAYS AS (NULLIF(payload->>'request_id','')) STORED UNIQUE);
CREATE TABLE tunnels (
 id text PRIMARY KEY, payload jsonb NOT NULL,
 client_id text GENERATED ALWAYS AS (payload->>'client_id') STORED REFERENCES clients(id),
 protocol text GENERATED ALWAYS AS (payload->>'protocol') STORED CHECK(protocol IN ('tcp','udp')),
 target_ip inet GENERATED ALWAYS AS ((payload->>'target_ip')::inet) STORED CHECK(family(target_ip)=4),
 target_port integer GENERATED ALWAYS AS ((payload->>'target_port')::integer) STORED CHECK(target_port BETWEEN 1 AND 65535)
);
CREATE INDEX tunnels_client_idx ON tunnels(client_id);
CREATE TABLE credentials (id text PRIMARY KEY, payload jsonb NOT NULL, tunnel_id text GENERATED ALWAYS AS (payload->>'tunnel_id') STORED REFERENCES tunnels(id), username text GENERATED ALWAYS AS (payload->>'username') STORED UNIQUE);
CREATE INDEX credentials_tunnel_idx ON credentials(tunnel_id);
CREATE TABLE config_revisions (id text PRIMARY KEY, payload jsonb NOT NULL, client_id text GENERATED ALWAYS AS (payload->>'client_id') STORED REFERENCES clients(id), revision bigint GENERATED ALWAYS AS ((payload->>'revision')::bigint) STORED, UNIQUE(client_id,revision));
CREATE TABLE edge_policy_revisions (id text PRIMARY KEY, payload jsonb NOT NULL);
CREATE TABLE operations (id text PRIMARY KEY, payload jsonb NOT NULL, idempotency_key text GENERATED ALWAYS AS (NULLIF(payload->>'idempotency_key','')) STORED UNIQUE);
CREATE TABLE outbox_events (id text PRIMARY KEY REFERENCES operations(id), payload jsonb NOT NULL);
CREATE TABLE audit_events (id text PRIMARY KEY, payload jsonb NOT NULL);
CREATE TABLE runtime_snapshots (id text PRIMARY KEY, payload jsonb NOT NULL);

-- +goose Down
DROP TABLE runtime_snapshots, audit_events, outbox_events, operations, edge_policy_revisions, config_revisions, credentials, tunnels, enrollments, clients, admin_sessions, admins, control_meta;
