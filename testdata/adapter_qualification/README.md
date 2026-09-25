# Real-application adapter qualification

One JSON file per app may be added here only after a maintainer records a real
launch against a pinned supported app version. Required evidence includes app
version, OS/architecture, provider/model selected, secret non-disclosure,
exit handling, and configuration restoration/conflict behavior.

This directory intentionally contains no blanket PASS file. Fake-executable
smoke tests live in Go tests and prove contract/secret-injection mechanics, not
third-party application compatibility. `verified` adapter confidence currently
means the four automated contract gates pass; release documentation must not
present it as real-app compatibility proof.
