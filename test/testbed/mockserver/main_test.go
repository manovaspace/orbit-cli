package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/manovaspace/orbit-cli/pkg/client"
	"github.com/manovaspace/orbit-cli/pkg/provisioner"
	"github.com/manovaspace/orbit-cli/pkg/staffhmac"
)

const (
	testHMACSecret   = "test-secret-32-byte-hex-or-string-123456"
	testInviteSecret = "test-invite-secret-32-byte-string-98765"
)

func newTestMockServer() *MockServer {
	return NewMockServer(Config{
		HMACSecret:   testHMACSecret,
		InviteSecret: testInviteSecret,
	})
}

func TestOwnerChallengeAndVerify(t *testing.T) {
	srv := newTestMockServer()
	handler := srv.UnifiedHandler()

	// 1. Initiate challenge
	reqBody, _ := json.Marshal(map[string]string{"email": "admin@manova.space"})
	req := httptest.NewRequest(http.MethodPost, "/v1/owner/challenge", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 from /v1/owner/challenge, got %d: %s", rec.Code, rec.Body.String())
	}

	var chResp client.ChallengeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &chResp); err != nil {
		t.Fatalf("failed to decode challenge response: %v", err)
	}
	if chResp.Status != "pending" && chResp.Status != "ok" {
		t.Errorf("expected pending or ok status, got %s", chResp.Status)
	}

	// 2. Verify with correct OTP
	verifyBody, _ := json.Marshal(map[string]string{
		"email": "admin@manova.space",
		"code":  "123456",
	})
	vReq := httptest.NewRequest(http.MethodPost, "/v1/owner/verify", bytes.NewReader(verifyBody))
	vReq.Header.Set("Content-Type", "application/json")
	vRec := httptest.NewRecorder()
	handler.ServeHTTP(vRec, vReq)

	if vRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 from /v1/owner/verify, got %d: %s", vRec.Code, vRec.Body.String())
	}

	var vResp client.VerifyResponse
	if err := json.Unmarshal(vRec.Body.Bytes(), &vResp); err != nil {
		t.Fatalf("failed to decode verify response: %v", err)
	}
	if vResp.Status != "verified" {
		t.Errorf("expected status 'verified', got %s", vResp.Status)
	}
}

func TestGrantThreeStrikeBurn(t *testing.T) {
	srv := newTestMockServer()
	handler := srv.UnifiedHandler()

	// Register 8-digit admin grant
	grantBody, _ := json.Marshal(map[string]interface{}{
		"email":       "dev@manova.space",
		"role":        "admin",
		"code":        "1234-5678",
		"ttl_seconds": 900,
	})
	gReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/grants", bytes.NewReader(grantBody))
	gReq.Header.Set("Content-Type", "application/json")
	gRec := httptest.NewRecorder()
	handler.ServeHTTP(gRec, gReq)

	if gRec.Code != http.StatusOK && gRec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201 from /api/v1/admin/grants, got %d: %s", gRec.Code, gRec.Body.String())
	}

	// Strike 1 & 2: invalid code
	for i := 1; i <= 2; i++ {
		badBody, _ := json.Marshal(map[string]string{
			"email": "dev@manova.space",
			"code":  "0000-0000",
		})
		badReq := httptest.NewRequest(http.MethodPost, "/v1/owner/verify", bytes.NewReader(badBody))
		badReq.Header.Set("Content-Type", "application/json")
		badRec := httptest.NewRecorder()
		handler.ServeHTTP(badRec, badReq)

		if badRec.Code != http.StatusBadRequest {
			t.Fatalf("strike %d: expected 400 Bad Request, got %d: %s", i, badRec.Code, badRec.Body.String())
		}
	}

	// Strike 3: 3rd invalid attempt must incinerate/burn the grant
	badBody3, _ := json.Marshal(map[string]string{
		"email": "dev@manova.space",
		"code":  "0000-0000",
	})
	badReq3 := httptest.NewRequest(http.MethodPost, "/v1/owner/verify", bytes.NewReader(badBody3))
	badReq3.Header.Set("Content-Type", "application/json")
	badRec3 := httptest.NewRecorder()
	handler.ServeHTTP(badRec3, badReq3)

	if badRec3.Code != http.StatusBadRequest && badRec3.Code != http.StatusGone {
		t.Fatalf("strike 3: expected 400 or 410, got %d: %s", badRec3.Code, badRec3.Body.String())
	}
	if !strings.Contains(badRec3.Body.String(), "burned") && !strings.Contains(badRec3.Body.String(), "exceeded") {
		t.Errorf("strike 3: expected message about burned/exceeded grant, got: %s", badRec3.Body.String())
	}

	// Attempt 4: Even with correct code, must fail because grant is burned
	correctBody, _ := json.Marshal(map[string]string{
		"email": "dev@manova.space",
		"code":  "1234-5678",
	})
	cReq := httptest.NewRequest(http.MethodPost, "/v1/owner/verify", bytes.NewReader(correctBody))
	cReq.Header.Set("Content-Type", "application/json")
	cRec := httptest.NewRecorder()
	handler.ServeHTTP(cRec, cReq)

	if cRec.Code == http.StatusOK {
		t.Fatalf("expected burned grant to be rejected, but got 200 OK")
	}
}

func TestStaffHMACAuthenticationAndReservedUserGuards(t *testing.T) {
	srv := newTestMockServer()
	handler := srv.UnifiedHandler()

	createPayload, _ := json.Marshal(client.StaffCreateInput{
		UID:             "alice",
		DisplayName:     "Alice Developer",
		PersonalForward: "alice@example.com",
		Groups:          []string{"core", "dev"},
		TOTP:            true,
	})

	// 1. Missing HMAC headers -> 401 Unauthorized
	unauthReq := httptest.NewRequest(http.MethodPost, "/api/v1/staff", bytes.NewReader(createPayload))
	unauthReq.Header.Set("Content-Type", "application/json")
	unauthRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthRec, unauthReq)

	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for missing HMAC, got %d", unauthRec.Code)
	}

	// 2. Bad HMAC signature -> 401 Unauthorized
	ts := time.Now().Unix()
	badSig := staffhmac.Sign("wrong-secret-signature", ts, http.MethodPost, "/api/v1/staff", createPayload)
	badSigReq := httptest.NewRequest(http.MethodPost, "/api/v1/staff", bytes.NewReader(createPayload))
	badSigReq.Header.Set("Content-Type", "application/json")
	badSigReq.Header.Set("X-Orbit-Timestamp", strconv.FormatInt(ts, 10))
	badSigReq.Header.Set("X-Orbit-Signature", badSig)
	badSigRec := httptest.NewRecorder()
	handler.ServeHTTP(badSigRec, badSigReq)

	if badSigRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for bad HMAC, got %d", badSigRec.Code)
	}

	// 3. Valid HMAC signature for normal user -> 200/201 Created
	validSig := staffhmac.Sign(testHMACSecret, ts, http.MethodPost, "/api/v1/staff", createPayload)
	validReq := httptest.NewRequest(http.MethodPost, "/api/v1/staff", bytes.NewReader(createPayload))
	validReq.Header.Set("Content-Type", "application/json")
	validReq.Header.Set("X-Orbit-Timestamp", strconv.FormatInt(ts, 10))
	validReq.Header.Set("X-Orbit-Signature", validSig)
	validRec := httptest.NewRecorder()
	handler.ServeHTTP(validRec, validReq)

	if validRec.Code != http.StatusOK && validRec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201 for valid staff create, got %d: %s", validRec.Code, validRec.Body.String())
	}

	var createRes client.StaffCreateResult
	if err := json.Unmarshal(validRec.Body.Bytes(), &createRes); err != nil {
		t.Fatalf("failed to decode staff create result: %v", err)
	}
	if createRes.UID != "alice" || createRes.Status != "active" {
		t.Errorf("unexpected create result: %+v", createRes)
	}

	// 4. Reserved UID rejection ("admin", "authelia-bind", "verdaccio-bind", "verdaccio-ci") -> 403 Forbidden
	reservedUIDs := []string{"admin", "authelia-bind", "verdaccio-bind", "verdaccio-ci"}
	for _, reserved := range reservedUIDs {
		resPayload, _ := json.Marshal(client.StaffCreateInput{
			UID:             reserved,
			DisplayName:     "Reserved Account",
			PersonalForward: reserved + "@example.com",
		})
		rTS := time.Now().Unix()
		rSig := staffhmac.Sign(testHMACSecret, rTS, http.MethodPost, "/api/v1/staff", resPayload)
		rReq := httptest.NewRequest(http.MethodPost, "/api/v1/staff", bytes.NewReader(resPayload))
		rReq.Header.Set("Content-Type", "application/json")
		rReq.Header.Set("X-Orbit-Timestamp", strconv.FormatInt(rTS, 10))
		rReq.Header.Set("X-Orbit-Signature", rSig)
		rRec := httptest.NewRecorder()
		handler.ServeHTTP(rRec, rReq)

		if rRec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for reserved UID %q, got %d: %s", reserved, rRec.Code, rRec.Body.String())
		}
	}
}

func TestStaffLifecycle(t *testing.T) {
	srv := newTestMockServer()
	handler := srv.UnifiedHandler()

	// 1. Create Bob
	createBody, _ := json.Marshal(client.StaffCreateInput{
		UID:             "bob",
		DisplayName:     "Bob Smith",
		PersonalForward: "bob@example.com",
		Groups:          []string{"dev"},
	})
	ts := time.Now().Unix()
	sig := staffhmac.Sign(testHMACSecret, ts, http.MethodPost, "/api/v1/staff", createBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/staff", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Orbit-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Orbit-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("failed to create staff: %d %s", rec.Code, rec.Body.String())
	}

	// 2. Get Bob
	ts = time.Now().Unix()
	sig = staffhmac.Sign(testHMACSecret, ts, http.MethodGet, "/api/v1/staff/bob", nil)
	gReq := httptest.NewRequest(http.MethodGet, "/api/v1/staff/bob", nil)
	gReq.Header.Set("X-Orbit-Timestamp", strconv.FormatInt(ts, 10))
	gReq.Header.Set("X-Orbit-Signature", sig)
	gRec := httptest.NewRecorder()
	handler.ServeHTTP(gRec, gReq)

	if gRec.Code != http.StatusOK {
		t.Fatalf("failed to get staff: %d %s", gRec.Code, gRec.Body.String())
	}

	// 3. Disable Bob
	ts = time.Now().Unix()
	sig = staffhmac.Sign(testHMACSecret, ts, http.MethodPost, "/api/v1/staff/bob/disable", nil)
	dReq := httptest.NewRequest(http.MethodPost, "/api/v1/staff/bob/disable", nil)
	dReq.Header.Set("X-Orbit-Timestamp", strconv.FormatInt(ts, 10))
	dReq.Header.Set("X-Orbit-Signature", sig)
	dRec := httptest.NewRecorder()
	handler.ServeHTTP(dRec, dReq)

	if dRec.Code != http.StatusOK {
		t.Fatalf("failed to disable staff: %d %s", dRec.Code, dRec.Body.String())
	}

	// 4. Delete Bob
	ts = time.Now().Unix()
	sig = staffhmac.Sign(testHMACSecret, ts, http.MethodDelete, "/api/v1/staff/bob", nil)
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/staff/bob", nil)
	delReq.Header.Set("X-Orbit-Timestamp", strconv.FormatInt(ts, 10))
	delReq.Header.Set("X-Orbit-Signature", sig)
	delRec := httptest.NewRecorder()
	handler.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK && delRec.Code != http.StatusNoContent {
		t.Fatalf("failed to delete staff: %d %s", delRec.Code, delRec.Body.String())
	}
}

func TestS3MockOperations(t *testing.T) {
	srv := newTestMockServer()
	handler := srv.UnifiedHandler()

	bucket := "orbit-test-bucket"
	key := "objects/abc123hash"
	data := []byte("hello world binary payload")

	// 1. HEAD Bucket
	headBReq := httptest.NewRequest(http.MethodHead, "/"+bucket, nil)
	headBRec := httptest.NewRecorder()
	handler.ServeHTTP(headBRec, headBReq)
	if headBRec.Code != http.StatusOK {
		t.Fatalf("HEAD bucket failed with %d", headBRec.Code)
	}

	// 2. PUT Object
	putReq := httptest.NewRequest(http.MethodPut, "/"+bucket+"/"+key, bytes.NewReader(data))
	putReq.Header.Set("Content-Type", "application/octet-stream")
	putReq.Header.Set("x-amz-meta-path", "manovaspace/ts")
	putRec := httptest.NewRecorder()
	handler.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT object failed with %d: %s", putRec.Code, putRec.Body.String())
	}

	// 3. HEAD Object
	headReq := httptest.NewRequest(http.MethodHead, "/"+bucket+"/"+key, nil)
	headRec := httptest.NewRecorder()
	handler.ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusOK {
		t.Fatalf("HEAD object failed with %d", headRec.Code)
	}
	if headRec.Header().Get("x-amz-meta-path") != "manovaspace/ts" {
		t.Errorf("expected x-amz-meta-path header, got %s", headRec.Header().Get("x-amz-meta-path"))
	}

	// 4. GET Object
	getReq := httptest.NewRequest(http.MethodGet, "/"+bucket+"/"+key, nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET object failed with %d: %s", getRec.Code, getRec.Body.String())
	}
	if !bytes.Equal(getRec.Body.Bytes(), data) {
		t.Fatalf("GET object payload mismatch: expected %s, got %s", string(data), getRec.Body.String())
	}

	// 5. HEAD Missing Object -> 404
	headMissReq := httptest.NewRequest(http.MethodHead, "/"+bucket+"/missing-key", nil)
	headMissRec := httptest.NewRecorder()
	handler.ServeHTTP(headMissRec, headMissReq)
	if headMissRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing object, got %d", headMissRec.Code)
	}
}

func TestForgejoSSHKeyMock(t *testing.T) {
	srv := newTestMockServer()
	handler := srv.UnifiedHandler()

	// 1. Add SSH Key
	keyBody, _ := json.Marshal(map[string]string{
		"title": "orbit-dev",
		"key":   "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample developer@manova.space",
	})
	postReq := httptest.NewRequest(http.MethodPost, "/api/v1/user/keys", bytes.NewReader(keyBody))
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusCreated && postRec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/user/keys failed with %d: %s", postRec.Code, postRec.Body.String())
	}

	// 2. List SSH Keys
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/user/keys failed with %d: %s", getRec.Code, getRec.Body.String())
	}

	var keys []map[string]interface{}
	if err := json.Unmarshal(getRec.Body.Bytes(), &keys); err != nil {
		t.Fatalf("failed to decode keys list: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
}

func TestOnboardClaimAndValidate(t *testing.T) {
	srv := newTestMockServer()
	handler := srv.UnifiedHandler()

	// 1. Claim
	claimBody, _ := json.Marshal(provisioner.ClaimRequest{
		InviteToken:  "test-invite-token",
		DesiredUID:   "charlie",
		Email:        "charlie@example.com",
		DisplayName:  "Charlie Developer",
		SSHPublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICharlieKey charlie@example.com",
	})
	cReq := httptest.NewRequest(http.MethodPost, "/v1/onboard/claim", bytes.NewReader(claimBody))
	cReq.Header.Set("Content-Type", "application/json")
	cRec := httptest.NewRecorder()
	handler.ServeHTTP(cRec, cReq)

	if cRec.Code != http.StatusOK {
		t.Fatalf("POST /v1/onboard/claim failed with %d: %s", cRec.Code, cRec.Body.String())
	}

	var resp provisioner.ClaimResponse
	if err := json.Unmarshal(cRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode claim response: %v", err)
	}
	if resp.User.UID != "charlie" {
		t.Errorf("expected UID 'charlie', got %s", resp.User.UID)
	}

	// 2. Validate token
	valReq := httptest.NewRequest(http.MethodGet, "/v1/onboard/validate?token=test-invite-token", nil)
	valRec := httptest.NewRecorder()
	handler.ServeHTTP(valRec, valReq)

	if valRec.Code != http.StatusOK {
		t.Fatalf("GET /v1/onboard/validate failed with %d: %s", valRec.Code, valRec.Body.String())
	}
}

func TestStaffRecreateAndUpdate(t *testing.T) {
	srv := newTestMockServer()
	handler := srv.UnifiedHandler()

	// 1. Recreate user dave
	recreateBody, _ := json.Marshal(client.StaffCreateInput{
		DisplayName:     "Dave Developer",
		PersonalForward: "dave@example.com",
		Groups:          []string{"core"},
		TOTP:            true,
	})
	ts := time.Now().Unix()
	sig := staffhmac.Sign(testHMACSecret, ts, http.MethodPost, "/api/v1/staff/dave/recreate", recreateBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/staff/dave/recreate", bytes.NewReader(recreateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Orbit-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Orbit-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("recreate failed with %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Update user dave
	updateBody, _ := json.Marshal(client.StaffUpdateInput{
		DisplayName:     "Dave Senior Developer",
		PersonalForward: "dave-senior@example.com",
		Groups:          []string{"core", "security"},
	})
	ts = time.Now().Unix()
	sig = staffhmac.Sign(testHMACSecret, ts, http.MethodPatch, "/api/v1/staff/dave", updateBody)
	uReq := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/dave", bytes.NewReader(updateBody))
	uReq.Header.Set("Content-Type", "application/json")
	uReq.Header.Set("X-Orbit-Timestamp", strconv.FormatInt(ts, 10))
	uReq.Header.Set("X-Orbit-Signature", sig)
	uRec := httptest.NewRecorder()
	handler.ServeHTTP(uRec, uReq)

	if uRec.Code != http.StatusOK {
		t.Fatalf("update failed with %d: %s", uRec.Code, uRec.Body.String())
	}

	var updated client.StaffMember
	if err := json.Unmarshal(uRec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to decode updated staff member: %v", err)
	}
	if updated.DisplayName != "Dave Senior Developer" || updated.PersonalForward != "dave-senior@example.com" {
		t.Errorf("unexpected updated data: %+v", updated)
	}
}

func TestMockServerStartAndClose(t *testing.T) {
	srv := NewMockServer(Config{
		EdgeAddr:     "127.0.0.1:0",
		StaffAddr:    "127.0.0.1:0",
		S3Addr:       "127.0.0.1:0",
		ForgejoAddr:  "127.0.0.1:0",
		HMACSecret:   testHMACSecret,
		InviteSecret: testInviteSecret,
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("failed to close mock server: %v", err)
	}
}

