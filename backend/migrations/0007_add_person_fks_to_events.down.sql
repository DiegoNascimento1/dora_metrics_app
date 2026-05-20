DROP INDEX IF EXISTS platform.deployment_triggerer_person_idx;
DROP INDEX IF EXISTS platform.merge_request_author_person_idx;
ALTER TABLE platform.deployment    DROP COLUMN IF EXISTS triggerer_person_id;
ALTER TABLE platform.merge_request DROP COLUMN IF EXISTS author_person_id;
