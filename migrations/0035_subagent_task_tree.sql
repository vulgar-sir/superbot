-- +goose Up
ALTER TABLE subagent_tasks
    ADD COLUMN parent_task_id uuid REFERENCES subagent_tasks(id) ON DELETE CASCADE;
ALTER TABLE subagent_tasks
    ADD COLUMN depth integer NOT NULL DEFAULT 0 CHECK (depth >= 0 AND depth <= 2);
CREATE INDEX subagent_tasks_parent_task_idx ON subagent_tasks(parent_task_id);
CREATE INDEX subagent_tasks_parent_conversation_idx ON subagent_tasks(parent_conversation_id);

-- +goose Down
DROP INDEX IF EXISTS subagent_tasks_parent_conversation_idx;
DROP INDEX IF EXISTS subagent_tasks_parent_task_idx;
ALTER TABLE subagent_tasks DROP COLUMN IF EXISTS depth;
ALTER TABLE subagent_tasks DROP COLUMN IF EXISTS parent_task_id;
