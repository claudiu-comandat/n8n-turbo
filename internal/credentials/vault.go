package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

type Vault struct {
	key    []byte
	shaKey []byte
}

func NewVault(encryptionKey string) (*Vault, error) {
	if strings.TrimSpace(encryptionKey) == "" {
		return nil, fmt.Errorf("encryption key cannot be empty")
	}
	return &Vault{key: deriveKey(encryptionKey), shaKey: deriveKeySHA([]byte(encryptionKey))}, nil
}

func (v *Vault) Encrypt(plaintext string) (string, error) {
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", fmt.Errorf("generate iv: %w", err)
	}
	return v.encryptLegacyHex(plaintext, iv)
}

func (v *Vault) encryptLegacyHex(plaintext string, iv []byte) (string, error) {
	if len(iv) != aes.BlockSize {
		return "", fmt.Errorf("invalid iv length")
	}
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	plainBytes := pkcs7Pad([]byte(plaintext), aes.BlockSize)
	ciphertext := make([]byte, len(plainBytes))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plainBytes)
	return hex.EncodeToString(iv) + ":" + hex.EncodeToString(ciphertext), nil
}

func (v *Vault) Decrypt(encrypted string) (string, error) {
	if strings.HasPrefix(encrypted, "v2:") {
		return v.decryptBase64(strings.TrimPrefix(encrypted, "v2:"))
	}
	if !strings.Contains(encrypted, ":") {
		return v.decryptBase64(encrypted)
	}
	return v.decryptLegacyHex(encrypted)
}

func IsEncryptedValue(value string) bool {
	if strings.HasPrefix(value, "v2:") {
		return true
	}
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 || len(parts[0]) != aes.BlockSize*2 || len(parts[1]) == 0 || len(parts[1])%(aes.BlockSize*2) != 0 {
		return false
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return false
	}
	return true
}

func (v *Vault) decryptBase64(encrypted string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	if len(decoded) < aes.BlockSize || len(decoded[aes.BlockSize:])%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid ciphertext length")
	}
	iv := decoded[:aes.BlockSize]
	ciphertext := decoded[aes.BlockSize:]
	block, err := aes.NewCipher(v.shaKey)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	unpadded, err := pkcs7Unpad(plaintext)
	if err != nil {
		return "", fmt.Errorf("unpad: %w", err)
	}
	return string(unpadded), nil
}

func (v *Vault) decryptLegacyHex(encrypted string) (string, error) {
	parts := strings.SplitN(encrypted, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid encrypted format")
	}
	iv, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("decode iv: %w", err)
	}
	if len(iv) != aes.BlockSize {
		return "", fmt.Errorf("invalid iv length")
	}
	ciphertext, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid ciphertext length")
	}
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	unpadded, err := pkcs7Unpad(plaintext)
	if err != nil {
		return "", fmt.Errorf("unpad: %w", err)
	}
	return string(unpadded), nil
}

func deriveKey(encryptionKey string) []byte {
	padded := encryptionKey
	for len(padded) < 32 {
		padded += " "
	}
	return []byte(padded)[:32]
}

func deriveKeySHA(key []byte) []byte {
	sum := sha256.Sum256(key)
	return sum[:]
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	return padded
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid padding")
	}
	for i := len(data) - padding; i < len(data); i++ {
		if int(data[i]) != padding {
			return nil, fmt.Errorf("invalid padding byte")
		}
	}
	return data[:len(data)-padding], nil
}
