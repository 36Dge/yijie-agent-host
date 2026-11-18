//go:build darwin && cgo

package imagegen

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation -framework Foundation -framework LocalAuthentication
#include <Security/Security.h>
#include <stdlib.h>
OSStatus readImageKey(const char *account, int allowInteraction, char *buffer, size_t capacity, size_t *size);
*/
import "C"

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"unsafe"
)

// ArkCredential reads only an operator-provisioned credential in this Host scope.
// It never prompts, creates, updates, copies, unlocks or logs a credential.
func ArkCredential(home string) func(context.Context) (string, error) {
	return arkCredential(home, false)
}

// OperatorArkCredential is only for the explicit attended local CLI action.
// The system owns the authorization dialog; no password or key is collected by
// this program. Generation always uses ArkCredential, which forbids interaction.
func OperatorArkCredential(home string) func(context.Context) (string, error) {
	return arkCredential(home, true)
}

func arkCredential(home string, allowInteraction bool) func(context.Context) (string, error) {
	digest := sha256.Sum256([]byte(home))
	account := hex.EncodeToString(digest[:])
	return func(ctx context.Context) (string, error) {
		if ctx.Err() != nil {
			return "", &ProviderError{Code: ErrorRequestCanceled}
		}
		scope := C.CString(account)
		defer C.free(unsafe.Pointer(scope))
		var buffer [16 << 10]byte
		defer clear(buffer[:])
		var size C.size_t
		interaction := C.int(0)
		if allowInteraction {
			interaction = 1
		}
		status := C.readImageKey(scope, interaction, (*C.char)(unsafe.Pointer(&buffer[0])), C.size_t(len(buffer)), &size)
		if ctx.Err() != nil {
			return "", &ProviderError{Code: ErrorRequestCanceled}
		}
		if status != C.errSecSuccess {
			return "", keychainReadError(int(status))
		}
		return string(buffer[:int(size)]), nil
	}
}

// Keep the local read stage distinct from an HTTP authentication rejection.
// Only fixed reason codes and OSStatus are retained; never Keychain data.
func keychainReadError(status int) *ProviderError {
	reason := "read_failed"
	switch C.OSStatus(status) {
	case C.errSecInteractionNotAllowed, C.errSecInteractionRequired:
		reason = "authorization_required"
	case C.errSecAuthFailed:
		reason = "access_denied"
	case C.errSecItemNotFound:
		reason = "not_found"
	case C.errSecUserCanceled:
		reason = "authorization_cancelled"
	case C.errSecNotAvailable:
		reason = "keychain_unavailable"
	}
	return &ProviderError{Code: ErrorAuthentication, CredentialReason: reason, KeychainStatus: status}
}
