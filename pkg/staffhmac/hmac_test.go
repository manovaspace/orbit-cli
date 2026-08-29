package staffhmac

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	secret := "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	body := []byte(`{"uid":"sara"}`)
	ts := int64(1700000000)
	sig := Sign(secret, ts, "POST", "/api/v1/staff", body)
	if err := Verify(secret, ts, "POST", "/api/v1/staff", body, sig, time.Unix(ts, 0)); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsOldTimestamp(t *testing.T) {
	secret := "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	ts := int64(1000)
	sig := Sign(secret, ts, "GET", "/api/v1/staff", nil)
	err := Verify(secret, ts, "GET", "/api/v1/staff", nil, sig, time.Unix(ts+400, 0))
	if err == nil {
		t.Fatal("expected replay window error")
	}
}

func TestEmptyBodyHash(t *testing.T) {
	sum := sha256.Sum256(nil)
	got := hex.EncodeToString(sum[:])
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Fatalf("got %s", got)
	}
}
