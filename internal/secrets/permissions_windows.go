//go:build windows

package secrets

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func currentWindowsUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read current Windows account: %w", err)
	}
	return user.User.Sid, nil
}

// restrictSecretPath replaces inherited access with one protected DACL entry
// granting the current account full control. It is the Windows equivalent of
// an owner-only 0600 file or 0700 directory.
func restrictSecretPath(path string) error {
	sid, err := currentWindowsUserSID()
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}}, nil)
	if err != nil {
		return fmt.Errorf("build owner-only Windows ACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return fmt.Errorf("apply owner-only Windows ACL: %w", err)
	}
	return nil
}

func validatePlatformPermissions(path string, insecure error) error {
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("read Windows ACL for %s: %w", path, err)
	}
	if descriptor == nil {
		return fmt.Errorf("%w: %s has no Windows security descriptor", insecure, path)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return fmt.Errorf("read Windows ACL control for %s: %w", path, err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("%w: %s inherits Windows access; restrict its ACL to the current account", insecure, path)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return fmt.Errorf("%w: %s has no restrictive Windows DACL", insecure, path)
	}
	current, err := currentWindowsUserSID()
	if err != nil {
		return err
	}
	allowed := 0
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			return fmt.Errorf("read Windows ACL entry for %s: %w", path, err)
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			entrySID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if !current.Equals(entrySID) {
				return fmt.Errorf("%w: %s grants Windows access to %s; restrict its ACL to the current account", insecure, path, entrySID.String())
			}
			allowed++
		default:
			return fmt.Errorf("%w: %s has an unsupported Windows ACL entry; restrict its ACL to the current account", insecure, path)
		}
	}
	if allowed != 1 {
		return fmt.Errorf("%w: %s does not have exactly one current-account Windows access entry", insecure, path)
	}
	return nil
}
