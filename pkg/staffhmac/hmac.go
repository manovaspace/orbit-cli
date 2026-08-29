package staffhmac

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const Window = 5 * time.Minute

func canonical(ts int64, method, path string, body []byte) string {
	sum := sha256.Sum256(body)
	return strconv.FormatInt(ts, 10) + "\n" + strings.ToUpper(method) + "\n" + path + "\n" + hex.EncodeToString(sum[:])
}

func Sign(secret string, ts int64, method, path string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical(ts, method, path, body)))
	return hex.EncodeToString(mac.Sum(nil))
}

func Verify(secret string, ts int64, method, path string, body []byte, sig string, now time.Time) error {
	if now.Sub(time.Unix(ts, 0)).Abs() > Window {
		return fmt.Errorf("timestamp outside replay window")
	}
	want := Sign(secret, ts, method, path, body)
	if !hmac.Equal([]byte(strings.ToLower(sig)), []byte(strings.ToLower(want))) {
		return fmt.Errorf("bad signature")
	}
	return nil
}
