package iam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/yz1qaq/asterflow/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

const tokenIssuer = "asterflow"

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountInactive    = errors.New("user account is not active")
	ErrInvalidToken       = errors.New("invalid token")
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expires_at"`
	User      *domain.User `json:"user"`
}

type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

func (s *Service) Login(
	ctx context.Context,
	req LoginRequest,
) (*LoginResponse, error) {
	if len(s.jwtKey) == 0 {
		return nil, fmt.Errorf("jwt key is not configured")
	}
	user, err := s.store.GetUserByEmail(ctx, normalizeEmail(req.Email))
	if errors.Is(err, ErrUserNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("get login user: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte(req.Password),
	); err != nil {
		return nil, ErrInvalidCredentials
	}
	if user.Status != "active" {
		return nil, ErrAccountInactive
	}

	now := time.Now().Truncate(time.Second)
	expiresAt := now.Add(24 * time.Hour)

	claims := &Claims{
		UserID: user.ID,
		Email:  user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	//创建token对象,确定算法和内容
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	//得到签名
	signed, err := token.SignedString(s.jwtKey)
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}
	return &LoginResponse{
		Token:     signed,
		ExpiresAt: expiresAt,
		User:      user,
	}, nil
}

func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	if len(s.jwtKey) == 0 {
		return nil, fmt.Errorf("jwt key is not configured")
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		//获取密钥的函数
		func(_ *jwt.Token) (any, error) {
			return s.jwtKey, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	if !token.Valid ||
		claims.UserID == "" ||
		claims.Subject != claims.UserID {
		return nil, ErrInvalidToken
	}

	return claims, nil
}
