package profiles

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

const (
	portableFormat  = "dmtx-profile-export"
	portableVersion = 1
	portableTime    = 3
	portableMemory  = 64 * 1024 // KiB
	portableThreads = 1
	portableKeyLen  = 32
	portableSaltLen = 16
	// maxPortableEnvelopeSize is the on-disk JSON transfer-file limit.  The
	// ciphertext is base64 encoded, so its permitted plaintext must be smaller
	// than this limit.  Keeping the two limits separate prevents SealPortable
	// from creating an export which OpenPortable will reject before it can be
	// authenticated.
	maxPortableEnvelopeSize  = 16 << 20 // profiles are configuration, never bulk data
	maxPortablePlaintextSize = ((maxPortableEnvelopeSize - 1024) * 3 / 4) - portableTagLen
	portableTagLen           = 16 // AES-GCM authentication tag length

	minPortableTime        = 1
	maxPortableTime        = 10
	minPortableMemoryKiB   = 32 * 1024
	maxPortableMemoryKiB   = 512 * 1024
	minPortableParallelism = 1
	maxPortableParallelism = 8
)

// portableHeader is authenticated AES-GCM additional data. Parameter bounds
// are intentionally narrow: an import must not let an untrusted file turn a
// password attempt into an unbounded memory allocation.
type portableHeader struct {
	Format      string `json:"format"`
	Version     int    `json:"version"`
	KDF         string `json:"kdf"`
	Time        uint32 `json:"time"`
	MemoryKiB   uint32 `json:"memory_kib"`
	Parallelism uint8  `json:"parallelism"`
	KeyLength   uint32 `json:"key_length"`
	Salt        string `json:"salt"`
	Cipher      string `json:"cipher"`
	Nonce       string `json:"nonce"`
}

type portableEnvelope struct {
	portableHeader
	Ciphertext string `json:"ciphertext"`
}

// SealPortable produces a self-describing, versioned profile transfer file.
// The store master key is never involved in this format.
func SealPortable(plain, passphrase []byte) ([]byte, error) {
	return sealPortableWithParameters(plain, passphrase, portableTime, portableMemory, portableThreads)
}

// sealPortableWithParameters exists so the v1 compatibility bounds can be
// tested. Production exports always use the defaults above.
func sealPortableWithParameters(plain, passphrase []byte, time, memory uint32, parallelism uint8) ([]byte, error) {
	if len(passphrase) == 0 {
		return nil, errors.New("passphrase is empty")
	}
	if len(plain) > maxPortablePlaintextSize {
		return nil, errors.New("portable profile is too large")
	}
	salt := make([]byte, portableSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate export salt: %w", err)
	}
	key := argon2.IDKey(passphrase, salt, time, memory, parallelism, portableKeyLen)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("prepare portable encryption: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("prepare portable encryption: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate export nonce: %w", err)
	}
	header := portableHeader{portableFormat, portableVersion, "argon2id", time, memory, parallelism, portableKeyLen, base64.RawStdEncoding.EncodeToString(salt), "aes-256-gcm", base64.RawStdEncoding.EncodeToString(nonce)}
	aad, err := json.Marshal(header)
	if err != nil {
		return nil, fmt.Errorf("encode portable header: %w", err)
	}
	envelope := portableEnvelope{portableHeader: header, Ciphertext: base64.RawStdEncoding.EncodeToString(gcm.Seal(nil, nonce, plain, aad))}
	return json.Marshal(envelope)
}

// OpenPortable authenticates and decrypts a profile transfer file. Errors are
// intentionally generic so neither passphrases nor decrypted configuration
// values can cross this boundary.
func OpenPortable(data, passphrase []byte) ([]byte, error) {
	if len(passphrase) == 0 {
		return nil, errors.New("portable profile authentication failed")
	}
	if len(data) == 0 || len(data) > maxPortableEnvelopeSize {
		return nil, errors.New("invalid portable profile format")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope portableEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, errors.New("invalid portable profile format")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("invalid portable profile format")
	}
	if err := validatePortableHeader(envelope.portableHeader); err != nil {
		return nil, err
	}
	salt, _ := base64.RawStdEncoding.DecodeString(envelope.Salt)
	nonce, _ := base64.RawStdEncoding.DecodeString(envelope.Nonce)
	ciphertext, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, errors.New("invalid portable profile format")
	}
	key := argon2.IDKey(passphrase, salt, envelope.Time, envelope.MemoryKiB, envelope.Parallelism, envelope.KeyLength)
	defer clear(key)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	aad, _ := json.Marshal(envelope.portableHeader)
	plain, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, errors.New("portable profile authentication failed")
	}
	if len(plain) > maxPortablePlaintextSize {
		clear(plain)
		return nil, errors.New("invalid portable profile format")
	}
	return plain, nil
}

func validatePortableHeader(header portableHeader) error {
	if header.Format != portableFormat || header.Version != portableVersion || header.KDF != "argon2id" || header.Cipher != "aes-256-gcm" || header.KeyLength != portableKeyLen {
		return errors.New("unsupported portable profile format")
	}
	if header.Time < minPortableTime || header.Time > maxPortableTime || header.MemoryKiB < minPortableMemoryKiB || header.MemoryKiB > maxPortableMemoryKiB || header.Parallelism < minPortableParallelism || header.Parallelism > maxPortableParallelism {
		return errors.New("unsupported portable profile parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(header.Salt)
	if err != nil || len(salt) != portableSaltLen {
		return errors.New("invalid portable profile format")
	}
	nonce, err := base64.RawStdEncoding.DecodeString(header.Nonce)
	if err != nil || len(nonce) != 12 {
		return errors.New("invalid portable profile format")
	}
	return nil
}

func clear(data []byte) {
	for index := range data {
		data[index] = 0
	}
}
