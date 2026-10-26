package detect

import (
	"slices"
	"strings"
	"testing"

	"github.com/satoseino/gelato/cfg"
)

// Token alphabets, assembled at runtime like every fixture in this file:
// a contiguous token-shaped literal would be flagged by GitHub push
// protection and by gelato's own hook.
var (
	upper    = runeRange('A', 'Z')
	lower    = runeRange('a', 'z')
	digits   = runeRange('0', '9')
	letters  = upper + lower
	alnum    = letters + digits
	base32   = upper + runeRange('2', '7')
	b64      = alnum + "+/"
	urlSafe  = alnum + "_-"
	lowerHex = digits + runeRange('a', 'f')
)

// cycle returns the first n runes of alphabet repeated, starting at its
// first rune. alphabet must be ASCII.
func cycle(alphabet string, n int) string {
	return strings.Repeat(alphabet, n/len(alphabet)+1)[:n]
}

func TestDefaultRules(t *testing.T) {
	awsKeyID := "AK" + "IA" + cycle(base32, 16)
	awsSecret := cycle(b64, 40)
	awsSecretSlash := cycle(alnum, 39) + "/"
	ghp := "gh" + "p_" + cycle(alnum, 36)
	ghs := "gh" + "s_" + cycle(alnum, 40)
	ghPAT := "github" + "_pat_" + cycle(alnum+"_", 82)
	glpat := "gl" + "pat-" + cycle(urlSafe, 20)
	slackBot := "xo" + "xb-" + digits + "-" + cycle(alnum, 24)
	slackHook := "https://hooks.slack.com/services/" + "T0" + cycle(upper, 8) + "/B0" + cycle(upper, 8) + "/" + cycle(alnum, 24)
	googleKey := "AI" + "za" + cycle(urlSafe, 35)
	gocspx := "GOC" + "SPX-" + cycle(urlSafe, 28)
	privateKey := "-----BEGIN " + "PRIV" + "ATE KEY-----"
	rsaKey := "-----BEGIN RSA " + "PRIV" + "ATE KEY-----"
	pgpKey := "-----BEGIN PGP " + "PRIV" + "ATE KEY BLOCK-----"
	stripe := "sk" + "_live_" + cycle(alnum, 24)
	stripeRestricted := "rk" + "_live_" + cycle(alnum, 99)
	twilio := "S" + "K" + cycle(lowerHex, 32)
	sendgrid := "S" + "G." + cycle(urlSafe, 22) + "." + cycle(urlSafe, 43)
	npm := "np" + "m_" + cycle(alnum, 36)
	pypi := "py" + "pi-" + "AgEIcHlwaS5vcmc" + cycle(urlSafe, 70)
	openAIWatermark := "T3Blb" + "kFJ"
	openAIProject := "sk-" + "proj-" + cycle(urlSafe, 74) + openAIWatermark + cycle(urlSafe, 74)
	openAILegacy := "sk-" + cycle(alnum, 20) + openAIWatermark + cycle(alnum, 20)
	anthropic := "sk-" + "ant-" + "api03-" + cycle(urlSafe, 93) + "AA"
	hf := "h" + "f_" + cycle(letters, 34)
	azureKey := cycle(b64, 86) + "=="
	serviceAccount := `"type": "service_` + `account"`

	tests := []struct {
		rule string
		name string
		text string
		// want is the rule's matches; nil for a negative case.
		want []string
	}{
		{rule: "AWS access key ID", name: "long-term key", text: "aws_access_key_id = " + awsKeyID, want: []string{awsKeyID}},
		{rule: "AWS access key ID", name: "temporary key", text: "AS" + "IA" + cycle(base32, 16), want: []string{"AS" + "IA" + cycle(base32, 16)}},
		{rule: "AWS access key ID", name: "one rune short", text: "AK" + "IA" + cycle(base32, 15)},
		{rule: "AWS access key ID", name: "one rune long", text: awsKeyID + "A"},
		{rule: "AWS access key ID", name: "IAM user ID prefix", text: "AI" + "DA" + cycle(base32, 16)},
		{rule: "AWS access key ID", name: "outside base32", text: "AK" + "IA" + cycle(base32, 15) + "1"},

		{rule: "AWS secret access key", name: "quoted", text: `aws_secret_access_key = "` + awsSecret + `"`, want: []string{awsSecret}},
		{rule: "AWS secret access key", name: "env var at end of text", text: "AWS_SECRET_ACCESS_KEY=" + awsSecret, want: []string{awsSecret}},
		{rule: "AWS secret access key", name: "ends in slash", text: `awsSecretAccessKey: "` + awsSecretSlash + `"`, want: []string{awsSecretSlash}},
		{rule: "AWS secret access key", name: "one rune short", text: "aws_secret_access_key = " + awsSecret[:39]},
		{rule: "AWS secret access key", name: "one rune long", text: "aws_secret_access_key = " + awsSecret + "A"},
		{rule: "AWS secret access key", name: "no key name", text: "secret = " + awsSecret},

		{rule: "GitHub token", name: "classic PAT", text: "token: " + ghp, want: []string{ghp}},
		{rule: "GitHub token", name: "installation token", text: ghs, want: []string{ghs}},
		{rule: "GitHub token", name: "unknown type letter", text: "gh" + "x_" + cycle(alnum, 36)},
		{rule: "GitHub token", name: "one rune short", text: "gh" + "p_" + cycle(alnum, 35)},

		{rule: "GitHub fine-grained token", name: "PAT", text: "GH_TOKEN=" + ghPAT, want: []string{ghPAT}},
		{rule: "GitHub fine-grained token", name: "one rune short", text: ghPAT[:len(ghPAT)-1]},
		{rule: "GitHub fine-grained token", name: "wrong prefix", text: "github" + "_pak_" + cycle(alnum, 82)},

		{rule: "GitLab token", name: "PAT", text: "GITLAB_TOKEN=" + glpat, want: []string{glpat}},
		{rule: "GitLab token", name: "one rune short", text: glpat[:len(glpat)-1]},
		{rule: "GitLab token", name: "deploy token prefix", text: "gl" + "dt-" + cycle(urlSafe, 20)},

		{rule: "Slack token", name: "bot token", text: "SLACK_BOT_TOKEN=" + slackBot, want: []string{slackBot}},
		{rule: "Slack token", name: "unknown type letter", text: "xo" + "xz-" + digits + "-" + cycle(alnum, 24)},
		{rule: "Slack token", name: "one rune short", text: "xo" + "xp-" + cycle(alnum, 9)},

		{rule: "Slack webhook", name: "webhook URL", text: "url: " + slackHook, want: []string{slackHook}},
		{rule: "Slack webhook", name: "secret one rune short", text: slackHook[:len(slackHook)-1]},
		{rule: "Slack webhook", name: "API URL", text: "https://hooks.slack.com/commands/" + cycle(alnum, 24)},

		{rule: "Google API key", name: "key", text: `key: "` + googleKey + `"`, want: []string{googleKey}},
		{rule: "Google API key", name: "one rune short", text: googleKey[:len(googleKey)-1]},
		{rule: "Google API key", name: "wrong prefix", text: "AI" + "zb" + cycle(urlSafe, 35)},

		{rule: "Google OAuth client secret", name: "secret", text: `"client_secret": "` + gocspx + `"`, want: []string{gocspx}},
		{rule: "Google OAuth client secret", name: "one rune short", text: gocspx[:len(gocspx)-1]},
		{rule: "Google OAuth client secret", name: "wrong prefix", text: "GOC" + "SPY-" + cycle(urlSafe, 28)},

		{
			rule: "Google service account", name: "key file",
			text: "{\n  " + serviceAccount + ",\n  \"project_id\": \"demo\"\n}",
			want: []string{serviceAccount},
		},
		{rule: "Google service account", name: "user credentials", text: `{"type": "authorized_user"}`},

		{rule: "Private key", name: "PKCS#8", text: privateKey + "\nMII", want: []string{privateKey}},
		{rule: "Private key", name: "RSA", text: rsaKey, want: []string{rsaKey}},
		{rule: "Private key", name: "PGP", text: pgpKey, want: []string{pgpKey}},
		{rule: "Private key", name: "public key", text: "-----BEGIN PUBLIC KEY-----"},
		{rule: "Private key", name: "certificate", text: "-----BEGIN CERTIFICATE-----"},

		{rule: "Stripe secret key", name: "secret key", text: "STRIPE_KEY=" + stripe, want: []string{stripe}},
		{rule: "Stripe secret key", name: "restricted key", text: stripeRestricted, want: []string{stripeRestricted}},
		{rule: "Stripe secret key", name: "test mode", text: "sk" + "_test_" + cycle(alnum, 24)},
		{rule: "Stripe secret key", name: "publishable key", text: "pk" + "_live_" + cycle(alnum, 24)},
		{rule: "Stripe secret key", name: "one rune short", text: stripe[:len(stripe)-1]},

		{rule: "Twilio API key", name: "API key SID", text: "TWILIO_API_KEY=" + twilio, want: []string{twilio}},
		{rule: "Twilio API key", name: "account SID", text: "A" + "C" + cycle(lowerHex, 32)},
		{rule: "Twilio API key", name: "one rune short", text: twilio[:len(twilio)-1]},
		{rule: "Twilio API key", name: "not hex", text: "S" + "K" + cycle(lowerHex, 31) + "g"},

		{rule: "SendGrid API key", name: "key", text: "SENDGRID_API_KEY=" + sendgrid, want: []string{sendgrid}},
		{rule: "SendGrid API key", name: "short ID segment", text: "S" + "G." + cycle(urlSafe, 21) + "." + cycle(urlSafe, 43)},
		{rule: "SendGrid API key", name: "short secret segment", text: sendgrid[:len(sendgrid)-1]},

		{rule: "npm token", name: "token", text: "//registry.npmjs.org/:_authToken=" + npm, want: []string{npm}},
		{rule: "npm token", name: "one rune short", text: npm[:len(npm)-1]},
		{rule: "npm token", name: "legacy UUID", text: "_authToken=" + cycle(lowerHex, 8) + "-" + cycle(lowerHex, 4) + "-" + cycle(lowerHex, 4)},

		{rule: "PyPI token", name: "token", text: "password = " + pypi, want: []string{pypi}},
		{rule: "PyPI token", name: "one rune short", text: pypi[:len(pypi)-1]},
		{rule: "PyPI token", name: "not a pypi.org macaroon", text: "py" + "pi-" + cycle(urlSafe, 100)},

		{rule: "OpenAI API key", name: "project key", text: "OPENAI_API_KEY=" + openAIProject, want: []string{openAIProject}},
		{rule: "OpenAI API key", name: "legacy key", text: openAILegacy, want: []string{openAILegacy}},
		{rule: "OpenAI API key", name: "no watermark", text: "sk-" + "proj-" + cycle(alnum, 156)},
		{rule: "OpenAI API key", name: "short tail", text: "sk-" + cycle(alnum, 20) + openAIWatermark + cycle(alnum, 19)},

		{rule: "Anthropic API key", name: "API key", text: "ANTHROPIC_API_KEY=" + anthropic, want: []string{anthropic}},
		{rule: "Anthropic API key", name: "short body", text: "sk-" + "ant-" + "api03-" + cycle(urlSafe, 79)},
		{rule: "Anthropic API key", name: "unversioned kind", text: "sk-" + "ant-" + "api-" + cycle(urlSafe, 95)},

		{rule: "Hugging Face token", name: "token", text: "HF_TOKEN=" + hf, want: []string{hf}},
		{rule: "Hugging Face token", name: "one rune short", text: hf[:len(hf)-1]},
		{rule: "Hugging Face token", name: "digit in body", text: hf[:len(hf)-1] + "1"},

		{
			rule: "Azure storage account key", name: "connection string",
			text: "DefaultEndpointsProtocol=https;AccountName=demo;AccountKey=" + azureKey + ";EndpointSuffix=core.windows.net",
			want: []string{azureKey},
		},
		{rule: "Azure storage account key", name: "one rune short", text: "AccountKey=" + azureKey[1:]},
		{rule: "Azure storage account key", name: "no padding", text: "AccountKey=" + cycle(b64, 88)},

		{rule: "Password in URL", name: "database URL", text: "postgres://admin:" + "hunter2pass" + "@db:5432/app", want: []string{"hunter2pass"}},
		{rule: "Password in URL", name: "scheme with plus", text: "mongodb+srv://app:" + "p4ss" + "@cluster0.example.net", want: []string{"p4ss"}},
		{rule: "Password in URL", name: "shell template", text: "mongodb://user:${MONGO_PASSWORD}@host"},
		{rule: "Password in URL", name: "placeholder", text: "https://user:<password>@example.com"},
		{rule: "Password in URL", name: "Jinja template", text: "redis://default:{{ redis_password }}@cache:6379"},
		{rule: "Password in URL", name: "printf template", text: "postgresql://app:" + "%(password)s" + "@db/app"},
		{rule: "Password in URL", name: "masked", text: "https://user:" + "********" + "@example.com"},
		{rule: "Password in URL", name: "placeholder word", text: "https://user:" + "password" + "@example.com", want: []string{"password"}},
		{rule: "Password in URL", name: "username only", text: "https://user@example.com/repo.git"},
		{rule: "Password in URL", name: "port, not password", text: "http://localhost:8080/path"},
		{rule: "Password in URL", name: "two-rune password", text: "ftp://user:" + "ab" + "@example.com"},
	}

	e, err := New(cfg.Config{Regex: true}, DefaultRules())
	if err != nil {
		t.Fatal(err)
	}

	positive, negative := map[string]bool{}, map[string]bool{}

	for _, tt := range tests {
		var got []string

		for _, i := range e.Scan(Chunk{Path: "config.env", Lines: strings.Split(tt.text, "\n")}) {
			if i.Detail == tt.rule {
				got = append(got, i.Match)
			}
		}

		if !slices.Equal(got, tt.want) {
			t.Errorf("%s / %s: matches = %q, want %q", tt.rule, tt.name, got, tt.want)
		}

		if tt.want != nil {
			positive[tt.rule] = true
		} else {
			negative[tt.rule] = true
		}
	}

	for _, r := range DefaultRules() {
		if !positive[r.Name] || !negative[r.Name] {
			t.Errorf("rule %q: has positive case %v, negative case %v; want both", r.Name, positive[r.Name], negative[r.Name])
		}
	}
}

func TestDefaultRulesWellFormed(t *testing.T) {
	seen := map[string]bool{}

	for _, r := range DefaultRules() {
		if r.Name == "" {
			t.Errorf("rule with pattern %q has no name", r.Pattern)
		}

		if seen[r.Name] {
			t.Errorf("rule %q: duplicate name", r.Name)
		}

		seen[r.Name] = true

		if n := r.Pattern.NumSubexp(); n > 1 {
			t.Errorf("rule %q: %d capture groups, want at most 1", r.Name, n)
		}

		if r.PathPattern != nil {
			t.Errorf("rule %q: PathPattern = %q, want nil", r.Name, r.PathPattern)
		}
	}
}

func TestDefaultRulesReturnsCopy(t *testing.T) {
	first := DefaultRules()
	want := slices.Clone(first)

	first[0] = Rule{Name: "mutated"}

	if got := DefaultRules(); !slices.Equal(got, want) {
		t.Errorf("DefaultRules() after mutating an earlier result = %v, want %v", got, want)
	}
}
