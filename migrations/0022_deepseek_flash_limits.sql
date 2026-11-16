-- +goose Up
UPDATE models
SET context_window = 1048576,
    max_output = 393216,
    supports_vision = true
WHERE name IN ('deepseek-flash', 'deepseek-v4-flash', 'deepseek-v4-flash-vision-exp');

-- +goose Down
UPDATE models
SET context_window = 1000000,
    max_output = 384000
WHERE name IN ('deepseek-flash', 'deepseek-v4-flash', 'deepseek-v4-flash-vision-exp');
