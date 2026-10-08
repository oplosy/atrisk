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
            if row[1] == "scenario.revalue":
                cursor.execute(
                    """
                    UPDATE scenario_runs SET state='running'
                    WHERE job_id=%s::uuid AND state='queued'
                    """,
                    (row[0],),
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

    def renew(self, job_id: str, worker_id: str, attempt_count: int, lease_seconds: float) -> bool:
        """Extend a live lease; a false result marks the owner as stale."""
        cursor = self._connection.cursor()
        try:
            cursor.execute(
                """
                UPDATE risk_jobs
                SET lease_expires_at=clock_timestamp() + make_interval(secs => %s)
                WHERE id=%s::uuid AND state='running' AND lease_owner=%s
                  AND attempt_count=%s AND lease_expires_at > clock_timestamp()
                """,
                (lease_seconds, job_id, worker_id, attempt_count),
            )
            renewed = cursor.rowcount == 1
            self._connection.commit()
            return renewed
        except Exception:
            self._connection.rollback()
            raise
        finally:
            cursor.close()

    def recover_expired(self) -> int:
        """Recover expired jobs and keep scenario evidence in the same lifecycle state."""
        cursor = self._connection.cursor()
        try:
            cursor.execute(
                """
                WITH expired AS (
                    UPDATE risk_jobs
                    SET state=CASE WHEN attempt_count >= max_attempts
                                   THEN 'failed' ELSE 'retryable_failed' END,
                        available_at=clock_timestamp(), lease_owner=NULL,
                        lease_expires_at=NULL,
                        completed_at=CASE WHEN attempt_count >= max_attempts
                                          THEN clock_timestamp() ELSE NULL END,
                        error_code='LEASE_EXPIRED',
                        error_message='worker lease expired'
                    WHERE state='running' AND lease_expires_at <= clock_timestamp()
                    RETURNING id::text, attempt_count, kind, state
                )
                SELECT id, attempt_count, kind, state FROM expired
                """
            )
            expired = cursor.fetchall()
            for job_id, attempt, kind, state in expired:
                cursor.execute(
                    """
                    UPDATE risk_job_attempts
                    SET finished_at=clock_timestamp(), outcome='expired',
                        error_code='LEASE_EXPIRED', error_message='worker lease expired'
                    WHERE job_id=%s::uuid AND attempt=%s AND finished_at IS NULL
                    """,
                    (job_id, attempt),
                )
                if kind != "scenario.revalue":
                    continue
                if state == "failed":
                    cursor.execute(
                        """
                        UPDATE scenario_runs
                        SET state='failed', completed_at=clock_timestamp()
                        WHERE job_id=%s::uuid AND state IN ('queued','running')
                        """,
                        (job_id,),
                    )
                elif state == "retryable_failed":
                    cursor.execute(
                        """
                        UPDATE scenario_runs
                        SET state='queued', completed_at=NULL
                        WHERE job_id=%s::uuid AND state='running'
                        """,
                        (job_id,),
                    )
            self._connection.commit()
            return len(expired)
        except Exception:
            self._connection.rollback()
            raise
        finally:
            cursor.close()

    def complete(
        self, job_id: str, worker_id: str, attempt_count: int, result: Mapping[str, Any]
    ) -> bool:
        cursor = self._connection.cursor()
        try:
            digest = sha256_json(result)
            cursor.execute(
                """
                UPDATE risk_jobs
                SET state='succeeded', result=%s::jsonb, result_hash=%s,
                    lease_owner=NULL, lease_expires_at=NULL, completed_at=clock_timestamp()
                WHERE id=%s::uuid AND state='running' AND lease_owner=%s
                  AND attempt_count=%s AND lease_expires_at > clock_timestamp()
                RETURNING id, kind
                """,
                (self._json(result), digest, job_id, worker_id, attempt_count),
            )
            row = cursor.fetchone()
            if row is None:
                self._connection.commit()
                return False
            if row[1] == "scenario.revalue":
                self._persist_scenario_result(cursor, job_id, result, digest)
            cursor.execute(
                """
                UPDATE risk_job_attempts
                SET finished_at=clock_timestamp(), outcome='succeeded'
                WHERE job_id=%s::uuid
                  AND attempt=%s AND finished_at IS NULL
                """,
                (job_id, attempt_count),
            )
            self._connection.commit()
            return True
        except Exception:
            self._connection.rollback()
            raise
        finally:
            cursor.close()

    def fail(
        self,
        job_id: str,
        worker_id: str,
        attempt_count: int,
        code: str,
        message: str,
        retryable: bool,
    ) -> bool:
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
                  AND attempt_count=%s AND lease_expires_at > clock_timestamp()
                RETURNING state
                """,
                (retryable, code, message, retryable, job_id, worker_id, attempt_count),
            )
            row = cursor.fetchone()
            if row is None:
                self._connection.commit()
                return False
            final_state = row[0]
            if final_state == "failed":
                cursor.execute(
                    """
                    UPDATE scenario_runs SET state='failed', completed_at=clock_timestamp()
                    WHERE job_id=%s::uuid AND state IN ('queued','running')
                    """,
                    (job_id,),
                )
            elif final_state == "retryable_failed":
                cursor.execute(
                    """
                    UPDATE scenario_runs SET state='queued'
                    WHERE job_id=%s::uuid AND state='running'
                    """,
                    (job_id,),
                )
            cursor.execute(
                """
                UPDATE risk_job_attempts
                SET finished_at=clock_timestamp(),
                    outcome=%s,
                    error_code=%s, error_message=%s
                WHERE job_id=%s::uuid
                  AND attempt=%s AND finished_at IS NULL
                """,
                (final_state, code, message, job_id, attempt_count),
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

    def _persist_scenario_result(
        self, cursor: Any, job_id: str, result: Mapping[str, Any], digest: str
    ) -> None:
        output = result.get("output")
        if not isinstance(output, Mapping):
            raise ValueError("scenario result output must be an object")
        state = output.get("state")
        if state not in {"valid", "degraded", "blocked"}:
            raise ValueError("scenario result state is invalid")
        cursor.execute(
            """
            UPDATE scenario_runs
            SET state=%s, result=%s::jsonb, result_hash=%s, completed_at=clock_timestamp()
            WHERE job_id=%s::uuid AND state IN ('queued','running')
            RETURNING id::text
            """,
            (state, self._json(result), digest, job_id),
        )
        if cursor.fetchone() is None:
            raise ValueError("scenario run for completed job was not found")

        for position in output.get("positions", []):
            if not isinstance(position, Mapping):
                raise ValueError("scenario position result must be an object")
            cursor.execute(
                """
                INSERT INTO scenario_run_positions (
                    run_id,snapshot_line_id,instrument_id,state,reason_codes,
                    pre_value_try,post_value_try,pnl_try,pre_value_usd,post_value_usd,pnl_usd,
                    price_return,yield_return,fx_multiplier_try,fx_multiplier_usd
                )
                SELECT id,%s::uuid,%s::uuid,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s
                FROM scenario_runs WHERE job_id=%s::uuid
                """,
                (
                    position.get("snapshot_line_id"),
                    position.get("instrument_id"),
                    position.get("state"),
                    position.get("reason_codes", []),
                    position.get("pre_value_try"),
                    position.get("post_value_try"),
                    position.get("pnl_try"),
                    position.get("pre_value_usd"),
                    position.get("post_value_usd"),
                    position.get("pnl_usd"),
                    position.get("price_return"),
                    position.get("yield_return"),
                    position.get("fx_multiplier_try"),
                    position.get("fx_multiplier_usd"),
                    job_id,
                ),
            )

        attribution = output.get("attribution")
        if isinstance(attribution, Mapping):
            method = attribution.get("method", "shapley")
            method_version = attribution.get("method_version", "1.0.0")
            tolerance = attribution.get("tolerance", "0")
            residual = attribution.get("interaction_residual")
            for factor in attribution.get("factor_contributions", []):
                if not isinstance(factor, Mapping):
                    raise ValueError("scenario factor attribution must be an object")
                cursor.execute(
                    """
                    INSERT INTO scenario_run_factor_attributions (
                        run_id,factor,contribution,method,method_version,interaction_residual,tolerance
                    )
                    SELECT id,%s,%s,%s,%s,%s,%s
                    FROM scenario_runs WHERE job_id=%s::uuid
                    """,
                    (
                        factor.get("factor"),
                        factor.get("contribution"),
                        method,
                        method_version,
                        residual,
                        tolerance,
                        job_id,
                    ),
                )
            for position in attribution.get("position_contributions", []):
                if not isinstance(position, Mapping):
                    raise ValueError("scenario position attribution must be an object")
                cursor.execute(
                    """
                    INSERT INTO scenario_run_position_attributions (
                        run_id,snapshot_line_id,instrument_id,state,total_pnl,factor_contributions,residual
                    )
                    SELECT id,%s::uuid,%s::uuid,%s,%s,%s::jsonb,%s
                    FROM scenario_runs WHERE job_id=%s::uuid
                    """,
                    (
                        position.get("snapshot_line_id"),
                        position.get("instrument_id"),
                        position.get("state", "blocked"),
                        position.get("total_pnl"),
                        self._json(position.get("factor_contributions", [])),
                        position.get("residual"),
                        job_id,
                    ),
                )

        pre_metrics = output.get("pre_metrics", {})
        post_metrics = output.get("post_metrics", {})
        for category, value_key in (("volatility", "annualized"), ("correlations", "coefficient")):
            before = pre_metrics.get(category, {}) if isinstance(pre_metrics, Mapping) else {}
            after = post_metrics.get(category, {}) if isinstance(post_metrics, Mapping) else {}
            for metric_key, pre_value in before.items():
                metric_state = (
                    pre_value.get("state", "valid") if isinstance(pre_value, Mapping) else "valid"
                )
                before_value = (
                    pre_value.get(value_key) if isinstance(pre_value, Mapping) else pre_value
                )
                reason = pre_value.get("reason") if isinstance(pre_value, Mapping) else None
                cursor.execute(
                    """
                    INSERT INTO scenario_run_metrics (
                        run_id,metric_key,pre_value,post_value,state,reason_code
                    )
                    SELECT id,%s,%s,%s,%s,%s FROM scenario_runs WHERE job_id=%s::uuid
                    """,
                    (
                        f"{category}:{metric_key}",
                        before_value,
                        after.get(metric_key),
                        metric_state,
                        reason,
                        job_id,
                    ),
                )
