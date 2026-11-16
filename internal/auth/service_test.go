package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateNewPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{name: "empty", password: "", wantErr: ErrPasswordTooShort},
		{name: "whitespace only", password: "          ", wantErr: ErrPasswordTooShort},
		{name: "nine runes", password: "123456789", wantErr: ErrPasswordTooShort},
		{name: "ten ascii runes", password: "1234567890"},
		{name: "chinese passphrase", password: "这是一个足够长的密码"},
		{name: "too long", password: strings.Repeat("a", PasswordMaxRunes+1), wantErr: ErrPasswordTooLong},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateNewPassword(test.password)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ValidateNewPassword() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestHashPasswordRoundTrip(t *testing.T) {
	const password = "correct horse battery staple"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if hash == password {
		t.Fatal("HashPassword() returned the plaintext password")
	}
	ok, err := VerifyPassword(hash, password)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !ok {
		t.Fatal("VerifyPassword() rejected the correct password")
	}
	ok, err = VerifyPassword(hash, password+"!")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if ok {
		t.Fatal("VerifyPassword() accepted a wrong password")
	}
}
