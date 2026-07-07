# AegisKeys — Full Implementation Specification

## Product Name

**AegisKeys**

## One-Line Summary

AegisKeys is an ultra-secure interactive terminal application for storing API providers and API secrets separately, then safely injecting the correct credentials into coding agents, CLIs, and developer tools on demand.

---

# 1. Core Mission

Build a local-first, secure, modular terminal application that allows the user to:

1. Store provider metadata without secrets.
2. Store API keys separately in an encrypted vault.
3. Create profiles that map providers to specific keys.
4. Launch coding tools with scoped environment variables injected only into the child process.
5. Avoid global secret exposure.
6. Audit local usage without ever logging secret values.
7. Provide a polished interactive terminal UI.
8. Provide a useful CLI for automation.

The application should be genuinely useful, functional, secure by default, and aesthetically polished.

---

# 2. Non-Negotiable Requirements

Do not ask clarification questions. Make reasonable implementation choices and proceed.

Use English only for all UI text, documentation, comments, and command output.

If this repository already has a clear language/framework, use it.

If the folder is empty or ambiguous, implement AegisKeys in **Go**.

If using Go, use:

* `cobra` for CLI commands
* `bubbletea` for the TUI
* `lipgloss` for visual styling
* `bubbles` for text inputs, lists, tables, spinners, etc.
* `age`, `x/crypto`, or another reputable crypto library for encrypted storage
* OS keyring support if practical
* Permission hardening for local vault/config files

Do not use Docker.

Do not use cloud services.

Do not store secrets in plaintext.

Do not print full secrets.

Do not log secrets.

Do not create fake “security theater.” Implement real protections.

---

# 3. Application Concept

AegisKeys separates three things:

## 3.1 Providers

Providers are non-secret metadata.

Example:

```json
{
  "name": "OpenRouter",
  "slug": "openrouter",
  "base_url": "https://openrouter.ai/api/v1",
  "env_var": "OPENROUTER_API_KEY",
  "auth_header": "Authorization: Bearer ${KEY}",
  "tags": ["coding", "router", "paid", "free-tier"],
  "models": ["anthropic/claude-sonnet-4", "openai/gpt-4.1", "qwen/qwen3-coder"],
  "notes": "Router provider for multiple LLM backends."
}
```

## 3.2 Secrets

Secrets are encrypted key records.

Example logical structure before encryption:

```json
{
  "id": "key_...",
  "provider_slug": "openrouter",
  "label": "main-openrouter",
  "secret": "sk-or-v1-...",
  "created_at": "...",
  "updated_at": "...",
  "last_used_at": null,
  "tags": ["primary", "coding"]
}
```

The secret value must never be stored outside the encrypted vault.

## 3.3 Profiles

Profiles bind a provider to a key and optional runtime behavior.

Example:

```json
{
  "name": "or-main",
  "provider_slug": "openrouter",
  "key_id": "key_...",
  "env": {
    "OPENAI_BASE_URL": "https://openrouter.ai/api/v1"
  },
  "aliases": ["openrouter-main", "router"]
}
```

Profiles allow commands like:

```bash
aegiskeys run --profile or-main -- aider
aegiskeys run --profile anthropic-main -- claude
aegiskeys env --profile mistral-dev
```

---

# 4. Target User

The target user is a developer using many coding agents, model providers, API platforms, routers, local model runners, and experimental inference services.

The user wants to avoid scattering secrets across:

* `.env` files
* shell history
* global shell exports
* config files
* IDE settings
* coding agent configs
* random scripts
* copied terminal commands

AegisKeys should act as a secure local bridge between the user’s provider credentials and the tools that need them.

---

# 5. Threat Model

AegisKeys protects against:

1. Accidental secret leakage into terminal output.
2. Accidental secret leakage into logs.
3. Accidental secret commits to Git.
4. Plaintext storage of API keys.
5. Overly broad shell environment exposure.
6. Unsafe file permissions.
7. Secret exposure through generated `.env` files.
8. Misconfigured provider/key mappings.
9. Confusion about which key is being used for which provider.
10. Basic local snooping by non-privileged users.

AegisKeys does **not** fully protect against:

1. A fully compromised machine.
2. Kernel-level malware.
3. Malicious coding tools that intentionally exfiltrate injected environment variables.
4. Shoulder surfing while the user enters secrets.
5. Terminal scrollback exposure if the user manually pastes secrets elsewhere.
6. Hardware keyloggers.

The README must clearly state this threat model.

---

# 6. Security Principles

AegisKeys must follow these principles:

1. **Local-first**: no remote sync by default.
2. **Secrets encrypted at rest**.
3. **Provider metadata separated from key material**.
4. **Child-process scoped secret injection**.
5. **No global exports unless explicitly requested**.
6. **No full secret display**.
7. **No secret logging**.
8. **Secure file permissions by default**.
9. **Explicit confirmation for risky operations**.
10. **Fail closed whenever possible**.
11. **Useful security diagnostics through `doctor`**.
12. **Audit metadata only, never key values**.

---

# 7. Storage Layout

Use an OS-appropriate config directory.

On Linux:

```text
~/.config/aegiskeys/
```

Suggested layout:

```text
~/.config/aegiskeys/
├── config.json
├── providers.json
├── profiles.json
├── vault.enc
├── audit.log
└── tmp/
```

File permissions:

```text
~/.config/aegiskeys/        0700
config.json                0600
providers.json             0600
profiles.json              0600
vault.enc                  0600
audit.log                  0600
tmp/                       0700
temporary env files        0600
```

Add repo-level `.gitignore` entries:

```gitignore
# AegisKeys local secret/config artifacts
.aegiskeys/
aegiskeys.local.*
vault.enc
*.vault
*.secret
*.secrets
.env
.env.*
!.env.example
*.key
*.pem
*.token
*.log
```

The app should also warn if it detects likely unsafe plaintext secret files in the current repo.

---

# 8. Encryption Design

Implement real encryption.

Preferred design:

1. User creates a master password during `aegiskeys init`.
2. Derive an encryption key using **Argon2id**.
3. Store only salt and KDF parameters alongside the encrypted vault.
4. Encrypt vault contents using **XChaCha20-Poly1305** or **AES-256-GCM**.
5. Authenticate all ciphertext.
6. Refuse to decrypt if authentication fails.
7. Never store the raw master password.
8. Zero sensitive byte slices where practical.

Vault envelope example:

```json
{
  "version": 1,
  "kdf": {
    "name": "argon2id",
    "memory_kib": 65536,
    "iterations": 3,
    "parallelism": 2,
    "salt": "base64..."
  },
  "cipher": {
    "name": "xchacha20-poly1305",
    "nonce": "base64..."
  },
  "ciphertext": "base64..."
}
```

Decrypted vault logical shape:

```json
{
  "version": 1,
  "keys": [
    {
      "id": "key_...",
      "provider_slug": "openrouter",
      "label": "main",
      "secret": "sk-or-v1-...",
      "created_at": "2026-07-05T00:00:00Z",
      "updated_at": "2026-07-05T00:00:00Z",
      "last_used_at": null,
      "tags": ["primary"]
    }
  ]
}
```

Optional enhancement:

* Use OS keyring to store a randomly generated vault key.
* Still support password-based fallback.
* If OS keyring is unavailable, use password-derived encryption.

Do not block MVP on OS keyring if it complicates the implementation. Password-derived encrypted vault is acceptable for MVP.

---

# 9. Provider Registry

Provider metadata should be stored separately in `providers.json`.

Seed default providers during `aegiskeys init`.

Default providers:

1. OpenAI
2. Anthropic
3. OpenRouter
4. Mistral
5. Google Gemini
6. Hugging Face
7. Groq
8. Cerebras
9. Together
10. Fireworks
11. DeepSeek
12. Zhipu / GLM
13. Moonshot / Kimi
14. Qwen / DashScope
15. ModelScope
16. Nvidia NIM
17. Cloudflare Workers AI
18. Ollama / Local
19. LM Studio / Local
20. Custom Provider

Provider fields:

```go
type Provider struct {
    Name        string            `json:"name"`
    Slug        string            `json:"slug"`
    BaseURL     string            `json:"base_url"`
    EnvVar      string            `json:"env_var"`
    AuthHeader  string            `json:"auth_header"`
    Models      []string          `json:"models,omitempty"`
    Tags        []string          `json:"tags,omitempty"`
    Notes       string            `json:"notes,omitempty"`
    ExtraEnv    map[string]string `json:"extra_env,omitempty"`
    CreatedAt   time.Time         `json:"created_at"`
    UpdatedAt   time.Time         `json:"updated_at"`
}
```

Provider operations:

* list
* search
* add
* edit
* remove
* inspect
* validate
* import defaults
* export non-secret metadata

Provider validation:

* slug must be unique
* env var must be valid shell-style env name
* base URL should be URL-like if present
* auth header template must not contain literal secret
* provider metadata must not contain API key-looking values

---

# 10. Secret Management

Secret commands:

```bash
aegiskeys key add
aegiskeys key list
aegiskeys key show --id <id>
aegiskeys key rename --id <id> --label <label>
aegiskeys key rotate --id <id>
aegiskeys key delete --id <id>
```

Rules:

* `key list` shows masked keys only.
* `key show` shows metadata and masked key only.
* Full key reveal requires an explicit command and confirmation.
* Prefer not to implement full reveal in MVP unless necessary.
* If implemented, command must be named clearly:

```bash
aegiskeys key reveal --id <id>
```

Before revealing:

```text
This will print the full secret to your terminal.
Terminal scrollback, shell logs, and screen recordings may capture it.
Type REVEAL to continue:
```

Masking examples:

```text
sk-...abcd
sk-or-v1-...91ef
ghp_...1132
hf_...aa09
<hidden>
```

Masking function requirements:

* Never return original secret unchanged.
* For very short secrets, return `<hidden>`.
* Preserve only small prefix/suffix.
* Tests must ensure full secrets are not printed by normal commands.

---

# 11. Profiles

Profiles map provider + key + optional runtime env.

Profile fields:

```go
type Profile struct {
    Name         string            `json:"name"`
    ProviderSlug string           `json:"provider_slug"`
    KeyID        string           `json:"key_id"`
    Env          map[string]string `json:"env,omitempty"`
    Aliases      []string          `json:"aliases,omitempty"`
    Notes        string           `json:"notes,omitempty"`
    CreatedAt    time.Time        `json:"created_at"`
    UpdatedAt    time.Time        `json:"updated_at"`
}
```

Commands:

```bash
aegiskeys profile create
aegiskeys profile list
aegiskeys profile inspect <name>
aegiskeys profile edit <name>
aegiskeys profile delete <name>
```

Validation:

* profile name must be unique
* provider slug must exist
* key ID must exist
* key must belong to provider unless user explicitly overrides with confirmation
* aliases must not collide with other profile names or aliases

---

# 12. Environment Injection

The safest default behavior is child-process scoped injection.

Primary command:

```bash
aegiskeys run --profile <profile> -- <command> [args...]
```

Examples:

```bash
aegiskeys run --profile openrouter-main -- aider
aegiskeys run --profile anthropic-main -- claude
aegiskeys run --profile openai-main -- codex
aegiskeys run --profile mistral-main -- crush
aegiskeys run --profile qwen-main -- qwen-code
```

Runtime behavior:

1. Unlock vault if needed.
2. Resolve profile.
3. Resolve provider.
4. Resolve key.
5. Build environment for child process.
6. Inject provider’s primary env var.
7. Inject configured base URL env vars when applicable.
8. Launch command.
9. Do not print secret.
10. Update `last_used_at`.
11. Write audit event without secret.

Example environment:

For OpenRouter:

```text
OPENROUTER_API_KEY=<secret>
OPENAI_API_KEY=<secret>              optional compatibility mode
OPENAI_BASE_URL=https://openrouter.ai/api/v1
```

For Anthropic:

```text
ANTHROPIC_API_KEY=<secret>
```

For OpenAI:

```text
OPENAI_API_KEY=<secret>
```

For Mistral:

```text
MISTRAL_API_KEY=<secret>
```

For Gemini:

```text
GEMINI_API_KEY=<secret>
GOOGLE_API_KEY=<secret>              optional compatibility mode
```

Add compatibility mode:

```bash
aegiskeys run --profile openrouter-main --compat openai -- aider
```

This can inject additional env vars expected by OpenAI-compatible clients.

---

# 13. Env Output Command

Command:

```bash
aegiskeys env --profile <profile>
```

Default behavior:

* Print masked preview only.
* Do not print exportable secret values by default.

Example default output:

```text
Profile: openrouter-main
Provider: OpenRouter
Injected variables:
  OPENROUTER_API_KEY=sk-or-v1-...91ef
  OPENAI_BASE_URL=https://openrouter.ai/api/v1

No full secrets printed.
Use --export with explicit confirmation to print shell exports.
```

Risky export mode:

```bash
aegiskeys env --profile openrouter-main --export
```

Before printing full export commands:

```text
This will print full secrets to your terminal.
Only continue if you understand the risk.
Type EXPORT to continue:
```

Then output:

```bash
export OPENROUTER_API_KEY='...'
export OPENAI_BASE_URL='https://openrouter.ai/api/v1'
```

---

# 14. Temporary Env Files

Command:

```bash
aegiskeys envfile --profile <profile>
```

Default behavior:

* Ask for confirmation.
* Create a temporary env file in AegisKeys tmp directory.
* Use permission `0600`.
* Print path.
* Provide deletion command.

Example:

```text
Created temporary env file:
~/.config/aegiskeys/tmp/openrouter-main.1720000000.env

Permissions: 0600

Delete it with:
aegiskeys shred-envfile ~/.config/aegiskeys/tmp/openrouter-main.1720000000.env
```

Command:

```bash
aegiskeys shred-envfile <path>
```

Behavior:

* Delete env file.
* If possible, overwrite first.
* Warn that SSDs and journaling filesystems may retain traces.

---

# 15. Audit Log

Audit log path:

```text
~/.config/aegiskeys/audit.log
```

Audit events must not include secret values.

Events:

* vault initialized
* vault unlocked
* vault locked
* provider added
* provider edited
* key added
* key rotated
* key deleted
* profile created
* profile used
* child command launched
* env export requested
* envfile created
* doctor warning found

Example audit event:

```json
{
  "time": "2026-07-05T00:00:00Z",
  "event": "profile_used",
  "profile": "openrouter-main",
  "provider": "openrouter",
  "command": "aider",
  "secret": "[redacted]"
}
```

---

# 16. Doctor Command

Command:

```bash
aegiskeys doctor
```

Doctor checks:

1. Config directory exists.
2. Config directory permission is `0700`.
3. Vault file exists.
4. Vault file permission is `0600`.
5. Vault envelope has encryption metadata.
6. Vault decrypts successfully after unlock.
7. Provider registry exists.
8. Default providers are loaded.
9. Profiles reference valid providers.
10. Profiles reference valid keys.
11. No provider metadata appears to contain secret-looking values.
12. `.gitignore` contains AegisKeys protections.
13. Current repo does not contain obvious plaintext secret files.
14. Temporary env files are not stale.
15. Audit log exists and is permission restricted.
16. No generated logs contain secret-looking values.
17. Shell history warning if dangerous command patterns are detected, where practical.

Example output:

```text
AegisKeys Doctor

Vault:
  ✓ Vault file exists
  ✓ Vault appears encrypted
  ✓ Vault permissions are 0600
  ✓ Config directory permissions are 0700

Providers:
  ✓ 20 providers loaded
  ✓ No provider metadata appears to contain secrets

Profiles:
  ✓ 3 profiles configured
  ! 1 profile references a missing key

Repository:
  ✓ .gitignore protects vault files
  ! Found .env.local in current directory

Overall: WARN
```

Exit codes:

```text
0 = OK
1 = WARN
2 = FAIL
```

---

# 17. Interactive TUI

Command:

```bash
aegiskeys tui
```

Also default to TUI when running:

```bash
aegiskeys
```

## 17.1 Visual Direction

The TUI should feel like a secure command center.

Style:

* Dark terminal aesthetic
* Clean borders
* Strong spacing
* Minimal clutter
* Clear selected state
* Subtle provider badges
* Masked secrets
* Security status indicators
* Helpful empty states
* No walls of text

Suggested palette names:

* Vault Gold
* Iron Gray
* Signal Green
* Warning Amber
* Danger Red
* Muted Blue

Do not hardcode unreadable colors. Keep terminal compatibility in mind.

## 17.2 Main Screens

Minimum screens:

1. Dashboard
2. Providers
3. Keys
4. Profiles
5. Launch
6. Security Doctor
7. Audit
8. Settings
9. Help

## 17.3 Dashboard

Shows:

* Vault status: locked/unlocked
* Number of providers
* Number of keys
* Number of profiles
* Recent profile usage
* Security status
* Warnings

Example:

```text
┌ AegisKeys ─ Secure Provider Vault ─────────────────────┐
│ Vault: Unlocked    Providers: 20    Keys: 7 Profiles: 5│
│ Doctor: WARN       Last Used: openrouter-main          │
├───────────────────────────────────────────────────────┤
│ Quick Actions                                          │
│ > Launch Tool                                          │
│   Add API Key                                          │
│   Create Profile                                       │
│   Run Doctor                                           │
└───────────────────────────────────────────────────────┘
```

## 17.4 Providers Screen

Features:

* List providers
* Search/filter
* Add custom provider
* Edit provider
* View provider details
* Show configured/missing key status

Provider badges:

```text
[OpenAI]       openai        coding paid
[OpenRouter]  openrouter    router coding free-tier paid
[Ollama]      ollama        local no-key
```

## 17.5 Keys Screen

Features:

* List keys by provider
* Add key
* Rotate key
* Delete key
* Show masked value only
* Show last used timestamp
* Never reveal full key in normal UI

Example:

```text
OpenRouter
  main       sk-or-v1-...91ef       last used: today
  backup     sk-or-v1-...4acd       last used: never

Anthropic
  claude     sk-ant-...0192         last used: yesterday
```

## 17.6 Profiles Screen

Features:

* List profiles
* Create profile
* Edit profile
* Delete profile
* Validate profile
* Show provider and masked key

Example:

```text
or-main
  Provider: OpenRouter
  Key: main / sk-or-v1-...91ef
  Compatibility: OpenAI

anthropic-main
  Provider: Anthropic
  Key: claude / sk-ant-...0192
```

## 17.7 Launch Screen

Allows user to:

1. Select profile.
2. Enter command.
3. Preview injected env vars in masked form.
4. Launch child process.

Example:

```text
Profile: openrouter-main
Command: aider

Will inject:
  OPENROUTER_API_KEY=sk-or-v1-...91ef
  OPENAI_BASE_URL=https://openrouter.ai/api/v1

[Launch] [Cancel]
```

## 17.8 Security Doctor Screen

Runs same checks as CLI doctor.

Show:

* OK checks
* warnings
* failures
* suggested fixes

## 17.9 Audit Screen

Show recent audit events.

Never show secrets.

## 17.10 Settings Screen

Settings:

* auto-lock timeout
* preferred compatibility mode
* default launch profile
* whether risky exports are allowed
* whether envfile generation is allowed
* whether audit logging is enabled
* UI theme

---

# 18. CLI Commands

Implement the following:

```bash
aegiskeys init
aegiskeys tui
aegiskeys provider list
aegiskeys provider add
aegiskeys provider inspect <slug>
aegiskeys provider remove <slug>
aegiskeys key add
aegiskeys key list
aegiskeys key rotate --id <id>
aegiskeys key delete --id <id>
aegiskeys profile create
aegiskeys profile list
aegiskeys profile inspect <name>
aegiskeys profile delete <name>
aegiskeys run --profile <name> -- <command>
aegiskeys env --profile <name>
aegiskeys env --profile <name> --export
aegiskeys envfile --profile <name>
aegiskeys shred-envfile <path>
aegiskeys doctor
aegiskeys lock
aegiskeys unlock
aegiskeys audit
aegiskeys version
```

Command behavior must be useful even before TUI is perfect.

---

# 19. Recommended Go Project Structure

If implementing in Go, use this structure:

```text
.
├── go.mod
├── go.sum
├── main.go
├── README.md
├── .gitignore
├── cmd/
│   ├── root.go
│   ├── init.go
│   ├── tui.go
│   ├── provider.go
│   ├── key.go
│   ├── profile.go
│   ├── run.go
│   ├── env.go
│   ├── doctor.go
│   ├── lock.go
│   └── audit.go
├── internal/
│   ├── app/
│   │   └── app.go
│   ├── config/
│   │   ├── paths.go
│   │   └── config.go
│   ├── provider/
│   │   ├── provider.go
│   │   ├── registry.go
│   │   ├── defaults.go
│   │   └── validate.go
│   ├── secret/
│   │   ├── vault.go
│   │   ├── crypto.go
│   │   ├── mask.go
│   │   └── keyring.go
│   ├── profile/
│   │   ├── profile.go
│   │   └── store.go
│   ├── runner/
│   │   ├── runner.go
│   │   └── env.go
│   ├── audit/
│   │   └── audit.go
│   ├── security/
│   │   ├── doctor.go
│   │   ├── permissions.go
│   │   ├── scan.go
│   │   └── redact.go
│   └── tui/
│       ├── model.go
│       ├── styles.go
│       ├── dashboard.go
│       ├── providers.go
│       ├── keys.go
│       ├── profiles.go
│       ├── launch.go
│       ├── doctor.go
│       ├── audit.go
│       └── help.go
├── docs/
│   ├── threat-model.md
│   ├── providers.md
│   └── security.md
└── testdata/
    └── fake_vault.json
```

---

# 20. Data Models

## Provider

```go
type Provider struct {
    Name       string            `json:"name"`
    Slug       string            `json:"slug"`
    BaseURL    string            `json:"base_url"`
    EnvVar     string            `json:"env_var"`
    AuthHeader string            `json:"auth_header"`
    Models     []string          `json:"models,omitempty"`
    Tags       []string          `json:"tags,omitempty"`
    Notes      string            `json:"notes,omitempty"`
    ExtraEnv   map[string]string `json:"extra_env,omitempty"`
    CreatedAt  time.Time         `json:"created_at"`
    UpdatedAt  time.Time         `json:"updated_at"`
}
```

## Secret Record

```go
type SecretRecord struct {
    ID           string    `json:"id"`
    ProviderSlug string   `json:"provider_slug"`
    Label        string   `json:"label"`
    Secret       string   `json:"secret"`
    CreatedAt    time.Time `json:"created_at"`
    UpdatedAt    time.Time `json:"updated_at"`
    LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
    Tags         []string  `json:"tags,omitempty"`
}
```

## Vault

```go
type Vault struct {
    Version int            `json:"version"`
    Keys    []SecretRecord `json:"keys"`
}
```

## Vault Envelope

```go
type VaultEnvelope struct {
    Version    int            `json:"version"`
    KDF        KDFParams      `json:"kdf"`
    Cipher     CipherParams   `json:"cipher"`
    Ciphertext string         `json:"ciphertext"`
}
```

## Profile

```go
type Profile struct {
    Name         string            `json:"name"`
    ProviderSlug string           `json:"provider_slug"`
    KeyID        string           `json:"key_id"`
    Env          map[string]string `json:"env,omitempty"`
    Aliases      []string          `json:"aliases,omitempty"`
    Notes        string           `json:"notes,omitempty"`
    CreatedAt    time.Time        `json:"created_at"`
    UpdatedAt    time.Time        `json:"updated_at"`
}
```

## Audit Event

```go
type AuditEvent struct {
    Time     time.Time         `json:"time"`
    Event    string           `json:"event"`
    Provider string           `json:"provider,omitempty"`
    Profile  string           `json:"profile,omitempty"`
    Command  string           `json:"command,omitempty"`
    Metadata map[string]string `json:"metadata,omitempty"`
}
```

---

# 21. Provider Defaults

Seed this provider set.

## OpenAI

```text
name: OpenAI
slug: openai
base_url: https://api.openai.com/v1
env_var: OPENAI_API_KEY
auth_header: Authorization: Bearer ${KEY}
tags: coding, chat, image, paid
```

## Anthropic

```text
name: Anthropic
slug: anthropic
base_url: https://api.anthropic.com
env_var: ANTHROPIC_API_KEY
auth_header: x-api-key: ${KEY}
tags: coding, chat, paid
```

## OpenRouter

```text
name: OpenRouter
slug: openrouter
base_url: https://openrouter.ai/api/v1
env_var: OPENROUTER_API_KEY
auth_header: Authorization: Bearer ${KEY}
extra_env:
  OPENAI_BASE_URL: https://openrouter.ai/api/v1
tags: router, coding, chat, paid, free-tier
```

## Mistral

```text
name: Mistral
slug: mistral
base_url: https://api.mistral.ai/v1
env_var: MISTRAL_API_KEY
auth_header: Authorization: Bearer ${KEY}
tags: coding, chat, paid, free-tier
```

## Google Gemini

```text
name: Google Gemini
slug: gemini
base_url: https://generativelanguage.googleapis.com
env_var: GEMINI_API_KEY
auth_header: key query parameter or provider-specific
tags: coding, chat, multimodal, paid, free-tier
```

## Hugging Face

```text
name: Hugging Face
slug: huggingface
base_url: https://router.huggingface.co/v1
env_var: HF_TOKEN
auth_header: Authorization: Bearer ${KEY}
tags: router, coding, chat, paid, free-tier
```

## Groq

```text
name: Groq
slug: groq
base_url: https://api.groq.com/openai/v1
env_var: GROQ_API_KEY
auth_header: Authorization: Bearer ${KEY}
extra_env:
  OPENAI_BASE_URL: https://api.groq.com/openai/v1
tags: fast, chat, coding, paid, free-tier
```

## Cerebras

```text
name: Cerebras
slug: cerebras
base_url: https://api.cerebras.ai/v1
env_var: CEREBRAS_API_KEY
auth_header: Authorization: Bearer ${KEY}
tags: fast, chat, coding
```

## Together

```text
name: Together
slug: together
base_url: https://api.together.xyz/v1
env_var: TOGETHER_API_KEY
auth_header: Authorization: Bearer ${KEY}
extra_env:
  OPENAI_BASE_URL: https://api.together.xyz/v1
tags: router, openai-compatible, coding, paid
```

## Fireworks

```text
name: Fireworks
slug: fireworks
base_url: https://api.fireworks.ai/inference/v1
env_var: FIREWORKS_API_KEY
auth_header: Authorization: Bearer ${KEY}
extra_env:
  OPENAI_BASE_URL: https://api.fireworks.ai/inference/v1
tags: router, openai-compatible, coding, paid
```

## DeepSeek

```text
name: DeepSeek
slug: deepseek
base_url: https://api.deepseek.com/v1
env_var: DEEPSEEK_API_KEY
auth_header: Authorization: Bearer ${KEY}
extra_env:
  OPENAI_BASE_URL: https://api.deepseek.com/v1
tags: coding, chat, paid
```

## Zhipu / GLM

```text
name: Zhipu / GLM
slug: zhipu
base_url: https://open.bigmodel.cn/api/paas/v4
env_var: ZHIPU_API_KEY
auth_header: Authorization: Bearer ${KEY}
tags: coding, chat, paid, free-tier
```

## Moonshot / Kimi

```text
name: Moonshot / Kimi
slug: moonshot
base_url: https://api.moonshot.ai/v1
env_var: MOONSHOT_API_KEY
auth_header: Authorization: Bearer ${KEY}
extra_env:
  OPENAI_BASE_URL: https://api.moonshot.ai/v1
tags: coding, chat, paid
```

## Qwen / DashScope

```text
name: Qwen / DashScope
slug: qwen
base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
env_var: DASHSCOPE_API_KEY
auth_header: Authorization: Bearer ${KEY}
extra_env:
  OPENAI_BASE_URL: https://dashscope.aliyuncs.com/compatible-mode/v1
tags: coding, chat, paid, free-tier
```

## ModelScope

```text
name: ModelScope
slug: modelscope
base_url: https://api-inference.modelscope.cn/v1
env_var: MODELSCOPE_API_KEY
auth_header: Authorization: Bearer ${KEY}
extra_env:
  OPENAI_BASE_URL: https://api-inference.modelscope.cn/v1
tags: coding, chat, free-tier
```

## Nvidia NIM

```text
name: Nvidia NIM
slug: nvidia-nim
base_url: https://integrate.api.nvidia.com/v1
env_var: NVIDIA_API_KEY
auth_header: Authorization: Bearer ${KEY}
extra_env:
  OPENAI_BASE_URL: https://integrate.api.nvidia.com/v1
tags: openai-compatible, coding, chat, paid, free-tier
```

## Cloudflare Workers AI

```text
name: Cloudflare Workers AI
slug: cloudflare
base_url: https://api.cloudflare.com/client/v4/accounts/${ACCOUNT_ID}/ai
env_var: CLOUDFLARE_API_TOKEN
auth_header: Authorization: Bearer ${KEY}
tags: edge, workers, ai, paid, free-tier
```

## Ollama

```text
name: Ollama
slug: ollama
base_url: http://localhost:11434/v1
env_var: ""
auth_header: none
extra_env:
  OPENAI_BASE_URL: http://localhost:11434/v1
tags: local, no-key, openai-compatible
```

## LM Studio

```text
name: LM Studio
slug: lmstudio
base_url: http://localhost:1234/v1
env_var: ""
auth_header: none
extra_env:
  OPENAI_BASE_URL: http://localhost:1234/v1
tags: local, no-key, openai-compatible
```

---

# 22. Runner Requirements

The runner must:

1. Accept command and args after `--`.
2. Preserve current environment.
3. Add provider/profile env vars.
4. Avoid printing secrets.
5. Stream stdin/stdout/stderr directly.
6. Return child process exit code.
7. Update last-used metadata.
8. Write audit event.

Example:

```bash
aegiskeys run --profile or-main -- aider --model openrouter/anthropic/claude-sonnet-4
```

If command is missing:

```text
No command provided.

Usage:
  aegiskeys run --profile <name> -- <command> [args...]
```

If profile is invalid:

```text
Profile "or-main" is invalid:
  missing key key_123
Run:
  aegiskeys profile list
  aegiskeys key list
```

---

# 23. Redaction

Implement central redaction utilities.

They should redact:

* full API keys
* bearer tokens
* env assignment values
* known secret patterns
* values associated with keys like `api_key`, `token`, `secret`, `password`

Example:

Input:

```text
OPENAI_API_KEY=sk-abc123
Authorization: Bearer sk-abc123
```

Output:

```text
OPENAI_API_KEY=<redacted>
Authorization: Bearer <redacted>
```

All logging and error output should pass through redaction where practical.

---

# 24. Tests

Add tests for:

## Secret

* encryption/decryption round trip
* wrong password fails
* tampered ciphertext fails
* secret masking never returns full secret
* short secrets become `<hidden>`

## Provider

* default providers load
* provider slugs unique
* provider validation rejects invalid env var
* provider metadata scanner detects secret-looking values

## Profile

* profile validates with existing provider/key
* profile fails with missing provider
* profile fails with missing key
* alias collision detection

## Runner

* env injection includes expected provider env var
* env injection includes base URL where appropriate
* child process does not receive unrelated secrets
* command args after `--` are preserved

## Doctor

* unsafe file permissions detected
* missing `.gitignore` warning
* plaintext `.env` warning
* missing key warning

## Redaction

* API keys redacted from strings
* Authorization headers redacted
* env var assignments redacted

---

# 25. README Requirements

Create a high-quality `README.md` with:

1. Project name and tagline
2. What AegisKeys does
3. Why it exists
4. Security model
5. Threat model
6. Install/build instructions
7. Quickstart
8. TUI usage
9. CLI examples
10. Provider/key/profile explanation
11. Safe launch examples
12. Doctor command explanation
13. How to add custom providers
14. How to use OpenAI-compatible providers
15. How to use local providers like Ollama
16. Limitations
17. Future work

Quickstart example:

```bash
go build -o aegiskeys .

./aegiskeys init
./aegiskeys key add
./aegiskeys profile create
./aegiskeys run --profile openrouter-main -- aider
```

---

# 26. MVP Acceptance Criteria

The MVP is acceptable only if:

1. The project builds.
2. `aegiskeys init` creates config, provider registry, profiles file, and encrypted vault.
3. Provider defaults are loaded.
4. User can add an API key.
5. API key is encrypted at rest.
6. User can create a profile.
7. User can run a command with profile-scoped env injection.
8. Secrets are masked in normal output.
9. `aegiskeys doctor` runs useful checks.
10. TUI opens and provides at least dashboard/provider/key/profile/launch views.
11. README explains usage and security.
12. Tests exist and pass for core security behavior.

---

# 27. Future Work

Add a `docs/future-work.md` file with:

* OS keyring integration improvements
* hardware-backed secret support
* YubiKey/passkey unlock
* per-profile policy rules
* provider health checks
* model catalog refresh
* secure import/export
* encrypted backup
* team mode with public-key sharing
* shell plugin integration
* agent-specific launch presets
* secret age/rotation reminders
* automatic stale temporary env cleanup
* richer TUI themes
* audit viewer filters
* policy engine for dangerous export behavior

---

# 28. Implementation Order

Build in this order:

1. Project skeleton
2. Config paths and permissions
3. Provider registry and defaults
4. Secret masking and redaction
5. Encrypted vault
6. Key add/list
7. Profile create/list
8. Runner env injection
9. Doctor
10. CLI polish
11. Basic TUI
12. README
13. Tests
14. Final verification

Do not spend all time on the TUI first. The secure core must work.

---

# 29. Final Report Required

At the end of implementation, report:

1. Files created/changed
2. Build command
3. Test command
4. How to initialize AegisKeys
5. How to add a key
6. How to create a profile
7. How to launch a coding tool with injected secrets
8. How to open the TUI
9. What security protections are implemented
10. What remains future work

---

# 30. Immediate Instruction to Coding Agent

Start implementing AegisKeys now.

Do not ask more clarification questions.

Make reasonable decisions.

Prioritize a working secure MVP over theoretical perfection.

The final result must be modular, functional, secure by default, visually polished, and useful for real developer workflows.

