package nodes

import (
	"bytes"
	"context"
	stdcrypto "crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"hash"
	"math/big"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Crypto struct{}

type cryptoConfig struct {
	Action            string
	Value             any
	Encoding          string
	Algorithm         string
	SecretKey         any
	PrivateKey        any
	PublicKey         any
	Signature         any
	SignatureEncoding string
	KeyType           string
	RSABitLength      int
	ECDSACurve        string
	AESKey            any
	AESAlgorithm      string
	AESIV             any
	OutputField       string
}

type ecdsaSignature struct {
	R *big.Int
	S *big.Int
}

func (Crypto) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	params := cryptoParams(in.Node.Parameters)
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	result := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		next, err := executeCryptoItem(in, params, items, item, index)
		if err != nil {
			return nil, fmt.Errorf("crypto item %d: %w", index, err)
		}
		result = append(result, next)
	}
	return dataplane.MainOutput(result), nil
}

func cryptoParams(params map[string]any) cryptoConfig {
	outputField := firstNonEmptyNode(stringParam(params, "outputFieldName", "destinationField"), "data")
	if outputField == "data" && stringParam(params, "fieldName", "sourceField") != "" {
		outputField = firstNonEmptyNode(stringParam(params, "outputFieldName", "destinationField"), "hash")
	}
	return cryptoConfig{
		Action:            firstNonEmptyNode(stringParam(params, "action", "operation"), "hash"),
		Value:             firstNonNil(params["value"], params["text"], params["data"]),
		Encoding:          firstNonEmptyNode(stringParam(params, "encoding"), "hex"),
		Algorithm:         strings.ToUpper(firstNonEmptyNode(stringParam(params, "algorithm"), "SHA256")),
		SecretKey:         firstNonNil(params["secretKey"], params["key"], params["secret"]),
		PrivateKey:        params["privateKey"],
		PublicKey:         params["publicKey"],
		Signature:         params["signature"],
		SignatureEncoding: firstNonEmptyNode(stringParam(params, "signatureEncoding"), stringParam(params, "encoding"), "hex"),
		KeyType:           strings.ToUpper(firstNonEmptyNode(stringParam(params, "keyType"), "RSA")),
		RSABitLength:      intParam(params, "rsaBitLength", 2048),
		ECDSACurve:        firstNonEmptyNode(stringParam(params, "ecdsaCurve"), "P-256"),
		AESKey:            firstNonNil(params["aesKey"], params["key"]),
		AESAlgorithm:      strings.ToLower(firstNonEmptyNode(stringParam(params, "aesAlgorithm", "algorithm"), "aes-256-gcm")),
		AESIV:             firstNonNil(params["aesIv"], params["aesIV"], params["iv"]),
		OutputField:       outputField,
	}
}

func executeCryptoItem(in engine.ExecuteInput, params cryptoConfig, items []dataplane.Item, item dataplane.Item, index int) (dataplane.Item, error) {
	switch strings.ToLower(params.Action) {
	case "hash":
		return cryptoHash(in, params, items, item, index)
	case "hmac":
		return cryptoHMAC(in, params, items, item, index)
	case "sign":
		return cryptoSign(in, params, items, item, index)
	case "verify":
		return cryptoVerify(in, params, items, item, index)
	case "generatekeypair", "generate-key-pair":
		return cryptoGenerateKeyPair(params)
	case "encrypt":
		return cryptoEncrypt(in, params, items, item, index)
	case "decrypt":
		return cryptoDecrypt(in, params, items, item, index)
	default:
		return dataplane.Item{}, fmt.Errorf("unsupported crypto action %s", params.Action)
	}
}

func cryptoHash(in engine.ExecuteInput, params cryptoConfig, items []dataplane.Item, item dataplane.Item, index int) (dataplane.Item, error) {
	value := cryptoValue(in, params, items, item, index)
	hashFunc, err := cryptoHashFunc(params.Algorithm)
	if err != nil {
		return dataplane.Item{}, err
	}
	h := hashFunc()
	_, _ = h.Write([]byte(value))
	return cryptoOutput(item, params.OutputField, encodeCryptoBytes(h.Sum(nil), params.Encoding)), nil
}

func cryptoHMAC(in engine.ExecuteInput, params cryptoConfig, items []dataplane.Item, item dataplane.Item, index int) (dataplane.Item, error) {
	value := cryptoValue(in, params, items, item, index)
	secret := fmt.Sprint(resolveValue(in, items, index, params.SecretKey))
	hashFunc, err := cryptoHashFunc(params.Algorithm)
	if err != nil {
		return dataplane.Item{}, err
	}
	mac := hmac.New(hashFunc, []byte(secret))
	_, _ = mac.Write([]byte(value))
	return cryptoOutput(item, params.OutputField, encodeCryptoBytes(mac.Sum(nil), params.Encoding)), nil
}

func cryptoSign(in engine.ExecuteInput, params cryptoConfig, items []dataplane.Item, item dataplane.Item, index int) (dataplane.Item, error) {
	value := cryptoValue(in, params, items, item, index)
	privateKey := fmt.Sprint(resolveValue(in, items, index, params.PrivateKey))
	key, err := parsePrivateKey(privateKey)
	if err != nil {
		return dataplane.Item{}, err
	}
	digest := sha256.Sum256([]byte(value))
	var signature []byte
	switch typed := key.(type) {
	case *rsa.PrivateKey:
		signature, err = rsa.SignPKCS1v15(rand.Reader, typed, stdcrypto.SHA256, digest[:])
	case *ecdsa.PrivateKey:
		r, s, signErr := ecdsa.Sign(rand.Reader, typed, digest[:])
		err = signErr
		if err == nil {
			signature, err = asn1.Marshal(ecdsaSignature{R: r, S: s})
		}
	default:
		err = fmt.Errorf("unsupported private key type")
	}
	if err != nil {
		return dataplane.Item{}, err
	}
	return cryptoOutput(item, params.OutputField, encodeCryptoBytes(signature, params.SignatureEncoding)), nil
}

func cryptoVerify(in engine.ExecuteInput, params cryptoConfig, items []dataplane.Item, item dataplane.Item, index int) (dataplane.Item, error) {
	value := cryptoValue(in, params, items, item, index)
	publicKey := fmt.Sprint(resolveValue(in, items, index, params.PublicKey))
	signatureValue := fmt.Sprint(resolveValue(in, items, index, params.Signature))
	signature, err := decodeCryptoBytes(signatureValue, params.SignatureEncoding)
	if err != nil {
		return dataplane.Item{}, err
	}
	key, err := parsePublicKey(publicKey)
	if err != nil {
		return dataplane.Item{}, err
	}
	digest := sha256.Sum256([]byte(value))
	valid := false
	switch typed := key.(type) {
	case *rsa.PublicKey:
		valid = rsa.VerifyPKCS1v15(typed, stdcrypto.SHA256, digest[:], signature) == nil
	case *ecdsa.PublicKey:
		var sig ecdsaSignature
		if _, err := asn1.Unmarshal(signature, &sig); err == nil && sig.R != nil && sig.S != nil {
			valid = ecdsa.Verify(typed, digest[:], sig.R, sig.S)
		}
	default:
		return dataplane.Item{}, fmt.Errorf("unsupported public key type")
	}
	return cryptoOutput(item, params.OutputField, valid), nil
}

func cryptoGenerateKeyPair(params cryptoConfig) (dataplane.Item, error) {
	switch params.KeyType {
	case "RSA", "":
		bits := params.RSABitLength
		if bits < 1024 || bits > 8192 {
			return dataplane.Item{}, fmt.Errorf("invalid RSA bit length %d", bits)
		}
		key, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			return dataplane.Item{}, err
		}
		privatePEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		publicBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			return dataplane.Item{}, err
		}
		publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicBytes})
		return dataplane.Item{JSON: map[string]any{"privateKey": string(privatePEM), "publicKey": string(publicPEM), "keyType": "RSA", "bitLength": bits}}, nil
	case "ECDSA", "EC":
		curve, curveName, err := cryptoCurve(params.ECDSACurve)
		if err != nil {
			return dataplane.Item{}, err
		}
		key, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			return dataplane.Item{}, err
		}
		privateBytes, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return dataplane.Item{}, err
		}
		publicBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			return dataplane.Item{}, err
		}
		return dataplane.Item{JSON: map[string]any{"privateKey": string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateBytes})), "publicKey": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicBytes})), "keyType": "ECDSA", "curve": curveName}}, nil
	default:
		return dataplane.Item{}, fmt.Errorf("unsupported key type %s", params.KeyType)
	}
}

func cryptoEncrypt(in engine.ExecuteInput, params cryptoConfig, items []dataplane.Item, item dataplane.Item, index int) (dataplane.Item, error) {
	value := cryptoValue(in, params, items, item, index)
	key, err := decodeAESKey(fmt.Sprint(resolveValue(in, items, index, params.AESKey)))
	if err != nil {
		return dataplane.Item{}, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return dataplane.Item{}, err
	}
	switch params.AESAlgorithm {
	case "", "aes-128-gcm", "aes-192-gcm", "aes-256-gcm":
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return dataplane.Item{}, err
		}
		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return dataplane.Item{}, err
		}
		ciphertext := gcm.Seal(nil, nonce, []byte(value), nil)
		payload := append(append([]byte{}, nonce...), ciphertext...)
		next := cryptoOutput(item, params.OutputField, encodeCryptoBytes(payload, params.Encoding))
		next.JSON["nonce"] = encodeCryptoBytes(nonce, "hex")
		return next, nil
	case "aes-128-cbc", "aes-192-cbc", "aes-256-cbc":
		iv, err := cryptoIV(in, params, items, index)
		if err != nil {
			return dataplane.Item{}, err
		}
		plaintext := pkcs7Pad([]byte(value), aes.BlockSize)
		ciphertext := make([]byte, len(plaintext))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plaintext)
		next := cryptoOutput(item, params.OutputField, encodeCryptoBytes(ciphertext, params.Encoding))
		next.JSON["iv"] = encodeCryptoBytes(iv, "hex")
		return next, nil
	default:
		return dataplane.Item{}, fmt.Errorf("unsupported AES algorithm %s", params.AESAlgorithm)
	}
}

func cryptoDecrypt(in engine.ExecuteInput, params cryptoConfig, items []dataplane.Item, item dataplane.Item, index int) (dataplane.Item, error) {
	value := cryptoValue(in, params, items, item, index)
	key, err := decodeAESKey(fmt.Sprint(resolveValue(in, items, index, params.AESKey)))
	if err != nil {
		return dataplane.Item{}, err
	}
	ciphertext, err := decodeCryptoBytes(value, params.Encoding)
	if err != nil {
		return dataplane.Item{}, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return dataplane.Item{}, err
	}
	switch params.AESAlgorithm {
	case "", "aes-128-gcm", "aes-192-gcm", "aes-256-gcm":
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return dataplane.Item{}, err
		}
		if len(ciphertext) < gcm.NonceSize() {
			return dataplane.Item{}, fmt.Errorf("ciphertext too short")
		}
		nonce := ciphertext[:gcm.NonceSize()]
		payload := ciphertext[gcm.NonceSize():]
		plaintext, err := gcm.Open(nil, nonce, payload, nil)
		if err != nil {
			return dataplane.Item{}, err
		}
		return cryptoOutput(item, params.OutputField, string(plaintext)), nil
	case "aes-128-cbc", "aes-192-cbc", "aes-256-cbc":
		iv, err := cryptoIV(in, params, items, index)
		if err != nil {
			return dataplane.Item{}, err
		}
		if len(ciphertext)%aes.BlockSize != 0 {
			return dataplane.Item{}, fmt.Errorf("ciphertext is not block aligned")
		}
		plaintext := make([]byte, len(ciphertext))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
		plaintext, err = pkcs7Unpad(plaintext)
		if err != nil {
			return dataplane.Item{}, err
		}
		return cryptoOutput(item, params.OutputField, string(plaintext)), nil
	default:
		return dataplane.Item{}, fmt.Errorf("unsupported AES algorithm %s", params.AESAlgorithm)
	}
}

func cryptoValue(in engine.ExecuteInput, params cryptoConfig, items []dataplane.Item, item dataplane.Item, index int) string {
	if params.Value != nil {
		return fmt.Sprint(resolveValue(in, items, index, params.Value))
	}
	field := stringParam(in.Node.Parameters, "fieldName", "sourceField")
	if field != "" {
		return fmt.Sprint(item.JSON[field])
	}
	return ""
}

func cryptoOutput(item dataplane.Item, field string, value any) dataplane.Item {
	next := cloneItem(item)
	next.JSON[field] = value
	return next
}

func cryptoHashFunc(algorithm string) (func() hash.Hash, error) {
	switch strings.ToUpper(strings.ReplaceAll(algorithm, "-", "")) {
	case "MD5":
		return md5.New, nil
	case "SHA1":
		return sha1.New, nil
	case "SHA224":
		return sha256.New224, nil
	case "SHA256", "":
		return sha256.New, nil
	case "SHA384":
		return sha512.New384, nil
	case "SHA512":
		return sha512.New, nil
	default:
		return nil, fmt.Errorf("unsupported hash algorithm %s", algorithm)
	}
}

func encodeCryptoBytes(data []byte, encoding string) string {
	switch strings.ToLower(encoding) {
	case "base64":
		return base64.StdEncoding.EncodeToString(data)
	case "latin1", "binary":
		return string(data)
	default:
		return hex.EncodeToString(data)
	}
}

func decodeCryptoBytes(value string, encoding string) ([]byte, error) {
	switch strings.ToLower(encoding) {
	case "base64":
		return base64.StdEncoding.DecodeString(value)
	case "latin1", "binary":
		return []byte(value), nil
	default:
		return hex.DecodeString(value)
	}
}

func decodeAESKey(value string) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf("AES key is required")
	}
	if decoded, err := hex.DecodeString(value); err == nil && validAESKeyLength(len(decoded)) {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && validAESKeyLength(len(decoded)) {
		return decoded, nil
	}
	raw := []byte(value)
	if validAESKeyLength(len(raw)) {
		return raw, nil
	}
	return nil, fmt.Errorf("invalid AES key length %d", len(raw))
}

func validAESKeyLength(length int) bool {
	return length == 16 || length == 24 || length == 32
}

func cryptoIV(in engine.ExecuteInput, params cryptoConfig, items []dataplane.Item, index int) ([]byte, error) {
	raw := strings.TrimSpace(fmt.Sprint(resolveValue(in, items, index, params.AESIV)))
	if raw == "" || raw == "<nil>" {
		iv := make([]byte, aes.BlockSize)
		_, err := rand.Read(iv)
		return iv, err
	}
	if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) == aes.BlockSize {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == aes.BlockSize {
		return decoded, nil
	}
	if len([]byte(raw)) == aes.BlockSize {
		return []byte(raw), nil
	}
	return nil, fmt.Errorf("invalid AES IV")
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(padding)}, padding)...)
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty PKCS7 data")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid PKCS7 padding")
	}
	for _, value := range data[len(data)-padding:] {
		if int(value) != padding {
			return nil, fmt.Errorf("invalid PKCS7 padding")
		}
	}
	return data[:len(data)-padding], nil
}

func parsePrivateKey(value string) (any, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, fmt.Errorf("invalid private key PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("unsupported private key")
}

func parsePublicKey(value string) (any, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, fmt.Errorf("invalid public key PEM")
	}
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("unsupported public key")
}

func cryptoCurve(name string) (elliptic.Curve, string, error) {
	switch strings.ToUpper(name) {
	case "", "P-256", "P256":
		return elliptic.P256(), "P-256", nil
	case "P-384", "P384":
		return elliptic.P384(), "P-384", nil
	case "P-521", "P521":
		return elliptic.P521(), "P-521", nil
	default:
		return nil, "", fmt.Errorf("unsupported ECDSA curve %s", name)
	}
}
