package host

import "testing"

func TestParseVirt(t *testing.T) {
	if ParseVirt("Linux version 6.8.0-generic", false) != "native" {
		t.Fatal("native")
	}
	if ParseVirt("Linux version 5.15.0-microsoft-standard-WSL2", false) != "wsl2" {
		t.Fatal("wsl2 kernel")
	}
	if ParseVirt("Linux version 4.4.0-Microsoft", false) != "wsl1" {
		t.Fatal("wsl1")
	}
	if ParseVirt("Linux version 4.4.0-Microsoft", true) != "wsl2" {
		t.Fatal("/run/WSL implies wsl2")
	}
}

func TestLoginShellFromPasswd(t *testing.T) {
	pw := "root:x:0:0:root:/root:/bin/bash\ndev:x:1000:1000:Dev:/home/dev:/usr/bin/zsh\n"
	if LoginShellFromPasswd(pw, "dev") != "/usr/bin/zsh" {
		t.Fatal("expected zsh")
	}
	if LoginShellFromPasswd(pw, "root") != "/bin/bash" {
		t.Fatal("expected bash")
	}
	if LoginShellFromPasswd(pw, "missing") != "" {
		t.Fatal("expected empty")
	}
}

func TestParseDsclUserShell(t *testing.T) {
	if got := ParseDsclUserShell("UserShell: /bin/zsh\n"); got != "/bin/zsh" {
		t.Fatalf("got %q", got)
	}
	if got := ParseDsclUserShell("dsAttrTypeNative:UserShell: /bin/bash"); got != "" {
		t.Fatalf("prefixed line must not match, got %q", got)
	}
	if got := ParseDsclUserShell("eDSRecordNotFound"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestDarwinLoginShell_EnvFallback(t *testing.T) {
	if got := darwinLoginShell("", "/bin/zsh"); got != "/bin/zsh" {
		t.Fatalf("empty user must fall back to $SHELL, got %q", got)
	}
}
