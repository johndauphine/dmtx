package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/profiles"
	"github.com/johndauphine/dmtx/internal/secrets"
)

const (
	maxProfilePassphraseFile = 4 << 10
	maxPortableProfileFile   = 16 << 20
)

func profilePaths() (string, string, error) {
	secretsPath, err := secrets.Path()
	if err != nil {
		return "", "", errors.New("locate profile storage")
	}
	return filepath.Join(filepath.Dir(secretsPath), "profiles.db"), secretsPath, nil
}

func openProfileStore() (*profiles.Store, error) {
	profilesPath, secretsPath, err := profilePaths()
	if err != nil {
		return nil, err
	}
	return profiles.OpenWithSecrets(profilesPath, secretsPath)
}

// configurationData resolves exactly one supported configuration origin. A
// loaded profile is returned only as bytes to the normal config parser; it is
// never materialised as a temporary plaintext file.
func configurationData(request Request) ([]byte, string, error) {
	profilesPath, secretsPath, err := profilePaths()
	if err != nil {
		return nil, "", err
	}
	return configurationDataAt(request, profilesPath, secretsPath)
}

func configurationDataAt(request Request, profilesPath, secretsPath string) ([]byte, string, error) {
	if request.ConfigPath != "" && request.ProfileName != "" {
		return nil, "", errors.New("choose either configuration path or profile")
	}
	if request.ProfileName == "" {
		if request.ConfigPath == "" {
			return nil, "", errors.New("configuration origin is required")
		}
		data, err := os.ReadFile(request.ConfigPath)
		if err != nil {
			return nil, "", err
		}
		return data, request.ConfigPath, nil
	}
	store, err := profiles.OpenWithSecrets(profilesPath, secretsPath)
	if err != nil {
		return nil, "", fmt.Errorf("open encrypted profile store: %w", err)
	}
	defer store.Close()
	data, err := store.Load(request.ProfileName)
	if err != nil {
		return nil, "", fmt.Errorf("load encrypted profile: %w", err)
	}
	return data, "profile " + request.ProfileName, nil
}

func executeProfile(request Request) Outcome {
	out := newOutcome(request.Command)
	return executeProfileWithStore(out, request, openProfileStore)
}

func executeProfileWithStore(
	out *outcomeBuilder,
	request Request,
	open func() (*profiles.Store, error),
) Outcome {
	switch request.ProfileAction {
	case "save":
		if request.ProfileName == "" || request.ConfigPath == "" {
			return out.failWith(ConfigurationError, "usage: dmtx profile save NAME --config migration.yaml")
		}
		data, err := os.ReadFile(request.ConfigPath)
		if err != nil {
			return out.failWith(FileError, "read configuration: "+err.Error())
		}
		if _, err := config.Parse(data); err != nil {
			return out.failWith(ConfigurationError, "configuration: "+err.Error())
		}
		store, err := open()
		if err != nil {
			return out.failWith(FileError, "open encrypted profile store: "+err.Error())
		}
		defer store.Close()
		if err := store.Save(request.ProfileName, data); err != nil {
			return out.failWith(FileError, "save encrypted profile: "+err.Error())
		}
		out.out("saved encrypted profile " + request.ProfileName)
		return out.done(Success)
	case "list":
		store, err := open()
		if err != nil {
			return out.failWith(FileError, "open encrypted profile store: "+err.Error())
		}
		defer store.Close()
		names, err := store.List()
		if err != nil {
			return out.failWith(FileError, "list encrypted profiles: "+err.Error())
		}
		for _, name := range names {
			out.out(name)
		}
		return out.done(Success)
	case "delete":
		if request.ProfileName == "" {
			return out.failWith(ConfigurationError, "usage: dmtx profile delete NAME")
		}
		store, err := open()
		if err != nil {
			return out.failWith(FileError, "open encrypted profile store: "+err.Error())
		}
		defer store.Close()
		if err := store.Delete(request.ProfileName); err != nil {
			return out.failWith(FileError, "delete encrypted profile: "+err.Error())
		}
		out.out("deleted encrypted profile " + request.ProfileName)
		return out.done(Success)
	case "export":
		if request.ProfileName == "" || request.OutputPath == "" || request.PassphraseFile == "" {
			return out.failWith(ConfigurationError, "usage: dmtx profile export NAME [OUTPUT] --passphrase-file PATH (default: NAME.dmtx-profile.json)")
		}
		passphrase, passphraseInfo, err := readProfilePassphraseWithInfo(request.PassphraseFile)
		if err != nil {
			return out.failWith(FileError, "read profile export passphrase file")
		}
		defer clearProfileBytes(passphrase)
		store, err := open()
		if err != nil {
			return out.failWith(FileError, "open encrypted profile store: "+err.Error())
		}
		defer store.Close()
		data, err := store.Load(request.ProfileName)
		if err != nil {
			return out.failWith(FileError, "load encrypted profile: "+err.Error())
		}
		if _, err := config.Parse(data); err != nil {
			return out.failWith(ConfigurationError, "profile configuration: "+err.Error())
		}
		exported, err := profiles.SealPortable(data, passphrase)
		if err != nil {
			return out.failWith(FileError, "encrypt portable profile")
		}
		// Do this immediately before the atomic replacement. In particular, an
		// output path which is a hard link to the passphrase must never replace
		// the passphrase directory entry.
		if err := rejectProfileFileAlias(request.OutputPath, passphraseInfo); err != nil {
			return out.failWith(FileError, "write portable encrypted profile")
		}
		if err := writeProtectedProfileExport(request.OutputPath, exported); err != nil {
			return out.failWith(FileError, "write portable encrypted profile: "+err.Error())
		}
		out.out("exported portable encrypted profile " + request.ProfileName + " to " + request.OutputPath)
		return out.done(Success)
	case "import":
		if request.ProfileName == "" || request.OutputPath == "" || request.PassphraseFile == "" {
			return out.failWith(ConfigurationError, "usage: dmtx profile import NAME INPUT --passphrase-file PATH")
		}
		passphrase, passphraseInfo, err := readProfilePassphraseWithInfo(request.PassphraseFile)
		if err != nil {
			return out.failWith(FileError, "read profile import passphrase file")
		}
		defer clearProfileBytes(passphrase)
		envelope, envelopeInfo, err := readRegularProfileFileWithInfo(request.OutputPath)
		if err != nil {
			return out.failWith(FileError, "read portable encrypted profile")
		}
		if os.SameFile(passphraseInfo, envelopeInfo) {
			return out.failWith(FileError, "read portable encrypted profile")
		}
		data, err := profiles.OpenPortable(envelope, passphrase)
		if err != nil {
			return out.failWith(ConfigurationError, "import portable encrypted profile: "+err.Error())
		}
		defer clearProfileBytes(data)
		if _, err := config.Parse(data); err != nil {
			return out.failWith(ConfigurationError, "imported profile configuration is invalid")
		}
		store, err := open()
		if err != nil {
			return out.failWith(FileError, "open encrypted profile store: "+err.Error())
		}
		defer store.Close()
		if err := store.Save(request.ProfileName, data); err != nil {
			return out.failWith(FileError, "save imported encrypted profile: "+err.Error())
		}
		out.out("imported portable encrypted profile " + request.ProfileName)
		return out.done(Success)
	default:
		return out.failWith(ConfigurationError, "usage: dmtx profile save NAME --config migration.yaml | list | delete NAME | export NAME [OUTPUT] --passphrase-file PATH (default: NAME.dmtx-profile.json) | import NAME INPUT --passphrase-file PATH")
	}
}

// writeProtectedProfileExport writes a portable encrypted profile with
// owner-only permissions. It refuses dangerous destinations before writing.
func writeProtectedProfileExport(path string, data []byte) error {
	return secrets.WriteProtectedFile(path, data)
}

func readProfilePassphrase(path string) ([]byte, error) {
	data, _, err := readProfilePassphraseWithInfo(path)
	return data, err
}

func readProfilePassphraseWithInfo(path string) ([]byte, os.FileInfo, error) {
	data, info, err := readBoundedRegularFile(path, maxProfilePassphraseFile, true)
	if err != nil || len(data) == 0 {
		return nil, nil, errors.New("passphrase file must be a private regular file")
	}
	// A final newline is conventional for a one-line secret file and does not
	// become part of the passphrase. Internal newlines are retained verbatim.
	data = bytes.TrimSuffix(data, []byte("\n"))
	data = bytes.TrimSuffix(data, []byte("\r"))
	if len(data) == 0 {
		return nil, nil, errors.New("passphrase file is empty")
	}
	return data, info, nil
}

func readRegularProfileFile(path string) ([]byte, error) {
	data, _, err := readRegularProfileFileWithInfo(path)
	return data, err
}

func readRegularProfileFileWithInfo(path string) ([]byte, os.FileInfo, error) {
	data, info, err := readBoundedRegularFile(path, maxPortableProfileFile, false)
	if err != nil {
		return nil, nil, errors.New("portable profile must be a regular file")
	}
	return data, info, nil
}

// rejectProfileFileAlias refuses a pre-existing output which names the opened
// passphrase file, including through a hard link. Lstat deliberately does not
// follow symlinks; WriteProtectedFile remains responsible for rejecting every
// non-regular destination before its atomic replacement.
func rejectProfileFileAlias(path string, protected os.FileInfo) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errors.New("inspect portable profile destination")
	}
	if os.SameFile(protected, info) {
		return errors.New("portable profile destination aliases passphrase file")
	}
	return nil
}

// readBoundedRegularFile rejects links and special files before reading, then
// limits the actual stream as a second guard against a file growing after its
// metadata check. Windows ACLs are not representable by POSIX mode bits, so
// this deliberately mirrors secrets.ValidatePermissions and omits that
// unverifiable check there.
func readBoundedRegularFile(path string, maximum int64, private bool) ([]byte, os.FileInfo, error) {
	return readBoundedRegularFileWithOpen(path, maximum, private, os.Open)
}

func readBoundedRegularFileWithOpen(
	path string,
	maximum int64,
	private bool,
	open func(string) (*os.File, error),
) ([]byte, os.FileInfo, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil || !pathInfo.Mode().IsRegular() || pathInfo.Size() > maximum {
		return nil, nil, errors.New("unsafe file")
	}
	if private && runtime.GOOS != "windows" && pathInfo.Mode().Perm()&0o077 != 0 {
		return nil, nil, errors.New("unsafe file")
	}
	file, err := open(path)
	if err != nil {
		return nil, nil, errors.New("unsafe file")
	}
	defer file.Close()
	// The opened descriptor must still identify the exact object inspected by
	// Lstat. This rejects a path exchanged for a regular file or symlink target
	// between those operations; all subsequent validation and reading use the
	// descriptor, which cannot be redirected by another path replacement.
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(pathInfo, openedInfo) || !openedInfo.Mode().IsRegular() || openedInfo.Size() > maximum {
		return nil, nil, errors.New("unsafe file")
	}
	if private && runtime.GOOS != "windows" && openedInfo.Mode().Perm()&0o077 != 0 {
		return nil, nil, errors.New("unsafe file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, nil, errors.New("unsafe file")
	}
	return data, openedInfo, nil
}

func clearProfileBytes(data []byte) {
	for index := range data {
		data[index] = 0
	}
}
