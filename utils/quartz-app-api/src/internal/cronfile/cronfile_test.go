package cronfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quartzwrt/quartz-app-api/internal/envelope"
)

func TestNormalize(t *testing.T) {
	if got := Normalize("  * * * * * echo hi\r\n\r\n"); got != "* * * * * echo hi\n" {
		t.Fatalf("normalize = %q", got)
	}
	if got := Normalize("  \r\n\t "); got != "" {
		t.Fatalf("empty normalize = %q", got)
	}
}

func TestValidate(t *testing.T) {
	if ee := Validate(Normalize("*/5 * * * * /usr/bin/true\n# comment\n")); ee != nil {
		t.Fatalf("valid crontab: %+v", ee)
	}
	if ee := Validate(""); ee != nil {
		t.Fatalf("empty crontab should be valid: %+v", ee)
	}
	if ee := Validate("a\x00b"); ee == nil || ee.Code != envelope.CodeBadRequest {
		t.Fatalf("NUL should be rejected: %+v", ee)
	}
	if ee := Validate("a\x01b"); ee == nil || ee.Code != envelope.CodeBadRequest {
		t.Fatalf("control char should be rejected: %+v", ee)
	}
	long := strings.Repeat("x", MaxLineBytes+1)
	if ee := Validate(long); ee == nil || ee.Code != envelope.CodeBadRequest {
		t.Fatalf("long line should be rejected: %+v", ee)
	}
}

func TestReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crontab")
	f, ee := Read(path)
	if ee != nil || f.Exists || f.Content != "" {
		t.Fatalf("missing read = %+v %+v", f, ee)
	}
	n, lines, ee := Write(path, "* * * * * echo hi\r\n# note\n")
	if ee != nil {
		t.Fatalf("write: %+v", ee)
	}
	if n == 0 || lines != 2 {
		t.Fatalf("write counts = %d/%d", n, lines)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "* * * * * echo hi\n# note\n" {
		t.Fatalf("content = %q", b)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", st.Mode().Perm())
	}
	f, ee = Read(path)
	if ee != nil || !f.Exists || f.Content != string(b) {
		t.Fatalf("read back = %+v %+v", f, ee)
	}
}
