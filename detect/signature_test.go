package detect_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/satoseino/gelato/detect"
)

func TestSignature(t *testing.T) {
	const want = "0e027678bd911189618fd0c11817c6900877b3933bcd54dea457f5e7029acebe"

	if got := detect.Signature("not-a-real-secret", "config/app.py"); got != want {
		t.Errorf("Signature() = %s, want %s", got, want)
	}
}

// TestSignatureGoldens checks Signature against digests computed with
// Python's hashlib.blake2s, as tartufo computes them.
func TestSignatureGoldens(t *testing.T) {
	data, err := os.ReadFile("testdata/signatures.json")
	if err != nil {
		t.Fatal(err)
	}

	var goldens []struct {
		Match     string `json:"match"`
		Path      string `json:"path"`
		Signature string `json:"signature"`
	}

	if err := json.Unmarshal(data, &goldens); err != nil {
		t.Fatal(err)
	}

	if len(goldens) == 0 {
		t.Fatal("no goldens")
	}

	for _, g := range goldens {
		if got := detect.Signature(g.Match, g.Path); got != g.Signature {
			t.Errorf("Signature(%q, %q) = %s, want %s", g.Match, g.Path, got, g.Signature)
		}
	}
}
