"""Small psycopg-compatible bridge for the PostgreSQL durable job queue.

The risk engine deliberately does not own a connection pool. The application
passes an open DB-API 2.0 PostgreSQL connection (for example psycopg 3) to this
adapter, so claim, execution result, and completion remain one explicit
runtime boundary without introducing an HTTP service or a second queue.
"""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from .canonical import sha256_json


class PostgresQueueClient:
    """DB-API adapter implementing :class:`~atlasrisk.jobs.worker.QueueClient`."""

    def __init__(self, connection: Any):
        self._connection = connection

    def claim(self, worker_id: str, lease_seconds: float) -> Mapping[str, Any] | None:
        cursor = self._connection.cursor()
        try:
            cursor.execute(
                """
                WITH candidate AS (
                    SELECT id FROM risk_jobs
                    WHERE state IN ('queued', 'retryable_failed')
                      AND available_at <= clock_timestamp()
                    ORDER BY available_at, created_at, id
                    FOR UPDATE SKIP LOCKED LIMIT 1
                )
                UPDATE risk_jobs AS job
                SET state='running', attempt_count=job.attempt_count+1,
                    lease_owner=%s,
                    lease_expires_at=clock_timestamp() + make_interval(secs => %s),
                    updated_at=clock_timestamp()
                FROM candidate
                WHERE job.id=candidate.id
                RETURNING job.id::text, job.kind, job.schema_version,
                    job.idempotency_key, job.input_snapshot_ids, job.payload,
                    job.attempt_count, job.lease_expires_at
                """,
                (worker_id, lease_seconds),
            )
            row = cursor.fetchone()
            if row is None:
                self._connection.commit()
                return None
            cursor.execute(
                """
                INSERT INTO risk_job_attempts (job_id, attempt, worker_id, lease_expires_at)
                VALUES (%s::uuid, %s, %s, %s)
                """,
                (row[0], row[6], worker_id, row[7]),
            )
            self._connection.commit()
            return {
                "id": row[0],
                "attempt_count": row[6],
                "envelope": {
                    "kind": row[1],
                    "schema_version": row[2],
                    "idempotency_key": row[3],
                    "input_snapshot_ids": list(row[4]),
                    "payload": row[5],
                },
            }
        except Exception:
            self._connection.rollback()
            raise
        finally:
            cursor.close()

    def complete(self, job_id: str, worker_id: str, result: Mapping[str, Any]) -> bool:
        cursor = self._connection.cursor()
        try:
            digest = sha256_json(result)
            cursor.execute(
                """
                UPDATE risk_jobs
                SET state='succeeded', result=%s::jsonb, result_hash=%s,
                    lease_owner=NULL, lease_expires_at=NULL, completed_at=clock_timestamp()
                WHERE id=%s::uuid AND state='running' AND lease_owner=%s
                  AND lease_expires_at > clock_timestamp()
                RETURNING id
                """,
                (self._json(result), digest, job_id, worker_id),
            )
            row = cursor.fetchone()
            if row is None:
                self._connection.commit()
                return False
            cursor.execute(
                """
                UPDATE risk_job_attempts
                SET finished_at=clock_timestamp(), outcome='succeeded'
                WHERE job_id=%s::uuid AND finished_at IS NULL
                """,
                (job_id,),
            )
            self._connection.commit()
            return True
        except Exception:
            self._connection.rollback()
            raise
        finally:
            cursor.close()

    def fail(self, job_id: str, worker_id: str, code: str, message: str, retryable: bool) -> bool:
        cursor = self._connection.cursor()
        try:
            cursor.execute(
                """
                UPDATE risk_jobs
                SET state=CASE WHEN %s AND attempt_count < max_attempts
                               THEN 'retryable_failed' ELSE 'failed' END,
                    available_at=clock_timestamp(), error_code=%s, error_message=%s,
                    lease_owner=NULL, lease_expires_at=NULL,
                    completed_at=CASE WHEN NOT (%s AND attempt_count < max_attempts)
                                      THEN clock_timestamp() ELSE NULL END
                WHERE id=%s::uuid AND state='running' AND lease_owner=%s
                  AND lease_expires_at > clock_timestamp()
                RETURNING state
                """,
                (retryable, code, message, retryable, job_id, worker_id),
            )
            row = cursor.fetchone()
            if row is None:
                self._connection.commit()
                return False
            final_state = row[0]
            cursor.execute(
                """
                UPDATE risk_job_attempts
                SET finished_at=clock_timestamp(),
                    outcome=%s,
                    error_code=%s, error_message=%s
                WHERE job_id=%s::uuid AND finished_at IS NULL
                """,
                (final_state, code, message, job_id),
            )
            self._connection.commit()
            return True
        except Exception:
            self._connection.rollback()
            raise
        finally:
            cursor.close()

    @staticmethod
    def _json(value: Mapping[str, Any]) -> str:
        from .canonical import canonical_json

        return canonical_json(value).decode("utf-8")
