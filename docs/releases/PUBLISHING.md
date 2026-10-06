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

Both images take their configuration from environment variables only and run
as non-root users; they work with a read-only root filesystem, all capabilities
dropped, and `no-new-privileges`.

| Variable | API | Worker | Meaning |
|---|---|---|---|
| `ATLASRISK_DATABASE_URL` | required | required | PostgreSQL connection URL |
| `ATLASRISK_S3_ENDPOINT`, `ATLASRISK_S3_REGION`, `ATLASRISK_S3_BUCKET` | optional | — | Raw archive for imports and decision evidence |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | with S3 | — | Raw archive credentials |
| `ATLASRISK_WORKER_POLL_SECONDS` | — | optional (2) | Idle wait between empty claims |
| `ATLASRISK_WORKER_LEASE_SECONDS` | — | optional (120) | Lease taken on each job |

The API listens on `:8080`; pass `-listen <address:port>` to bind elsewhere.

## Upgrading an installation

1. Back up PostgreSQL and the raw archive (`docs/runbooks/BACKUP_RESTORE.md`).
2. Run the new API image once with `migrate`; it applies pending migrations and
   exits 0, or exits non-zero and changes nothing further.
3. Start the new API and worker images, then replace the web bundle.

Migrations are forward-only. Rolling back to an older image is safe only when
the newer release added no migration; otherwise restore the backup from step 1.
