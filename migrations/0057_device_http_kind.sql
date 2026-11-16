-- +goose Up
ALTER TABLE device_jobs DROP CONSTRAINT IF EXISTS device_jobs_kind_check;
ALTER TABLE device_jobs
    ADD CONSTRAINT device_jobs_kind_check
    CHECK (kind IN (
        'shell', 'screenshot', 'clipboard_read', 'clipboard_write',
        'fs_read', 'fs_write', 'app_list', 'app_open',
        'location', 'audio_list', 'http_request'
    ));

-- +goose Down
ALTER TABLE device_jobs DROP CONSTRAINT IF EXISTS device_jobs_kind_check;
ALTER TABLE device_jobs
    ADD CONSTRAINT device_jobs_kind_check
    CHECK (kind IN (
        'shell', 'screenshot', 'clipboard_read', 'clipboard_write',
        'fs_read', 'fs_write', 'app_list', 'app_open', 'location', 'audio_list'
    ));
