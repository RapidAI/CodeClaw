package skillmarket

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/pbkdf2"
)

const (
	rsaKeyBits     = 2048
	pbkdf2Iter     = 100_000
	aesKeyLen      = 32
	privateKeyFile = "rsa_private.pem"
	publicKeyFile  = "rsa_public.pem"
)

// EncryptedPackage 是加密后的下载包。
type EncryptedPackage struct {
	EncryptedSalt []byte `json:"encrypted_salt"` // RSA-OAEP 加密的 salt
	EncryptedZip  []byte `json:"encrypted_zip"`  // AES-256-GCM 加密的 zip (nonce + ciphertext)
}

// EnsureRSAKeyPair 确保 dataDir 下存在 RSA 密钥对。
// 不存在则生成，已存在则加载，不覆盖。
func EnsureRSAKeyPair(dataDir string) (*rsa.PrivateKey, error) {
	privPath := filepath.Join(dataDir, privateKeyFile)
	pubPath := filepath.Join(dataDir, publicKeyFile)

	// 尝试加载已有密钥
	if privKey, err := loadPrivateKey(privPath); err == nil {
		return privKey, nil
	}

	// 生成新密钥对
	privKey, err := rsa.GenerateKey(rand.Reader, rsaKeyBits)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dataDir, err)
	}

	// 写私钥
	privDER := x509.MarshalPKCS1PrivateKey(privKey)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDER})
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}

	// 写公钥
	pubDER, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		return nil, fmt.Errorf("write public key: %w", err)
	}

	return privKey, nil
}

// LoadPublicKeyPEM 从 dataDir 加载公钥 PEM 字节。
func LoadPublicKeyPEM(dataDir string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dataDir, publicKeyFile))
}

// ParsePublicKeyPEM 解析 PEM 编码的公钥。
func ParsePublicKeyPEM(pemData []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return rsaPub, nil
}

// EncryptForDownload 为指定用户加密 Skill zip 包。
// 流程: 生成 salt → PBKDF2 派生 AES key → AES-GCM 加密 zip → RSA-OAEP 加密 salt。
func EncryptForDownload(zipData []byte, userID string, rsaPrivKey *rsa.PrivateKey) (*EncryptedPackage, error) {
	// 生成随机 salt
	salt := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	// PBKDF2 派生 AES key
	aesKey := pbkdf2.Key([]byte(userID), salt, pbkdf2Iter, aesKeyLen, sha256.New)

	// AES-256-GCM 加密 zip
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	encryptedZip := gcm.Seal(nonce, nonce, zipData, nil) // nonce 前置于密文

	// RSA-OAEP 加密 salt：用公钥加密，客户端用私钥解密。
	// 实际部署中客户端通过安全通道获取解密能力（服务端代为解密或分发临时密钥）。
	// 这里的 RSA 层主要防止离线暴力破解 salt。
	encryptedSalt, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &rsaPrivKey.PublicKey, salt, nil)
	if err != nil {
		return nil, fmt.Errorf("rsa encrypt salt: %w", err)
	}

	return &EncryptedPackage{
		EncryptedSalt: encryptedSalt,
		EncryptedZip:  encryptedZip,
	}, nil
}

// DecryptDownload 客户端解密下载包。
// 使用 RSA 私钥解密 salt，然后 PBKDF2 派生 AES key，最后 AES-GCM 解密 zip。
func DecryptDownload(pkg *EncryptedPackage, userID string, rsaPrivKey *rsa.PrivateKey) ([]byte, error) {
	// RSA-OAEP 解密 salt
	salt, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, rsaPrivKey, pkg.EncryptedSalt, nil)
	if err != nil {
		return nil, fmt.Errorf("rsa decrypt salt: %w", err)
	}

	// PBKDF2 派生 AES key
	aesKey := pbkdf2.Key([]byte(userID), salt, pbkdf2Iter, aesKeyLen, sha256.New)

	// AES-256-GCM 解密 zip
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(pkg.EncryptedZip) < nonceSize {
		return nil, fmt.Errorf("encrypted zip too short")
	}
	plaintext, err := gcm.Open(nil, pkg.EncryptedZip[:nonceSize], pkg.EncryptedZip[nonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("aes decrypt: %w", err)
	}
	return plaintext, nil
}

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in %s", path)
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}
