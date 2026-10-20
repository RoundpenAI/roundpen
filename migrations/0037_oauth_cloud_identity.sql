-- Official GitHub relay identities use provider_id = 'github-cloud', which is
-- not a row in oauth_providers. Drop the FK so those links can be stored.
ALTER TABLE user_identities DROP CONSTRAINT IF EXISTS user_identities_provider_id_fkey;
