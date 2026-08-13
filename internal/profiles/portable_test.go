package profiles

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestPortableRoundTripAndCompatibleParameters(t *testing.T) {
	plain := []byte("source:\n  password: do-not-leak\n")
	passphrase := []byte("portable passphrase")
	for _, test := range []struct {
		name string
		seal func() ([]byte, error)
	}{
		{"defaults", func() ([]byte, error) { return SealPortable(plain, passphrase) }},
		{"in-range-v1-cost", func() ([]byte, error) {
			return sealPortableWithParameters(plain, passphrase, 2, minPortableMemoryKiB, 2)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			exported, err := test.seal()
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(exported, plain) || bytes.Contains(exported, passphrase) {
				t.Fatal("portable export contains secret plaintext")
			}
			opened, err := OpenPortable(exported, passphrase)
			if err != nil || !bytes.Equal(opened, plain) {
				t.Fatalf("round trip = %q, %v", opened, err)
			}
		})
	}
}

func TestPortableMaximumPermittedPlaintextRoundTrips(t *testing.T) {
	plain := bytes.Repeat([]byte{'p'}, maxPortablePlaintextSize)
	passphrase := []byte("portable passphrase")
	exported, err := SealPortable(plain, passphrase)
	if err != nil {
		t.Fatalf("seal maximum plaintext: %v", err)
	}
	if len(exported) > maxPortableEnvelopeSize {
		t.Fatalf("export size = %d, limit = %d", len(exported), maxPortableEnvelopeSize)
	}
	opened, err := OpenPortable(exported, passphrase)
	if err != nil {
		t.Fatalf("open maximum plaintext: %v", err)
	}
	if !bytes.Equal(opened, plain) {
		t.Fatal("maximum plaintext did not round trip")
	}
	if _, err := SealPortable(append(plain, 'x'), passphrase); err == nil {
		t.Fatal("plaintext beyond the portable limit was accepted")
	}
}

func TestPortableRefusesMalformedAndTamperedInputsWithoutLeaks(t *testing.T) {
	plain := []byte("plaintext-profile-value")
	passphrase := []byte("portable-passphrase-value")
	exported, err := SealPortable(plain, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(exported, &envelope); err != nil {
		t.Fatal(err)
	}
	encoded := func(change func(map[string]any)) []byte {
		copy := make(map[string]any, len(envelope))
		for key, value := range envelope {
			copy[key] = value
		}
		change(copy)
		data, marshalErr := json.Marshal(copy)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return data
	}
	cases := map[string][]byte{
		"wrong passphrase": exported,
		"malformed json":   []byte(`{"format":`),
		"unknown field":    encoded(func(value map[string]any) { value["unknown"] = true }),
		"trailing json":    append(append([]byte(nil), exported...), []byte(" {}")...),
		"bad salt":         encoded(func(value map[string]any) { value["salt"] = "%%%" }),
		"bad nonce":        encoded(func(value map[string]any) { value["nonce"] = "%%%" }),
		"bad ciphertext":   encoded(func(value map[string]any) { value["ciphertext"] = "%%%" }),
		"ciphertext tamper": encoded(func(value map[string]any) {
			value["ciphertext"] = base64.RawStdEncoding.EncodeToString([]byte("tampered"))
		}),
		"excessive memory": encoded(func(value map[string]any) { value["memory_kib"] = float64(maxPortableMemoryKiB + 1) }),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			usedPassphrase := passphrase
			if name == "wrong passphrase" {
				usedPassphrase = []byte("wrong")
			}
			_, openErr := OpenPortable(data, usedPassphrase)
			if openErr == nil {
				t.Fatal("unsafe portable input was accepted")
			}
			if strings.Contains(openErr.Error(), string(plain)) || strings.Contains(openErr.Error(), string(passphrase)) {
				t.Fatalf("error leaked secret: %v", openErr)
			}
		})
	}
}

func TestPortableHeaderHasRequiredFields(t *testing.T) {
	data, err := SealPortable([]byte("config"), []byte("passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"format", "version", "kdf", "time", "memory_kib", "parallelism", "key_length", "salt", "cipher", "nonce", "ciphertext"} {
		if _, ok := envelope[key]; !ok {
			t.Fatalf("portable header omitted %s", key)
		}
	}
}
