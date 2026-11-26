package tool

import (
	"fmt"
	"strings"

	"github.com/yz1qaq/asterflow/internal/domain"
)

type CredentialCipher interface {
	Encrypt(plain string) ([]byte, error)
	Decrypt(ciphertext []byte) (string, error)
}

// ConfigureSecurity 供启动装配注入版本凭据加密器和端点策略。
func (s *Service) ConfigureSecurity(cipher CredentialCipher, policy *EndpointPolicy) {
	s.cipher = cipher
	s.policy = policy
}

func (s *Service) prepareVersionAuth(version *domain.ToolVersion) error {
	// 空字符串和去掉首尾空白后的 {} 表示未配置凭据。
	plain := strings.TrimSpace(version.AuthConfig)
	version.HasAuth = plain != "" && plain != "{}"
	if !version.HasAuth {
		return nil
	}
	// 有凭据时必须提供加密器，避免后续把明文写入数据库。
	if s.cipher == nil {
		return fmt.Errorf("tool credential cipher is not configured")
	}

	ciphertext, err := s.cipher.Encrypt(version.AuthConfig)
	if err != nil {
		return fmt.Errorf("encrypt tool auth config: %w", err)
	}
	// 加密成功后再更新待写入的版本，将明文配置替换为空对象。
	version.AuthConfigEncrypted = ciphertext
	version.AuthConfig = "{}"
	return nil
}

func (s *Service) authPlain(version *domain.ToolVersion) (string, error) {
	// 兼容没有密文的旧版本；新版本保存时仍会加密。
	if len(version.AuthConfigEncrypted) == 0 {
		return version.AuthConfig, nil
	}
	if s.cipher == nil {
		return "", fmt.Errorf("tool credential cipher is not configured")
	}
	plain, err := s.cipher.Decrypt(version.AuthConfigEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt tool auth config: %w", err)
	}
	return plain, nil
}
