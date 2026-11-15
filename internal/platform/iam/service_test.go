package iam

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"golang.org/x/crypto/bcrypt"
)

func TestCreateUserValidation(t *testing.T) {
	service := NewService(nil, nil)
	for _, tc := range []struct {
		name, email, password, displayName string
		want                               error
	}{
		{"invalid email", "invalid", "password123", "Alice", ErrInvalidEmail},
		{"display address", "Alice <alice@example.test>", "password123", "Alice", ErrInvalidEmail},
		{"short password", "alice@example.test", "short", "Alice", ErrWeakPassword},
		{"blank name", "alice@example.test", "password123", "  ", ErrNameRequired},
		{"bcrypt password limit", "alice@example.test", strings.Repeat("a", 73), "Alice", bcrypt.ErrPasswordTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user, err := service.CreateUser(context.Background(), tc.email, tc.password, tc.displayName)
			if user != nil || !errors.Is(err, tc.want) {
				t.Fatalf("user=%v, error=%v; want %v", user, err, tc.want)
			}
		})
	}
}

func TestUserServicePostgres(t *testing.T) {
	store := NewPostgresStore(newIAMTestPool(t))
	service := NewService(store, nil)
	ctx := context.Background()

	t.Run("creation normalizes identity and stores a password hash", func(t *testing.T) {
		user, err := service.CreateUser(ctx, " Alice@Example.test ", " password123 ", " Alice ")
		if err != nil {
			t.Fatal(err)
		}
		if user.Email != "alice@example.test" || user.Name != "Alice" ||
			user.Role != GlobalRoleMember || user.Status != "active" {
			t.Fatal("unexpected normalized identity or initial permissions")
		}
		stored, err := store.GetUserByID(ctx, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.PasswordHash == " password123 " ||
			bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte(" password123 ")) != nil {
			t.Fatal("persisted password must be a verifiable bcrypt hash")
		}
		if bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("password123")) == nil {
			t.Fatal("password whitespace must not be stripped")
		}
		if _, err := service.CreateUser(ctx, " ALICE@example.test ", "another123", "Other"); !errors.Is(err, ErrEmailExists) {
			t.Fatalf("normalized duplicate email error = %v", err)
		}
	})

	t.Run("seed is repeatable and preserves the existing password", func(t *testing.T) {
		if err := service.EnsureSeedAdmin(ctx, " Admin@Example.test ", "original123"); err != nil {
			t.Fatal(err)
		}
		before, err := store.GetUserByEmail(ctx, "admin@example.test")
		if err != nil {
			t.Fatal(err)
		}
		if err := service.EnsureSeedAdmin(ctx, before.Email, "replacement123"); err != nil {
			t.Fatal(err)
		}
		after, err := store.GetUserByEmail(ctx, before.Email)
		if err != nil || after.ID != before.ID || after.Role != GlobalRoleAdmin ||
			after.PasswordHash != before.PasswordHash {
			t.Fatalf("seed must retain identity/password and grant admin: %v", err)
		}
	})

	t.Run("concurrent startup creates one seed account", func(t *testing.T) {
		start := make(chan struct{})
		results := make(chan error, 2)
		var workers sync.WaitGroup
		for i := 0; i < 2; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				results <- service.EnsureSeedAdmin(ctx, "parallel@example.test", "password123")
			}()
		}
		close(start)
		workers.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal(err)
			}
		}
		user, err := store.GetUserByEmail(ctx, "parallel@example.test")
		if err != nil || user.Role != GlobalRoleAdmin {
			t.Fatalf("concurrent seed account: %v", err)
		}
	})
}

func TestLoginPostgres(t *testing.T) {
	pool := newIAMTestPool(t)
	service := NewService(NewPostgresStore(pool), []byte("login-fixture-key-with-32-bytes!!"))
	ctx := context.Background()
	user, err := service.CreateUser(ctx, "login@example.test", "password123", "Login User")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("login signs identity with a matching expiry and no password JSON", func(t *testing.T) {
		response, err := service.Login(ctx, LoginRequest{Email: " LOGIN@example.test ", Password: "password123"})
		if err != nil {
			t.Fatal(err)
		}
		claims, err := service.ValidateToken(response.Token)
		if err != nil || claims.UserID != user.ID || claims.Subject != user.ID ||
			claims.Email != user.Email || claims.Issuer != tokenIssuer {
			t.Fatalf("signed login identity mismatch: %v", err)
		}
		if !response.ExpiresAt.Equal(claims.ExpiresAt.Time) ||
			!claims.ExpiresAt.Time.Equal(claims.IssuedAt.Add(24*time.Hour)) {
			t.Fatal("response/token expiry must agree and retain the 24-hour lifetime")
		}
		body, err := json.Marshal(response)
		if err != nil || strings.Contains(string(body), "password") ||
			strings.Contains(string(body), user.PasswordHash) {
			t.Fatal("login JSON exposed a password field or hash")
		}
	})

	t.Run("unknown email and wrong password reject credentials", func(t *testing.T) {
		for _, req := range []LoginRequest{
			{Email: user.Email, Password: "incorrect123"},
			{Email: "missing@example.test", Password: "password123"},
		} {
			if response, err := service.Login(ctx, req); response != nil || !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("invalid credentials returned response=%v error=%v", response, err)
			}
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := service.Login(canceled, LoginRequest{Email: user.Email, Password: "password123"}); !errors.Is(err, context.Canceled) || errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("database cancellation must retain its cause: %v", err)
		}
	})

	t.Run("inactive account cannot log in", func(t *testing.T) {
		if _, err := pool.Exec(ctx, "UPDATE users SET status='inactive' WHERE id=$1", user.ID); err != nil {
			t.Fatal(err)
		}
		if response, err := service.Login(ctx, LoginRequest{Email: user.Email, Password: "password123"}); response != nil || !errors.Is(err, ErrAccountInactive) {
			t.Fatalf("inactive login returned response=%v error=%v", response, err)
		}
	})
}

func TestValidateToken(t *testing.T) {
	key := []byte("jwt-fixture-key-with-32-bytes!!!!")
	service := NewService(nil, key)
	now := time.Now().Add(-time.Minute)
	newClaims := func() *Claims {
		return &Claims{
			UserID: "user-1", Email: "user@example.test",
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer: tokenIssuer, Subject: "user-1",
				ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now),
			},
		}
	}
	sign := func(c *Claims, method jwt.SigningMethod, signingKey []byte) string {
		signed, err := jwt.NewWithClaims(method, c).SignedString(signingKey)
		if err != nil {
			t.Fatal(err)
		}
		return signed
	}
	good := sign(newClaims(), jwt.SigningMethodHS256, key)
	if claims, err := service.ValidateToken(good); err != nil || claims.UserID != "user-1" {
		t.Fatalf("valid token: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Claims)
		method jwt.SigningMethod
		key    []byte
	}{
		{"expired", func(c *Claims) { c.ExpiresAt = jwt.NewNumericDate(now) }, jwt.SigningMethodHS256, key},
		{"missing expiry", func(c *Claims) { c.ExpiresAt = nil }, jwt.SigningMethodHS256, key},
		{"wrong issuer", func(c *Claims) { c.Issuer = "other" }, jwt.SigningMethodHS256, key},
		{"subject mismatch", func(c *Claims) { c.Subject = "other" }, jwt.SigningMethodHS256, key},
		{"missing user", func(c *Claims) { c.UserID = ""; c.Subject = "" }, jwt.SigningMethodHS256, key},
		{"not yet valid", func(c *Claims) { c.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Hour)) }, jwt.SigningMethodHS256, key},
		{"future issued", func(c *Claims) { c.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Hour)) }, jwt.SigningMethodHS256, key},
		{"wrong algorithm", func(c *Claims) {}, jwt.SigningMethodHS384, key},
		{"wrong key", func(c *Claims) {}, jwt.SigningMethodHS256, []byte("another-fixture-signing-key!!!!!!")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := newClaims()
			tc.mutate(claims)
			if _, err := service.ValidateToken(sign(claims, tc.method, tc.key)); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("invalid token error = %v", err)
			}
		})
	}
	if _, err := service.ValidateToken("malformed"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("malformed token error = %v", err)
	}
	if _, err := NewService(nil, nil).ValidateToken(good); err == nil {
		t.Fatal("empty signing key accepted")
	}
	if _, err := NewService(nil, nil).Login(context.Background(), LoginRequest{}); err == nil {
		t.Fatal("login accepted an empty signing key")
	}
}
