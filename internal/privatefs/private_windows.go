//go:build windows

// Package privatefs enforces owner-only access for application-owned files
// and directories using the native platform permission model.
package privatefs

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func currentUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read current Windows account: %w", err)
	}
	return user.User.Sid, nil
}

// Restrict replaces inherited access with one protected DACL entry granting
// the current account full control.
func Restrict(path string) error {
	sid, err := currentUserSID()
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
		return fmt.Errorf("build current-account-only Windows ACL: %w", err)
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
		return fmt.Errorf("apply current-account-only Windows ACL: %w", err)
	}
	return nil
}

// Validate requires a protected DACL whose only allow entry belongs to the
// current account. Deny entries are harmless and may remain.
func Validate(path string) error {
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("read Windows ACL for %s: %w", path, err)
	}
	if descriptor == nil {
		return fmt.Errorf("%s has no Windows security descriptor", path)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return fmt.Errorf("read Windows ACL control for %s: %w", path, err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("%s inherits Windows access", path)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return fmt.Errorf("%s has no restrictive Windows DACL", path)
	}
	current, err := currentUserSID()
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
				return fmt.Errorf("%s grants Windows access to %s", path, entrySID.String())
			}
			if ace.Mask&windows.GENERIC_ALL == 0 {
				return fmt.Errorf("%s does not grant the current Windows account full control", path)
			}
			allowed++
		default:
			return fmt.Errorf("%s has an unsupported Windows ACL entry", path)
		}
	}
	if allowed != 1 {
		return fmt.Errorf("%s does not have exactly one current-account Windows access entry", path)
	}
	return nil
}
