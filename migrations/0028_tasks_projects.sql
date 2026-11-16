-- +goose Up
CREATE TABLE projects (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tasks (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id        uuid REFERENCES projects(id) ON DELETE SET NULL,
    title             text NOT NULL,
    description       text NOT NULL DEFAULT '',
    status            text NOT NULL DEFAULT 'todo'
                      CHECK (status IN ('todo', 'in_progress', 'blocked', 'done', 'canceled')),
    priority          integer NOT NULL DEFAULT 0,
    assignee_bot_id   uuid REFERENCES bots(id) ON DELETE SET NULL,
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    due_at            timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX tasks_owner_status_idx ON tasks(owner_id, status, priority DESC, created_at DESC);
CREATE INDEX tasks_assignee_idx ON tasks(assignee_bot_id);

-- +goose Down
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS projects;
