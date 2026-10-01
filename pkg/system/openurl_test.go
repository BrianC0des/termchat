package system

import (
	"strings"
	"testing"
)

func TestValidateOpenURL(t *testing.T) {
	for _, ok := range []string{"https://github.com/login/device", "http://localhost:8080/x", "https://example.com/a?b=c#d"} {
		if err := validateOpenURL(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"", "file:///etc/passwd", "javascript:alert(1)", "ms-msdt:/id x", "smb://host/share",
		"-oProxyCommand=evil", "--help", "ftp://x/y", "https://", "//evil.example",
		"https://a.com/\nx", "https://a.com/\x00", strings.Repeat("a", 5000),
	} {
		if err := validateOpenURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
