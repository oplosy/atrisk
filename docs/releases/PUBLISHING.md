# Publishing a release

Publishing turns an approved candidate (see `RELEASE_CHECKLIST.md`) into the
artifacts a self-hosted installation runs. It does not deploy anything.

## Cut the release

Tag the approved commit on `main` and push the tag:

```powershell
git tag -a v1.0.0 -m "AtlasRisk v1.0.0" <candidate-commit>
git push origin v1.0.0
```

The `Release` workflow refuses tags that are not `vMAJOR.MINOR.PATCH` or that
point outside `main`. It produces:

| Artifact | Where |
|---|---|
| API image (`atlasrisk-api`, also runs `migrate`) | `ghcr.io/oplosy/atrisk-api:<tag>` |
| Risk-worker image (`atlasrisk-risk-worker`) | `ghcr.io/oplosy/atrisk-risk-worker:<tag>` |
| Collector image (`atlasrisk-collector`) | `ghcr.io/oplosy/atrisk-collector:<tag>` |
| Web bundle (contents of `apps/web/dist`) | release asset `atlasrisk-web-<tag>.tar.gz` |
| Web bundle checksum | release asset `atlasrisk-web-<tag>.tar.gz.sha256` |
| Image references by digest | release asset `images.txt` and the release notes |

Deployments pin the images by the digests in `images.txt`, never by tag alone,
and verify the web bundle against its checksum before unpacking it.

## Package visibility

GHCR creates both packages as private on the first publish. Either make them
public in the package settings (the source repository is public and the images
contain no secrets), or give each server a token with only `read:packages` to
pull them. Decide once, after the first release.

## Running the images

The API and risk-worker use environment variables. The collector reads a local
JSON schedule file with `--config`; its database and archive secret values are
resolved from environment-variable names in that file. All images run as
non-root users and work with a read-only root filesystem, all capabilities
dropped, and `no-new-privileges`.

| Variable | API | Worker | Meaning |
|---|---|---|---|
| `ATLASRISK_DATABASE_URL` | required | required | PostgreSQL connection URL |
| `ATLASRISK_S3_ENDPOINT`, `ATLASRISK_S3_REGION`, `ATLASRISK_S3_BUCKET` | optional | — | Raw archive for imports and decision evidence |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | with S3 | — | Raw archive credentials |
| `ATLASRISK_WORKER_POLL_SECONDS` | — | optional (2) | Idle wait between empty claims |
| `ATLASRISK_WORKER_LEASE_SECONDS` | — | optional (120) | Lease taken on each job |

Run the collector once with `atlasrisk-collector --config /run/secrets/collector.json --once`.
The schedule file stores provider, source identity, request window, interval,
and lease policy; it never stores credential values. Without `--once`, the
collector polls PostgreSQL durable schedules until it receives SIGTERM.

Minimal schedule template (replace IDs and set the referenced environment
variables at runtime):

```json
{
  "database_url_env": "ATLASRISK_DATABASE_URL",
  "archive": {
    "endpoint": "https://object-store.example",
    "region": "region",
    "bucket": "atlasrisk-raw",
    "access_key_env": "AWS_ACCESS_KEY_ID",
    "secret_key_env": "AWS_SECRET_ACCESS_KEY"
  },
  "schedules": [{
    "name": "fred-series",
    "provider": "fred",
    "source_id": "00000000-0000-4000-8000-000000000000",
    "series_id": "00000000-0000-4000-8000-000000000001",
    "credential_env": "FRED_API_KEY",
    "request": {"series_id": "CPIAUCSL", "limit": 1000, "max_pages": 100},
    "interval_seconds": 3600,
    "max_attempts": 3,
    "lease_seconds": 120
  }]
}
```

The API binary defaults to `127.0.0.1:8080`. A container deployment that needs
Docker-published traffic must explicitly pass `-listen 0.0.0.0:8080`; remote
exposure still requires an external TLS reverse proxy and OIDC-aware access
proxy. Binding the container interface does not provide authentication or
transport security. Pass `-listen <address:port>` to override either mode.

For example, a local container run may publish the explicitly bound API with:

```powershell
docker run --rm -p 127.0.0.1:8080:8080 `
  -e ATLASRISK_DATABASE_URL="postgres://..." `
  ghcr.io/oplosy/atrisk-api:<tag> -listen 0.0.0.0:8080
```

Use a TLS/OIDC proxy and a private container network before publishing the API
to a remote interface.

## Upgrading an installation

1. Back up PostgreSQL and the raw archive (`docs/runbooks/BACKUP_RESTORE.md`).
2. Run the new API image once with `migrate`; it applies pending migrations and
   exits 0, or exits non-zero and changes nothing further.
3. Start the new API and worker images, then replace the web bundle.

Migrations are forward-only. Rolling back to an older image is safe only when
the newer release added no migration; otherwise restore the backup from step 1.
