# Developer Web Onboarding Portal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the Developer Web Onboarding Portal at `https://orbit.manova.space/setup?token=...` featuring client-side token claim parsing, dynamic copyable setup one-liners, 2FA TOTP SVG QR code rendering, and CLI web setup link output.

**Architecture:** Extend `orbit-server` (`pkg/onboard`) to serve the static landing page at `/setup` and `/onboard`. Update `pkg/invite` and `cmd/orbit/staff.go` to carry `otpauth` metadata inside HMAC-signed tokens. Enhance `landing.html` with client-side token parsing, SVG QR code rendering, and tactile copy buttons.

**Tech Stack:** Go 1.23, Go `net/http`, HTML5/CSS/JavaScript (zero CDN dependencies), Charm CLI.

## Global Constraints

- No external CDN or third-party tracking scripts in `landing.html`.
- Zero backend cryptographic changes required to decode UI claims (token signature validation remains strictly enforced during terminal claim API calls).
- Content negotiation preserved: CLI User-Agents (`curl`, `wget`) always receive `install.sh`; browsers receive `landing.html`.

---

### Task 1: Server Routing for `/setup` and `/onboard`

**Files:**
- Modify: `orbit/orbit-cli/pkg/onboard/server.go:248-285`
- Test: `orbit/orbit-cli/pkg/onboard/server_test.go`

**Interfaces:**
- Consumes: `s.mux.HandleFunc`, `s.handleInstallScript`
- Produces: `GET /setup` and `GET /onboard` HTTP route handlers

- [ ] **Step 1: Write the failing tests in `server_test.go`**

```go
func TestHandleInstallScript_SetupAndOnboardRoutes(t *testing.T) {
	s, err := NewServer(ServerConfig{
		Secret: []byte("test-secret-32-bytes-long-key-1234"),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	for _, path := range []string{"/", "/setup", "/onboard"} {
		t.Run(path+"_BrowserHTML", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", "text/html,application/xhtml+xml")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want 200", path, w.Code)
			}
			if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
				t.Errorf("%s Content-Type = %q, want text/html", path, ct)
			}
			if !strings.Contains(w.Body.String(), "Install Orbit") {
				t.Errorf("%s body missing title: %s", path, w.Body.String()[:100])
			}
		})

		t.Run(path+"_CurlScript", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("User-Agent", "curl/8.5.0")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want 200", path, w.Code)
			}
			if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/x-shellscript") {
				t.Errorf("%s Content-Type = %q, want text/x-shellscript", path, ct)
			}
			if !strings.Contains(w.Body.String(), "#!/usr/bin/env bash") {
				t.Errorf("%s body missing bash shebang: %s", path, w.Body.String()[:100])
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./pkg/onboard -run TestHandleInstallScript_SetupAndOnboardRoutes`
Expected: FAIL with `404 Not Found` for `/setup` and `/onboard`.

- [ ] **Step 3: Update `routes()` and `handleInstallScript` in `server.go`**

```go
func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleInstallScript)
	s.mux.HandleFunc("GET /setup", s.handleInstallScript)
	s.mux.HandleFunc("GET /onboard", s.handleInstallScript)
	s.mux.HandleFunc("GET /v1/onboard/health", s.handleHealth)
    // ... remaining routes
}

func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path != "/" && path != "/setup" && path != "/onboard" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Vary", "Accept, User-Agent")
	if wantsInstallHTML(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(installLandingHTML)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(canonicalInstallScript)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./pkg/onboard -run TestHandleInstallScript_SetupAndOnboardRoutes`
Expected: PASS.

- [ ] **Step 5: Commit changes**

```bash
git add pkg/onboard/server.go pkg/onboard/server_test.go
git commit -m "feat(server): route /setup and /onboard to install landing handler"
```

---

### Task 2: Pass TOTP `otpauth` Metadata into Invite Tokens and Output Web Setup Link

**Files:**
- Modify: `orbit/orbit-cli/cmd/orbit/staff.go`
- Modify: `orbit/orbit-cli/cmd/orbit/invite.go`
- Test: `orbit/orbit-cli/cmd/orbit/staff_test.go`
- Test: `orbit/orbit-cli/cmd/orbit/invite_test.go`

**Interfaces:**
- Consumes: `client.StaffCreateResult.OTPAuth`, `invite.GenerateToken`
- Produces: `claims.Metadata["otpauth"]`, formatted Web Setup URL output

- [ ] **Step 1: Write failing tests in `staff_test.go` and `invite_test.go`**

```go
func TestStaffCreate_InviteWithTOTPAndWebSetupURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/staff" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"uid":              "sara",
				"personal_forward": "sara@gmail.com",
				"ldap_password":    "secret-ldap",
				"mail_password":    "secret-mail",
				"otpauth":          "otpauth://totp/Authelia:sara?secret=JBSWY3DPEHPK3PXP&issuer=Authelia",
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	storePath, rec := createTestVerifiedOwnerStore(t, t.TempDir())
	buf := new(bytes.Buffer)
	cmd := newRootCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"staff", "create",
		"--uid", "sara",
		"--forward", "sara@gmail.com",
		"--totp",
		"--invite",
		"--owner-store", storePath,
		"--server", srv.URL,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("staff create: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "web setup https://orbit.manova.space/setup?token=orbit-inv.") {
		t.Fatalf("missing web setup URL in output:\n%s", out)
	}

	// Verify token contains otpauth in metadata claims
	lines := strings.Split(out, "\n")
	var tokenStr string
	for _, l := range lines {
		if strings.HasPrefix(l, "token") {
			tokenStr = strings.TrimSpace(strings.TrimPrefix(l, "token"))
		}
	}
	if tokenStr == "" {
		t.Fatal("missing token in output")
	}

	claims, err := invite.ValidateToken(tokenStr, []byte(rec.RootSigningSecret))
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Metadata["otpauth"] != "otpauth://totp/Authelia:sara?secret=JBSWY3DPEHPK3PXP&issuer=Authelia" {
		t.Fatalf("claims.Metadata[otpauth] = %q, want expected URI", claims.Metadata["otpauth"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./cmd/orbit -run TestStaffCreate_InviteWithTOTPAndWebSetupURL`
Expected: FAIL with missing web setup URL and missing metadata.

- [ ] **Step 3: Update `cmd/orbit/staff.go` and `cmd/orbit/invite.go`**

In `cmd/orbit/staff.go`:
```go
func createStaffInvite(cmd *cobra.Command, email, name, ttlStr, otpauth string) (string, string, error) {
	// ... resolve secret
	meta := make(map[string]string)
	if strings.TrimSpace(otpauth) != "" {
		meta["otpauth"] = strings.TrimSpace(otpauth)
	}
	req := invite.InviteRequest{
		Email:       email,
		DisplayName: name,
		Scope:       "all",
		TTL:         ttl,
		CreatedBy:   "staff-admin",
		Metadata:    meta,
	}
	tokenStr, claims, err := invite.GenerateToken(req, []byte(secret))
	// ... save invite to store
	return tokenStr, claims.ID, nil
}
```
And in `newStaffCreateCmd`:
```go
tokenStr, invID, err := createStaffInvite(cmd, inviteEmail, strings.TrimSpace(nameFlag), inviteTTLFlag, res.OTPAuth)
if err != nil {
	fmt.Fprintf(cmd.OutOrStdout(), "warn   invite generation failed: %v\n", err)
} else {
	fmt.Fprintf(cmd.OutOrStdout(), "invite    %s\n", invID)
	fmt.Fprintf(cmd.OutOrStdout(), "token     %s\n", tokenStr)
	fmt.Fprintf(cmd.OutOrStdout(), "web setup https://orbit.manova.space/setup?token=%s\n", tokenStr)
}
```

In `cmd/orbit/invite.go` in `newInviteCreateCmd`:
```go
fmt.Fprintf(out, "  %s  Web Setup: %s\n\n", iconOK, infoStyle.Render(fmt.Sprintf("https://orbit.manova.space/setup?token=%s", tokenStr)))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./cmd/orbit -run "TestStaffCreate|TestInvite"`
Expected: PASS.

- [ ] **Step 5: Commit changes**

```bash
git add cmd/orbit/staff.go cmd/orbit/invite.go cmd/orbit/staff_test.go
git commit -m "feat(cli): embed otpauth into invite metadata and output web setup URL"
```

---

### Task 3: Interactive Web Setup View & Client-Side TOTP QR Code in `landing.html`

**Files:**
- Modify: `orbit/orbit-cli/pkg/onboard/landing.html`
- Test: `orbit/orbit-cli/pkg/onboard/server_test.go`

**Interfaces:**
- Consumes: `window.location.search`, `token` parameter, `claims.metadata.otpauth`
- Produces: Dynamic DOM layout with personalized greeting, 1-click terminal copy, SVG QR code, and secret copy button

- [ ] **Step 1: Add HTML/CSS & SVG QR Generator to `landing.html`**

1. Include embedded, lightweight QR SVG generation script:
```javascript
// Lightweight pure-JS QR code matrix generator for alphanumeric/byte SVG rendering
```
2. Parse `window.location.search` on DOMContentLoaded:
   - Extract `token`.
   - If present, parse `payload = JSON.parse(atob(token.split('.')[1]))`.
   - Update terminal command text and `data-copy` attribute to:
     `curl -fsSL orbit.manova.space | bash -s -- onboard --token <token>`
   - Display personalized heading: `Welcome, <name>!` and status pill.
   - If `payload.metadata && payload.metadata.otpauth`:
     - Render `#totp-section` with generated SVG QR code.
     - Extract `secret` query param from `otpauth://` URI and display with a "Copy Secret" button.

- [ ] **Step 2: Verify `landing.html` in Go test suite**

Run: `go test -v ./pkg/onboard -run TestHandleInstallScript_SetupAndOnboardRoutes`
Expected: PASS.

- [ ] **Step 3: Commit changes**

```bash
git add pkg/onboard/landing.html
git commit -m "feat(landing): add dynamic token onboarding view and TOTP QR code generator"
```

---

### Task 4: End-to-End Verification & Documentation

**Files:**
- Test: Full workspace test suites across `orbit-cli` and `orbit-staff`.
- Modify: `orbit/orbit-cli/README.md` (or handbook docs if applicable).

- [ ] **Step 1: Run all tests in `orbit-cli`**

Run: `go test -v ./...` in `/home/opmc/Dev/Manova/orbit/orbit-cli`
Expected: 100% PASS.

- [ ] **Step 2: Run all tests in `orbit-staff`**

Run: `go test -v ./...` in `/home/opmc/Dev/Manova/orbit/orbit-staff`
Expected: 100% PASS.

- [ ] **Step 3: Final Commit**

```bash
git commit -m "docs: finalize developer web onboarding portal docs"
```
