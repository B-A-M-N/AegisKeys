# Security Policy

## Supported Versions

AegisKeys is pre-1.0. Security fixes are applied to the current mainline until
versioned releases begin.

## Reporting a Vulnerability

Do not open a public issue for suspected secret disclosure, vault corruption,
or credential-injection bypasses. Report privately to the project maintainer or
repository owner.

Include:

- Affected command or adapter.
- Exact version or commit.
- Minimal reproduction steps.
- Whether any real credential may have been exposed.

## Security Model

AegisKeys protects against accidental local disclosure in its own render,
preview, config-write, audit, and launch-preparation paths. It cannot prevent a
launched child process from reading or exfiltrating the credentials deliberately
injected into that child.

Run `aegiskeys doctor` after setup and before filing reports; include
`aegiskeys doctor --json` output when possible.
