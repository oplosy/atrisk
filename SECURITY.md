# Security Policy

AtlasRisk is a single-user, self-hosted decision-support system. It does not
execute orders, hold custody, store broker credentials, or provide automated
trading. Security reports and changes must preserve that boundary.

## Protected data and secrets

Never commit or paste into issues, pull requests, logs, fixtures, or examples:

- real API keys, access tokens, session cookies, private keys, or passwords;
- broker credentials, exchange credentials, or credentials for storage and
  infrastructure services;
- personal portfolio data, account identifiers, transaction exports, or other
  personally identifying financial records;
- production source payloads when they contain credentials, private data, or
  account-specific information.

Use environment variables or the deployment secret store for runtime secrets.
Keep local secret files outside the repository and add only redacted example
configuration. A secret-looking value in a test must be an obviously synthetic
fixture, not a truncated copy of a real value.

Logs and error reports must redact authorization headers, credentials, raw
portfolio payloads, and sensitive source responses. Raw external data may be
archived only according to the accepted provenance and retention design; it is
not permission to publish private or credential-bearing payloads.

## Reporting a vulnerability

Do not open a public issue for an unpatched vulnerability or include an
exploitable proof of concept in a public pull request. Use GitHub's private
vulnerability reporting/security advisory channel for this repository when it
is enabled. If that channel is unavailable, contact the repository maintainers
privately and provide:

- affected commit, file, endpoint, or dependency;
- concise impact and reproduction steps;
- the minimum safe evidence needed to validate the issue;
- any suggested mitigation and disclosure constraints.

Do not include secrets or personal financial data in the report. Maintainers
should acknowledge receipt, reproduce privately, prepare a fix, and coordinate
disclosure only after affected users have a mitigation.

## Contributor security checks

Before opening a pull request:

- inspect the diff for secrets and personal financial fixtures;
- verify that logs and errors do not expose credentials or portfolio payloads;
- run the packet's required verification and any configured secret scan;
- report dependency or infrastructure assumptions in the handoff report.

Security fixes remain subject to the same task-packet ownership and protected
trunk workflow. A security concern is a reason to stop and report a scope or
architecture conflict, not a reason to bypass review or push directly to
`main`.
