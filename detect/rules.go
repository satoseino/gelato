package detect

import (
	"regexp"
	"slices"
)

// defaultRules is gelato's built-in rule set, written from the token formats
// each provider documents. A pattern with a capture group reports only the
// group, so a finding's signature covers the secret and not the text around
// it.
var defaultRules = []Rule{
	{
		// https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_identifiers.html#identifiers-unique-ids
		// AKIA long-term, ASIA temporary (STS), ABIA STS bearer token, ACCA
		// context-specific credential; base32 body.
		Name:    "AWS access key ID",
		Pattern: regexp.MustCompile(`\b((?:AKIA|ASIA|ABIA|ACCA)[A-Z2-7]{16})\b`),
	},
	{
		// https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_access-keys.html
		// The key has no prefix, so it needs its variable name as context. It
		// can end in "/" or "+", where \b would not hold, so the end is any
		// character outside the key's alphabet.
		Name:    "AWS secret access key",
		Pattern: regexp.MustCompile(`(?i)aws_?secret_?access_?key["']?\s*[:=]\s*["']?([A-Za-z0-9/+]{40})(?:[^A-Za-z0-9/+]|$)`),
	},
	{
		// https://github.blog/engineering/platform-security/behind-githubs-new-authentication-token-formats/
		// https://docs.github.com/en/organizations/managing-programmatic-access-to-your-organization/github-credential-types
		Name:    "GitHub token",
		Pattern: regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{36,255})\b`),
	},
	{
		// https://docs.github.com/en/organizations/managing-programmatic-access-to-your-organization/github-credential-types
		Name:    "GitHub fine-grained token",
		Pattern: regexp.MustCompile(`\b(github_pat_[A-Za-z0-9_]{82})\b`),
	},
	{
		// https://docs.gitlab.com/security/tokens/#token-prefixes
		// Personal, project, group and impersonation tokens share glpat-.
		Name:    "GitLab token",
		Pattern: regexp.MustCompile(`\b(glpat-[A-Za-z0-9_-]{20,})\b`),
	},
	{
		// https://api.slack.com/authentication/token-types
		Name:    "Slack token",
		Pattern: regexp.MustCompile(`\b(xox[abeoprs]-[A-Za-z0-9-]{10,})\b`),
	},
	{
		// https://api.slack.com/messaging/webhooks
		// The whole URL is the secret.
		Name:    "Slack webhook",
		Pattern: regexp.MustCompile(`https://hooks\.slack\.com/services/T[A-Z0-9]+/B[A-Z0-9]+/[A-Za-z0-9]{24}`),
	},
	{
		// https://cloud.google.com/docs/authentication/api-keys
		Name:    "Google API key",
		Pattern: regexp.MustCompile(`\b(AIza[A-Za-z0-9_-]{35})`),
	},
	{
		// https://developers.googleblog.com/usability-and-safety-updates-to-google-auth-platform/
		Name:    "Google OAuth client secret",
		Pattern: regexp.MustCompile(`\b(GOCSPX-[A-Za-z0-9_-]{28})`),
	},
	{
		// https://cloud.google.com/iam/docs/keys-create-delete
		// A service account key file is JSON whose "type" is
		// "service_account".
		Name:    "Google service account",
		Pattern: regexp.MustCompile(`"type"\s*:\s*"service_account"`),
	},
	{
		// https://www.rfc-editor.org/rfc/rfc7468 (PEM labels),
		// https://www.rfc-editor.org/rfc/rfc4880#section-6.2 (PGP armor),
		// https://cvsweb.openbsd.org/src/usr.bin/ssh/PROTOCOL.key (OpenSSH)
		Name:    "Private key",
		Pattern: regexp.MustCompile(`-----BEGIN (?:RSA |DSA |EC |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY(?: BLOCK)?-----`),
	},
	{
		// https://docs.stripe.com/keys
		// Secret (sk_) and restricted (rk_) live-mode keys; test-mode keys
		// grant no access to real data.
		Name:    "Stripe secret key",
		Pattern: regexp.MustCompile(`\b((?:sk|rk)_live_[A-Za-z0-9]{24,})\b`),
	},
	{
		// https://www.twilio.com/docs/iam/api-keys/key-resource-v1
		// https://www.twilio.com/docs/glossary/what-is-a-sid
		Name:    "Twilio API key",
		Pattern: regexp.MustCompile(`\b(SK[0-9a-fA-F]{32})\b`),
	},
	{
		// https://www.twilio.com/docs/sendgrid/ui/account-and-settings/api-keys
		// https://support.sendgrid.com/hc/en-us/articles/44146758703387
		// Always 69 characters: SG., 22, ".", 43.
		Name:    "SendGrid API key",
		Pattern: regexp.MustCompile(`\b(SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43})`),
	},
	{
		// https://github.blog/security/announcing-npms-new-access-token-format/
		Name:    "npm token",
		Pattern: regexp.MustCompile(`\b(npm_[A-Za-z0-9]{36})\b`),
	},
	{
		// https://docs.pypi.org/api/secrets/
		// pypi- then a base64 macaroon of at least 85 characters, which for
		// pypi.org always begins by encoding the location "pypi.org".
		Name:    "PyPI token",
		Pattern: regexp.MustCompile(`\b(pypi-AgEIcHlwaS5vcmc[A-Za-z0-9_-]{70,})`),
	},
	{
		// https://platform.openai.com/docs/api-reference/authentication
		// Keys embed "T3BlbkFJ", base64 for "OpenAI".
		Name:    "OpenAI API key",
		Pattern: regexp.MustCompile(`\b(sk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_-]{20,}T3BlbkFJ[A-Za-z0-9_-]{20,})`),
	},
	{
		// https://docs.anthropic.com/en/api/overview
		// sk-ant- then a versioned kind such as api03 or admin01.
		Name:    "Anthropic API key",
		Pattern: regexp.MustCompile(`\b(sk-ant-[a-z]+\d{2}-[A-Za-z0-9_-]{80,})`),
	},
	{
		// https://huggingface.co/docs/hub/security-tokens
		Name:    "Hugging Face token",
		Pattern: regexp.MustCompile(`\b(hf_[A-Za-z]{34})\b`),
	},
	{
		// https://learn.microsoft.com/en-us/azure/storage/common/storage-configure-connection-string
		// A 64-byte key, base64-encoded to 88 characters.
		Name:    "Azure storage account key",
		Pattern: regexp.MustCompile(`AccountKey=([A-Za-z0-9+/]{86}==)`),
	},
	{
		// https://www.rfc-editor.org/rfc/rfc3986#section-3.2.1
		// Passwords starting with "$", "<" or "{" are templates such as
		// ${DB_PASSWORD} or <password>, not secrets.
		Name:    "Password in URL",
		Pattern: regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:([^\s:/@$<{][^\s:/@]{2,})@`),
	},
}

// DefaultRules returns gelato's built-in rules, the ones `regex = true`
// scans with. Each call returns a new slice, so changing it doesn't affect
// later calls.
func DefaultRules() []Rule {
	return slices.Clone(defaultRules)
}
