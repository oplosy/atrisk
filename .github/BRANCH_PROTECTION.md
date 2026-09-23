# Protected `main` policy

GitHub repository settings must enforce the following policy for `main`:

- pull requests are required; direct pushes are not permitted;
- at least one approving review is required;
- required status checks must pass before merge;
- stale approvals are dismissed when new commits change the reviewed diff;
- force pushes and branch deletion are disabled;
- administrators are subject to these protections;
- the branch may not be bypassed by a personal access token or automation.

The required-check names are configured with the repository's CI workflow when
that workflow exists. Until CI exists, the orchestrator must run the task
packet's required verification commands and record their results; this is not a
waiver of the protected-trunk rule.

Short-lived branches must use `task/AR-NNN-short-slug` and target `main`. Do not
create a long-lived `dev` branch. One active writer gets one worktree; review
and merge remain serial.

## Bootstrap exception

If a remote repository has no commit and therefore cannot accept a pull request,
the repository owner may explicitly authorize one seed commit to `main`. Record
that approval and commit in the orchestration report. This one-time setup action
does not permit later direct pushes to `main`.
