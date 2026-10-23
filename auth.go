package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionTTL = 30 * 24 * time.Hour

func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(b), err
}

func checkPassword(hash, pw string) bool {
	return hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func newSessionKey() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func makeSession(key string, exp int64) string {
	mac := hmac.New(sha256.New, []byte(key))
	fmt.Fprintf(mac, "%d", exp)
	return strconv.FormatInt(exp, 10) + "." + hex.EncodeToString(mac.Sum(nil))
}

func validSession(key, val string) bool {
	parts := strings.SplitN(val, ".", 2)
	if len(parts) != 2 {
		return false
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	mac := hmac.New(sha256.New, []byte(key))
	fmt.Fprintf(mac, "%d", exp)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(parts[1]))
}

func sessionCookie(r *http.Request, cfg *Config) bool {
	if cfg.PasswordHash == "" || cfg.SessionKey == "" {
		return false
	}
	c, err := r.Cookie("nuitelle_session")
	if err != nil {
		c, err = r.Cookie("vela_session")
	}
	if err != nil {
		return false
	}
	return validSession(cfg.SessionKey, c.Value)
}

func setSessionCookie(w http.ResponseWriter, cfg *Config) {
	exp := time.Now().Add(sessionTTL).Unix()
	http.SetCookie(w, &http.Cookie{
		Name: "nuitelle_session", Value: makeSession(cfg.SessionKey, exp), Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "nuitelle_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

// --- TOTP (RFC 6238, SHA-1, 30s, 6 digits) ---

func newTOTPSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

func totpAt(secret string, t time.Time) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return ""
	}
	counter := uint64(t.Unix() / 30)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[off]&0x7f) << 24) | (uint32(sum[off+1]) << 16) | (uint32(sum[off+2]) << 8) | uint32(sum[off+3])
	return fmt.Sprintf("%06d", code%1000000)
}

func verifyTOTP(secret, code string) bool {
	code = strings.TrimSpace(code)
	if secret == "" || len(code) != 6 {
		return false
	}
	now := time.Now()
	for skew := -1; skew <= 1; skew++ {
		if subtle.ConstantTimeCompare([]byte(totpAt(secret, now.Add(time.Duration(skew)*30*time.Second))), []byte(code)) == 1 {
			return true
		}
	}
	return false
}
