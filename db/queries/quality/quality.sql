-- name: ListQualityObservations :many
WITH latest AS (
    SELECT DISTINCT ON (series_id, observation_time)
           id, series_id, observation_time, value, value_text,
           system_known_at, quality_flags
    FROM observation_revisions
    WHERE series_id = sqlc.arg(series_id)::uuid
      AND observation_time >= sqlc.arg(from_time)::timestamptz
      AND observation_time < sqlc.arg(to_time)::timestamptz
      AND observation_time <= sqlc.arg(evaluation_cutoff)::timestamptz
      AND system_known_at <= sqlc.arg(evaluation_cutoff)::timestamptz
    ORDER BY series_id, observation_time, system_known_at DESC, id DESC
)
SELECT latest.id, latest.series_id, latest.observation_time,
       latest.system_known_at, latest.quality_flags,
       EXISTS (
           SELECT 1
           FROM observation_revisions AS prior
           WHERE prior.series_id = latest.series_id
             AND prior.observation_time = latest.observation_time
             AND prior.system_known_at <= sqlc.arg(evaluation_cutoff)::timestamptz
             AND (prior.value IS DISTINCT FROM latest.value
                  OR prior.value_text IS DISTINCT FROM latest.value_text)
       ) AS revised
FROM latest
ORDER BY latest.observation_time, latest.id
LIMIT 10001;
