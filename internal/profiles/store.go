// Package profiles stores portable migration configurations without storing
// their plaintext in the profile database.
package profiles

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/johndauphine/dmtx/internal/secrets"

	_ "modernc.org/sqlite"
)

const formatV1 = "v1:"

// Store owns one encrypted SQLite profile database.
type Store struct {
	db   *sql.DB
	path string
	gcm  cipher.AEAD
}

// OpenWithSecrets obtains the profile-sealing key exclusively from the
// protected secrets file before opening the owned encrypted store. The key is
// never included in an error or returned to the caller.
//
// EnsureMasterKey creates and persists a new key only when the encryption
// section exists with an empty master_key, so profile data is never sealed with
// an ephemeral, unrecoverable key.
func OpenWithSecrets(path, secretsPath string) (*Store, error) {
	key, err := secrets.EnsureMasterKey(secretsPath)
	if err != nil {
		return nil, fmt.Errorf("prepare profile store key: %w", err)
	}
	return Open(path, key)
}

// Open creates or opens path. key must be the 32-byte decoded master key from
// the protected secrets file; callers must never place it in a configuration.
func Open(path string, key []byte) (*Store, error) {
	if len(key) != 32 {
		return nil, errors.New("profile master key must be 32 bytes")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create profile directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("restrict profile directory: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open profile store: %w", err)
	}
	store := &Store{db: db, path: path, gcm: gcm}
	if _, err = db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE IF NOT EXISTS profiles (name TEXT PRIMARY KEY, payload BLOB NOT NULL);`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialise profile store: %w", err)
	}
	if err := store.restrictFiles(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) restrictFiles() error {
	for _, path := range []string{store.path, store.path + "-wal", store.path + "-shm"} {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("restrict profile file: %w", err)
		}
	}
	return nil
}

// Save seals raw YAML before it enters SQLite. A profile name is intentionally
// metadata only; no configuration value is used as a SQL identifier.
func (store *Store) Save(name string, config []byte) error {
	if name == "" {
		return errors.New("profile name is required")
	}
	payload, err := store.seal(config)
	if err != nil {
		return err
	}
	if _, err = store.db.Exec(`INSERT INTO profiles(name,payload) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET payload=excluded.payload`, name, payload); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	return store.restrictFiles()
}

// List returns profile names only. Configuration bytes remain sealed in SQLite
// until Load is explicitly requested.
func (store *Store) List() ([]string, error) {
	rows, err := store.db.Query(`SELECT name FROM profiles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read profile name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	return names, nil
}

// Delete removes one encrypted profile by name. A missing name is successful:
// the requested postcondition is that it is absent.
func (store *Store) Delete(name string) error {
	if name == "" {
		return errors.New("profile name is required")
	}
	if _, err := store.db.Exec(`DELETE FROM profiles WHERE name=?`, name); err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	return store.restrictFiles()
}

// Load returns only decrypted configuration bytes for the requested profile.
func (store *Store) Load(name string) ([]byte, error) {
	var payload []byte
	if err := store.db.QueryRow(`SELECT payload FROM profiles WHERE name=?`, name).Scan(&payload); err != nil {
		return nil, fmt.Errorf("load profile: %w", err)
	}
	return store.open(payload)
}

func (store *Store) Close() error { return store.db.Close() }

func (store *Store) seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, store.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return []byte(formatV1 + base64.RawStdEncoding.EncodeToString(append(nonce, store.gcm.Seal(nil, nonce, plain, nil)...))), nil
}

func (store *Store) open(payload []byte) ([]byte, error) {
	if len(payload) < len(formatV1) || string(payload[:len(formatV1)]) != formatV1 {
		return nil, errors.New("unsupported encrypted profile format")
	}
	encoded, err := base64.RawStdEncoding.DecodeString(string(payload[len(formatV1):]))
	if err != nil {
		return nil, errors.New("invalid encrypted profile payload")
	}
	if len(encoded) < store.gcm.NonceSize() {
		return nil, errors.New("invalid encrypted profile payload")
	}
	plain, err := store.gcm.Open(nil, encoded[:store.gcm.NonceSize()], encoded[store.gcm.NonceSize():], nil)
	if err != nil {
		return nil, errors.New("cannot decrypt profile")
	}
	return plain, nil
}
