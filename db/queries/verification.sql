-- name: InsertVerification :one
INSERT INTO verifications (user_id, verification_type_id, verification_status_id, evidence_ref, notes)
SELECT $1, vt.id, vs.id, $3, $4
FROM verification_types vt, verification_statuses vs
WHERE vt.code = $2 AND vs.code = 'pending'
RETURNING id;

-- name: ListPendingVerifications :many
SELECT v.id, v.user_id, pr.display_name AS talent_display_name,
       vt.code AS type_code, vt.name AS type_name,
       v.evidence_ref, v.notes, v.created_at
FROM verifications v
JOIN verification_types vt ON vt.id = v.verification_type_id
JOIN verification_statuses vs ON vs.id = v.verification_status_id
JOIN profiles pr ON pr.user_id = v.user_id
WHERE vs.code = 'pending'
ORDER BY v.created_at ASC;

-- name: ListMyVerifications :many
SELECT v.id, vt.code AS type_code, vt.name AS type_name,
       vs.code AS status_code, vs.name AS status_name,
       v.evidence_ref, v.notes, v.review_notes, v.created_at
FROM verifications v
JOIN verification_types vt ON vt.id = v.verification_type_id
JOIN verification_statuses vs ON vs.id = v.verification_status_id
WHERE v.user_id = $1
ORDER BY v.created_at DESC;

-- name: GetVerificationForReview :one
SELECT v.id, v.user_id, vt.code AS type_code, vs.code AS status_code
FROM verifications v
JOIN verification_types vt ON vt.id = v.verification_type_id
JOIN verification_statuses vs ON vs.id = v.verification_status_id
WHERE v.id = $1;

-- name: ReviewVerification :execrows
-- Only ever moves a verification out of `pending`; re-reviewing an
-- already-decided one affects zero rows (caller treats that as an error).
UPDATE verifications v
SET verification_status_id = (SELECT id FROM verification_statuses WHERE code = sqlc.arg(new_status_code)::text),
    reviewed_by = $2,
    review_notes = $3,
    updated_at = now()
FROM verification_statuses cur
WHERE v.id = $1 AND v.verification_status_id = cur.id AND cur.code = 'pending';

-- name: GetVerificationByID :one
SELECT v.id, vt.code AS type_code, vt.name AS type_name,
       vs.code AS status_code, vs.name AS status_name,
       v.evidence_ref, v.notes, v.review_notes, v.created_at
FROM verifications v
JOIN verification_types vt ON vt.id = v.verification_type_id
JOIN verification_statuses vs ON vs.id = v.verification_status_id
WHERE v.id = $1;

-- name: SetTalentSearchableByUserID :execrows
UPDATE talent_profiles SET is_searchable = $2, updated_at = now() WHERE user_id = $1;
