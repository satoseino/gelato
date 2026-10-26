# Add gelato's own default regex rule set

## Background

Users' configs set `regex = true` (e.g. `jarvis-registry/tartufo.toml`), and tartufo then scans with its 39 built-in rules from `tartufo/tartufo/data/default_regexes.json`. That file is GPL-2.0-only data inherited from truffleHog, so gelato (MIT) must not copy it (see `CLAUDE.md`, "Project constraints"). Spec 2 provides `detect.Rule` and an engine that runs rules but ships none. This spec adds a small, high-precision default set written from token formats the providers publish.

---

## Changes

### 1. `detect/rules.go` — `func DefaultRules() []Rule`

- Rules are Go literals in a package-level slice compiled with `regexp.MustCompile`; `DefaultRules` returns a copy so callers can't mutate the shared slice. No data file or parser: the set is small and changes only with code review.
- Each rule has at most one capture group, which marks the secret part when the pattern needs surrounding context (spec 2 reports group 1). A secret's signature therefore covers only the secret, not its surrounding text.
- Each rule has a comment linking the provider documentation its format came from. **Implementation step:** verify every format below against that documentation before committing; the patterns here are the intended starting point, not verified facts.
- `PathPattern` is `nil` for every rule (no rule is file-type specific).

Starting set (names are user-visible in output as `Detail`):

| Name | Pattern (Go RE2) |
|---|---|
| `AWS access key ID` | `\b((?:AKIA\|ASIA\|ABIA\|ACCA)[A-Z2-7]{16})\b` |
| `AWS secret access key` | `(?i)aws_?secret_?access_?key["']?\s*[:=]\s*["']?([A-Za-z0-9/+]{40})\b` |
| `GitHub token` | `\b(gh[pousr]_[A-Za-z0-9]{36,255})\b` |
| `GitHub fine-grained token` | `\b(github_pat_[A-Za-z0-9_]{82})\b` |
| `GitLab token` | `\b(glpat-[A-Za-z0-9_-]{20,})\b` |
| `Slack token` | `\b(xox[abeoprs]-[A-Za-z0-9-]{10,})\b` |
| `Slack webhook` | `https://hooks\.slack\.com/services/T[A-Z0-9]+/B[A-Z0-9]+/[A-Za-z0-9]{24}` |
| `Google API key` | `\b(AIza[A-Za-z0-9_-]{35})` |
| `Google OAuth client secret` | `\b(GOCSPX-[A-Za-z0-9_-]{28})` |
| `Google service account` | `"type"\s*:\s*"service_account"` |
| `Private key` | `-----BEGIN (?:RSA \|DSA \|EC \|OPENSSH \|PGP \|ENCRYPTED )?PRIVATE KEY(?: BLOCK)?-----` |
| `Stripe secret key` | `\b((?:sk\|rk)_live_[A-Za-z0-9]{24,})\b` |
| `Twilio API key` | `\b(SK[0-9a-f]{32})\b` |
| `SendGrid API key` | `\b(SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43})` |
| `npm token` | `\b(npm_[A-Za-z0-9]{36})\b` |
| `PyPI token` | `\b(pypi-AgEIcHlwaS5vcmc[A-Za-z0-9_-]{50,})` |
| `OpenAI API key` | `\b(sk-(?:proj-\|svcacct-\|admin-)?[A-Za-z0-9_-]{20,}T3BlbkFJ[A-Za-z0-9_-]{20,})` |
| `Anthropic API key` | `\b(sk-ant-[a-z]+\d{2}-[A-Za-z0-9_-]{80,})` |
| `Hugging Face token` | `\b(hf_[A-Za-z]{34})\b` |
| `Azure storage account key` | `AccountKey=([A-Za-z0-9+/]{86}==)` |
| `Password in URL` | `\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:([^\s:/@$<{][^\s:/@]{2,})@` |

(`\|` above is table escaping; the Go source uses a plain `|`.)

`Password in URL` skips passwords starting with `$`, `<` or `{`, so templates such as `mongodb://user:${MONGO_PASSWORD}@host` and `://user:<password>@` don't match.

### 2. Tests that never commit a token-shaped string — `detect/rules_test.go`

- Every rule has at least one positive and one negative case, table-driven, run through `detect.New(cfg, detect.DefaultRules())` so they exercise spec 2's group extraction and signatures.
- **Positive fixtures are built at runtime from fragments** (e.g. `"AK" + "IA" + strings.Repeat("Q", 16)`). A contiguous token-shaped literal in the repository would be flagged by GitHub push protection and by gelato's own hook, and committing such strings is exactly the habit this tool exists to stop.
- Negative cases cover the near misses each pattern must reject: wrong prefix, one character short, `Password in URL` templates (`${…}`, `<…>`), a URL with only a username, and the Twilio-style 34-character `AC…` account SID (not a secret).
- A test asserts every rule name is unique and every pattern has at most one capture group (also guarded by `detect.New`).

### 3. Mention the rule set in `CLAUDE.md`

In "Project constraints", replace "The default regex rules are our own." with "The default regex rules (`detect/rules.go`) are our own; each cites the provider documentation for its format, and test fixtures are assembled from fragments at runtime so the repository never contains a token-shaped string."

---

## What does NOT change

- No user-defined rules (`rule-patterns`) and no way to turn the defaults off; `regex = false` remains the only switch.
- The engine's matching semantics (spec 2).

---

## Acceptance Criteria

- [ ] `detect.DefaultRules()` returns the rules above, each with a comment linking its provider documentation.
- [ ] Every rule has a passing positive and negative table case in `detect/rules_test.go`.
- [ ] `git -c submodule.recurse=false grep --untracked -nE 'AKIA[A-Z2-7]{16}|gh[pousr]_[A-Za-z0-9]{36}|xox[abeoprs]-[A-Za-z0-9-]{10}|AIza[A-Za-z0-9_-]{35}'` finds nothing (tracked and untracked files, all types, ignored paths such as `tartufo/` excluded; `submodule.recurse=false` because `--untracked` refuses to run when a user's config enables submodule recursion).
- [ ] Mutating the slice returned by `DefaultRules()` does not affect a later call.
- [ ] `make all` passes.

---

## Files to Change

| File | Change |
|---|---|
| `detect/rules.go` | `DefaultRules` and the rule literals with documentation links |
| `detect/rules_test.go` | Positive/negative cases built from fragments; uniqueness and group-count checks |
| `CLAUDE.md` | Updated "default regex rules" constraint line |

---

## Risk

A loose pattern blocks commits with false positives, and a tight one misses real tokens. The negative cases and the parity run on jarvis-registry's history in spec 7 are where noisy rules show up before release.

---

## Out of scope / follow-up

- Compared with tartufo's set, this drops vague context rules (`Generic Secret`, `Generic API Key`, `Facebook OAuth`, `GitHub` keyword rules) that rely on keywords plus any 32–45 character string. Entropy scanning still covers most of what they caught.
