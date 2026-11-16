-- +goose Up
-- +goose StatementBegin
ALTER TABLE skills ADD COLUMN IF NOT EXISTS description_en text NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE skills DROP COLUMN IF EXISTS description_en;
-- +goose StatementEnd
