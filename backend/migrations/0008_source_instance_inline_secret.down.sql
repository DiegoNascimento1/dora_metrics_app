ALTER TABLE platform.source_instance
  DROP COLUMN IF EXISTS auth_email,
  DROP COLUMN IF EXISTS secret_value;
