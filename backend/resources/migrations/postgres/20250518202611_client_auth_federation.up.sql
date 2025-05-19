ALTER TABLE oidc_clients ADD COLUMN client_auth_federation_enabled BOOLEAN DEFAULT FALSE NOT NULL;
ALTER TABLE oidc_clients ADD COLUMN client_auth_federation_issuer TEXT NOT NULL;
ALTER TABLE oidc_clients ADD COLUMN client_auth_federation_subject TEXT NOT NULL;
ALTER TABLE oidc_clients ADD COLUMN client_auth_federation_audience TEXT NOT NULL;
