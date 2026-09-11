//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

type testAccount struct {
	User     string `json:"user"`
	Password string `json:"password"`
	SID      string `json:"sid"`
}
type aclRequest struct {
	Owner       testAccount `json:"owner"`
	Reader      testAccount `json:"reader"`
	ControlPath string      `json:"control_path"`
}

var aclAdvapi = windows.NewLazySystemDLL("advapi32.dll")
var aclLogon = aclAdvapi.NewProc("LogonUserW")
var aclImpersonate = aclAdvapi.NewProc("ImpersonateLoggedOnUser")
var aclRevert = aclAdvapi.NewProc("RevertToSelf")

// All filesystem work remains on the impersonated OS thread. No component or
// goroutine is launched under these temporary credentials.
func asOrdinaryAccount(account testAccount, fn func() error) error {
	user, err := windows.UTF16PtrFromString(account.User)
	if err != nil {
		return errors.New("invalid account name")
	}
	domain, _ := windows.UTF16PtrFromString(".")
	password, err := windows.UTF16PtrFromString(account.Password)
	if err != nil {
		return errors.New("invalid account credential")
	}
	var token windows.Token
	ok, _, callErr := aclLogon.Call(uintptr(unsafe.Pointer(user)), uintptr(unsafe.Pointer(domain)), uintptr(unsafe.Pointer(password)), 2, 0, uintptr(unsafe.Pointer(&token)))
	if ok == 0 {
		return fmt.Errorf("test account logon failed: %w", callErr)
	}
	defer token.Close()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ok, _, callErr = aclImpersonate.Call(uintptr(token))
	if ok == 0 {
		return fmt.Errorf("test impersonation failed: %w", callErr)
	}
	defer func() {
		reverted, _, _ := aclRevert.Call()
		if reverted == 0 {
			fmt.Fprintln(os.Stderr, "cannot revert test impersonation")
			os.Exit(1)
		}
	}()
	var effective windows.Token
	if err = windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY, true, &effective); err != nil {
		return err
	}
	defer effective.Close()
	identity, err := effective.GetTokenUser()
	if err != nil {
		return err
	}
	if identity.User.Sid.String() != account.SID {
		return errors.New("test effective account identity mismatch")
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	member, err := effective.IsMember(admins)
	if err != nil {
		return err
	}
	if member {
		return errors.New("ACL test account must not be an administrator")
	}
	return fn()
}

func checkAccountACL(paths standalone.Paths) (result any, err error) {
	var request aclRequest
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 16384))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		return nil, errors.New("invalid ACL acceptance input")
	}
	if request.Owner.User == "" || request.Reader.User == "" || request.Owner.SID == request.Reader.SID {
		return nil, errors.New("two distinct ordinary test accounts are required")
	}
	// This helper deletes only the new isolated instance it creates itself.
	if _, statErr := os.Lstat(paths.InstallDir); !errors.Is(statErr, os.ErrNotExist) {
		return nil, errors.New("ACL probe requires a nonexistent instance directory")
	}
	if filepath.Base(paths.InstallDir) != "instance" || filepath.Dir(request.ControlPath) != filepath.Dir(paths.InstallDir) {
		return nil, errors.New("ACL probe requires an isolated instance and sibling control file")
	}
	created := false
	defer func() {
		if created {
			cleanupErr := asOrdinaryAccount(request.Owner, func() error { return os.RemoveAll(paths.InstallDir) })
			if cleanupErr != nil && err == nil {
				err = errors.New("cannot clean the isolated ACL test instance")
				result = nil
			}
		}
	}()
	var first standalone.InstanceConfig
	var aclSummary map[string]string
	err = asOrdinaryAccount(request.Owner, func() error {
		created = true
		if e := prepareProbePaths(paths); e != nil {
			return e
		}
		var e error
		first, e = standalone.InitializeConfig(paths)
		if e != nil {
			if descriptor, aclErr := windows.GetNamedSecurityInfo(paths.ConfigDir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION); aclErr == nil {
				fmt.Fprintln(os.Stderr, "configuration ACL:", descriptor.String())
			}
			return e
		}
		second, e := standalone.InitializeConfig(paths)
		if e != nil {
			return e
		}
		if first.ConfigSHA256 != second.ConfigSHA256 || first.JWTSecret() != second.JWTSecret() || first.RedisPassword() != second.RedisPassword() || first.ZLMSecret() != second.ZLMSecret() {
			return errors.New("normal initialization rotated instance secrets")
		}
		aclSummary = map[string]string{}
		for _, path := range []string{paths.ConfigDir, first.ConfigPath, first.RedisConfigPath, first.ZLMConfigPath} {
			descriptor, e := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
			if e != nil {
				return e
			}
			owner, _, e := descriptor.Owner()
			if e != nil {
				return e
			}
			if owner.String() != request.Owner.SID {
				return errors.New("configuration owner does not match ordinary account")
			}
			control, _, e := descriptor.Control()
			if e != nil {
				return e
			}
			if control&windows.SE_DACL_PROTECTED == 0 {
				return errors.New("configuration ACL is not protected")
			}
			aclSummary[filepath.Base(path)] = descriptor.String()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	denied := []string{}
	err = asOrdinaryAccount(request.Reader, func() error {
		control, e := os.Open(request.ControlPath)
		if e != nil {
			return errors.New("ordinary reader could not open the positive control file")
		}
		var one [1]byte
		_, e = control.Read(one[:])
		_ = control.Close()
		if e != nil {
			return errors.New("positive control read failed")
		}
		for _, path := range []string{paths.ConfigDir, first.ConfigPath, first.RedisConfigPath, first.ZLMConfigPath} {
			file, e := os.Open(path)
			if e == nil {
				_ = file.Close()
				return errors.New("ordinary reader accessed protected configuration")
			}
			if !errors.Is(e, windows.ERROR_ACCESS_DENIED) {
				return errors.New("configuration read failed for a reason other than access denied")
			}
			denied = append(denied, filepath.Base(path))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"passed": true, "owner_is_standard": true, "reader_is_standard": true, "owner_sid": request.Owner.SID, "reader_sid": request.Reader.SID, "config_sha256": first.ConfigSHA256, "repeat_preserved": true, "positive_control_read": true, "read_denied": denied, "acl": aclSummary}, nil
}
