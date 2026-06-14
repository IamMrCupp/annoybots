# Security Policy

## Supported versions

annoybots is released from `main` with semantic-versioned tags. Security fixes
land on the latest release; please test against the most recent `vX.Y.Z` (or
`:latest`) before reporting.

## Reporting a vulnerability

**Please do not open a public issue for security vulnerabilities.**

Report privately via GitHub's
[private vulnerability reporting](https://github.com/IamMrCupp/annoybots/security/advisories/new)
(the repo's **Security → Report a vulnerability** tab). If you can't use that,
email the maintainer at mrcupp@mrcupp.com.

Please include:

- a description of the issue and its impact,
- steps to reproduce (a minimal config or message sequence helps), and
- affected version / commit.

You'll get an acknowledgement, and a fix or mitigation will be coordinated before
any public disclosure. This is a small solo-maintained project, so response is
best-effort — thanks for your patience.

## Scope notes

- **Secrets** (IRC/SASL passwords, Twitch/Discord tokens) are never stored in
  configs or committed — they're injected via env vars from Kubernetes Secrets.
  A finding that real credentials were committed is in scope; placeholder
  templates (`secret.example.yaml`, `changeme` values) are not.
- The **admin console** authenticates by verified identity (services account /
  Discord ID / Twitch login) and is DM-only. The optional `!login` password
  fallback is nick-keyed and documented as weaker — auth-bypass beyond those
  documented limits is in scope.
