package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/profiles"
	"github.com/johndauphine/dmtx/internal/secrets"
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
		if request.ProfileName == "" || request.OutputPath == "" {
			return out.failWith(ConfigurationError, "usage: dmtx profile export NAME [OUTPUT]")
		}
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
		if err := writeProtectedProfileExport(request.OutputPath, data); err != nil {
			return out.failWith(FileError, "export encrypted profile: "+err.Error())
		}
		out.out("exported encrypted profile " + request.ProfileName + " to " + request.OutputPath)
		return out.done(Success)
	default:
		return out.failWith(ConfigurationError, "usage: dmtx profile save NAME --config migration.yaml | list | delete NAME")
	}
}

// writeProtectedProfileExport writes the deliberately requested plaintext
// profile with owner-only permissions. The explicit export is the boundary at
// which sealed profile bytes are allowed back onto disk; never loosen the file
// simply because a prior file at the path had looser permissions.
func writeProtectedProfileExport(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
