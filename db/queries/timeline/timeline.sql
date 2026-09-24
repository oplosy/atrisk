-- name: ListTimelineSeries :many
SELECT s.id, s.dataset_id, s.source_code, s.name, s.unit, s.frequency,
       s.seasonal_adjustment, s.source_timezone, s.freshness_policy, s.created_at,
       ds.code AS data_source_code, ds.name AS data_source_name,
       d.external_key AS dataset_external_key,
       EXISTS (SELECT 1 FROM observation_revisions AS capability
               WHERE capability.series_id = s.id
                 AND capability.source_known_at IS NOT NULL) AS source_as_of_supported
FROM series AS s
JOIN datasets AS d ON d.id = s.dataset_id
JOIN data_sources AS ds ON ds.id = d.source_id
ORDER BY ds.code, s.source_code, s.id
LIMIT $1 OFFSET $2;

-- name: GetTimelineSeries :one
SELECT s.id, s.dataset_id, s.source_code, s.name, s.unit, s.frequency,
       s.seasonal_adjustment, s.source_timezone, s.freshness_policy, s.created_at,
       ds.code AS data_source_code, ds.name AS data_source_name,
       d.external_key AS dataset_external_key,
       EXISTS (SELECT 1 FROM observation_revisions AS capability
               WHERE capability.series_id = s.id
                 AND capability.source_known_at IS NOT NULL) AS source_as_of_supported
FROM series AS s
JOIN datasets AS d ON d.id = s.dataset_id
JOIN data_sources AS ds ON ds.id = d.source_id
WHERE s.id = $1;

-- name: ListTimelineObservationsLatest :many
SELECT o.id, o.series_id, o.observation_time, o.value, o.value_text,
       o.source_known_at, o.system_known_at, o.knowledge_time_basis,
       o.raw_object_id, o.quality_flags, ro.content_sha256 AS raw_object_sha256,
       s.unit, s.frequency, ds.code AS data_source_code
FROM latest_observation_revisions AS o
JOIN series AS s ON s.id = o.series_id
JOIN datasets AS d ON d.id = s.dataset_id
JOIN data_sources AS ds ON ds.id = d.source_id
JOIN raw_objects AS ro ON ro.id = o.raw_object_id
WHERE o.series_id = $1 AND o.observation_time >= $2 AND o.observation_time < $3
  AND ($5::boolean = false OR (o.observation_time > $6 OR (o.observation_time = $6 AND o.id > $7::uuid)))
ORDER BY o.observation_time, o.id
LIMIT $4;

-- name: ListTimelineObservationsSystemAsOf :many
WITH ranked AS (
  SELECT DISTINCT ON (series_id, observation_time) id, series_id, observation_time, value, value_text,
         source_known_at, system_known_at, knowledge_time_basis, raw_object_id, quality_flags
  FROM observation_revisions
  WHERE series_id = $1 AND observation_time >= $2 AND observation_time < $3
    AND system_known_at <= $4::timestamptz
  ORDER BY series_id, observation_time, system_known_at DESC, id DESC
), filtered AS (
  SELECT * FROM ranked
  WHERE ($6::boolean = false OR (observation_time > $7 OR (observation_time = $7 AND id > $8::uuid)))
)
SELECT filtered.id, filtered.series_id, filtered.observation_time, filtered.value, filtered.value_text,
       filtered.source_known_at, filtered.system_known_at, filtered.knowledge_time_basis,
       filtered.raw_object_id, filtered.quality_flags, ro.content_sha256 AS raw_object_sha256,
       s.unit, s.frequency, ds.code AS data_source_code
FROM filtered
JOIN series AS s ON s.id = filtered.series_id
JOIN datasets AS d ON d.id = s.dataset_id
JOIN data_sources AS ds ON ds.id = d.source_id
JOIN raw_objects AS ro ON ro.id = filtered.raw_object_id
ORDER BY filtered.observation_time, filtered.id
LIMIT $5;

-- name: ListTimelineObservationsSourceAsOf :many
WITH ranked AS (
  SELECT DISTINCT ON (series_id, observation_time) id, series_id, observation_time, value, value_text,
         source_known_at, system_known_at, knowledge_time_basis, raw_object_id, quality_flags
  FROM observation_revisions
  WHERE series_id = $1 AND observation_time >= $2 AND observation_time < $3
    AND source_known_at IS NOT NULL AND source_known_at <= $4::timestamptz
  ORDER BY series_id, observation_time, source_known_at DESC, system_known_at DESC, id DESC
), filtered AS (
  SELECT * FROM ranked
  WHERE ($6::boolean = false OR (observation_time > $7 OR (observation_time = $7 AND id > $8::uuid)))
)
SELECT filtered.id, filtered.series_id, filtered.observation_time, filtered.value, filtered.value_text,
       filtered.source_known_at, filtered.system_known_at, filtered.knowledge_time_basis,
       filtered.raw_object_id, filtered.quality_flags, ro.content_sha256 AS raw_object_sha256,
       s.unit, s.frequency, ds.code AS data_source_code
FROM filtered
JOIN series AS s ON s.id = filtered.series_id
JOIN datasets AS d ON d.id = s.dataset_id
JOIN data_sources AS ds ON ds.id = d.source_id
JOIN raw_objects AS ro ON ro.id = filtered.raw_object_id
ORDER BY filtered.observation_time, filtered.id
LIMIT $5;

-- name: ListTimelineObservationRevisions :many
SELECT o.id, o.series_id, o.observation_time, o.value, o.value_text,
       o.source_known_at, o.system_known_at, o.knowledge_time_basis,
       o.raw_object_id, o.quality_flags, ro.content_sha256 AS raw_object_sha256,
       s.unit, s.frequency, ds.code AS data_source_code
FROM observation_revisions AS o
JOIN series AS s ON s.id = o.series_id
JOIN datasets AS d ON d.id = s.dataset_id
JOIN data_sources AS ds ON ds.id = d.source_id
JOIN raw_objects AS ro ON ro.id = o.raw_object_id
WHERE o.series_id = $1 AND o.observation_time >= $2 AND o.observation_time < $3
  AND ($5::boolean = false OR (o.observation_time > $6 OR
       (o.observation_time = $6 AND o.system_known_at > $7) OR
       (o.observation_time = $6 AND o.system_known_at = $7 AND o.id > $8::uuid)))
ORDER BY o.observation_time, o.system_known_at, o.id
LIMIT $4;

-- name: ListTimelineObservationsCombinedLatest :many
SELECT o.id, o.series_id, o.observation_time, o.value, o.value_text,
       o.source_known_at, o.system_known_at, o.knowledge_time_basis,
       o.raw_object_id, o.quality_flags, ro.content_sha256 AS raw_object_sha256,
       s.unit, s.frequency, ds.code AS data_source_code
FROM latest_observation_revisions AS o
JOIN series AS s ON s.id = o.series_id
JOIN datasets AS d ON d.id = s.dataset_id
JOIN data_sources AS ds ON ds.id = d.source_id
JOIN raw_objects AS ro ON ro.id = o.raw_object_id
WHERE o.series_id = ANY($1::uuid[])
  AND o.observation_time >= $2 AND o.observation_time < $3
  AND ($5::boolean = false OR (o.observation_time > $6 OR
       (o.observation_time = $6 AND o.series_id > $7::uuid) OR
       (o.observation_time = $6 AND o.series_id = $7::uuid AND o.id > $8::uuid)))
ORDER BY o.observation_time, o.series_id, o.id
LIMIT $4;

-- name: ListTimelineObservationsCombinedSystemAsOf :many
WITH ranked AS (
  SELECT DISTINCT ON (series_id, observation_time) id, series_id, observation_time, value, value_text,
         source_known_at, system_known_at, knowledge_time_basis, raw_object_id, quality_flags
  FROM observation_revisions
  WHERE series_id = ANY($1::uuid[])
    AND observation_time >= $2 AND observation_time < $3
    AND system_known_at <= $4::timestamptz
  ORDER BY series_id, observation_time, system_known_at DESC, id DESC
), filtered AS (
  SELECT * FROM ranked
  WHERE ($6::boolean = false OR (observation_time > $7 OR
         (observation_time = $7 AND series_id > $8::uuid) OR
         (observation_time = $7 AND series_id = $8::uuid AND id > $9::uuid)))
)
SELECT filtered.id, filtered.series_id, filtered.observation_time, filtered.value, filtered.value_text,
       filtered.source_known_at, filtered.system_known_at, filtered.knowledge_time_basis,
       filtered.raw_object_id, filtered.quality_flags, ro.content_sha256 AS raw_object_sha256,
       s.unit, s.frequency, ds.code AS data_source_code
FROM filtered
JOIN series AS s ON s.id = filtered.series_id
JOIN datasets AS d ON d.id = s.dataset_id
JOIN data_sources AS ds ON ds.id = d.source_id
JOIN raw_objects AS ro ON ro.id = filtered.raw_object_id
ORDER BY filtered.observation_time, filtered.series_id, filtered.id
LIMIT $5;

-- name: ListTimelineObservationsCombinedSourceAsOf :many
WITH ranked AS (
  SELECT DISTINCT ON (series_id, observation_time) id, series_id, observation_time, value, value_text,
         source_known_at, system_known_at, knowledge_time_basis, raw_object_id, quality_flags
  FROM observation_revisions
  WHERE series_id = ANY($1::uuid[])
    AND observation_time >= $2 AND observation_time < $3
    AND source_known_at IS NOT NULL AND source_known_at <= $4::timestamptz
  ORDER BY series_id, observation_time, source_known_at DESC, system_known_at DESC, id DESC
), filtered AS (
  SELECT * FROM ranked
  WHERE ($6::boolean = false OR (observation_time > $7 OR
         (observation_time = $7 AND series_id > $8::uuid) OR
         (observation_time = $7 AND series_id = $8::uuid AND id > $9::uuid)))
)
SELECT filtered.id, filtered.series_id, filtered.observation_time, filtered.value, filtered.value_text,
       filtered.source_known_at, filtered.system_known_at, filtered.knowledge_time_basis,
       filtered.raw_object_id, filtered.quality_flags, ro.content_sha256 AS raw_object_sha256,
       s.unit, s.frequency, ds.code AS data_source_code
FROM filtered
JOIN series AS s ON s.id = filtered.series_id
JOIN datasets AS d ON d.id = s.dataset_id
JOIN data_sources AS ds ON ds.id = d.source_id
JOIN raw_objects AS ro ON ro.id = filtered.raw_object_id
ORDER BY filtered.observation_time, filtered.series_id, filtered.id
LIMIT $5;
