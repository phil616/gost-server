-- name: GetControlMeta :one
SELECT payload FROM control_meta WHERE id = 1;

-- name: SetControlMeta :exec
UPDATE control_meta SET payload = sqlc.arg(payload)::jsonb WHERE id = 1;
