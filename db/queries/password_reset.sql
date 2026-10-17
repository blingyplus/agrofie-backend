-- name: GetPasswordResetRequest :one
SELECT email, flow_id, csrf_token, cookies, attempts, settings_flow_id, verified_code_hash,
       requested_at, window_started_at, window_count
FROM password_reset_requests
WHERE email = sqlc.arg(email)::text;

-- name: UpsertPasswordResetRequest :exec
INSERT INTO password_reset_requests (email, flow_id, csrf_token, cookies, attempts, settings_flow_id, verified_code_hash,
                                     requested_at, window_started_at, window_count)
VALUES (sqlc.arg(email)::text, sqlc.arg(flow_id)::text, sqlc.arg(csrf_token)::text, sqlc.arg(cookies)::jsonb, 0, NULL, NULL,
        now(), sqlc.arg(window_started_at)::timestamptz, sqlc.arg(window_count)::int)
ON CONFLICT (email) DO UPDATE SET
    flow_id = EXCLUDED.flow_id, csrf_token = EXCLUDED.csrf_token, cookies = EXCLUDED.cookies,
    attempts = 0, settings_flow_id = NULL, verified_code_hash = NULL,
    requested_at = now(), window_started_at = EXCLUDED.window_started_at, window_count = EXCLUDED.window_count;

-- name: BumpPasswordResetAttempts :one
UPDATE password_reset_requests SET attempts = attempts + 1 WHERE email = sqlc.arg(email)::text RETURNING attempts;

-- name: MarkPasswordResetVerified :exec
UPDATE password_reset_requests
SET settings_flow_id = sqlc.arg(settings_flow_id)::text, verified_code_hash = sqlc.arg(verified_code_hash)::text,
    cookies = sqlc.arg(cookies)::jsonb
WHERE email = sqlc.arg(email)::text;

-- name: DeletePasswordResetRequest :exec
DELETE FROM password_reset_requests WHERE email = sqlc.arg(email)::text;
