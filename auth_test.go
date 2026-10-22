package main

import (
	"testing"
	"time"
)

func TestSessionTokens(t *testing.T) {
	key := newSessionKey()
	good := makeSession(key, time.Now().Add(time.Hour).Unix())
	if !validSession(key, good) {
		t.Fatal("valid session rejected")
	}
	if validSession(key, makeSession("other", time.Now().Add(time.Hour).Unix())) {
		t.Fatal("wrong key accepted")
	}
	if validSession(key, makeSession(key, time.Now().Add(-time.Hour).Unix())) {
		t.Fatal("expired session accepted")
	}
	if validSession(key, good+"x") {
		t.Fatal("tampered session accepted")
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := hashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	if !checkPassword(h, "secret") {
		t.Fatal("correct password rejected")
	}
	if checkPassword(h, "wrong") {
		t.Fatal("wrong password accepted")
	}
	if checkPassword("", "secret") {
		t.Fatal("empty hash accepted")
	}
}
