-- Freeze the app configuration: encode every row of the standalone config table
-- as a single JSON object (mapping key -> value) and store it in the "kv" table
-- under the "config_migrated" key. The config is then managed by an actor rather
-- than living in its own table.
--
-- json_object_agg aggregates all rows into a JSON object. When the table is empty
-- the aggregate returns NULL, so COALESCE falls back to an empty object.
INSERT INTO kv ("key", "value")
SELECT 'config_migrated', COALESCE(json_object_agg("key", "value"), '{}'::json)::text
FROM app_config_variables;

-- Drop the now-frozen standalone config table
DROP TABLE app_config_variables;
