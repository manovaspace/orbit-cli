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
