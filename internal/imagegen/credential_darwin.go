//go:build darwin && cgo

package imagegen

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdlib.h>

static CFMutableDictionaryRef imageKeyQuery(const char *account) {
    CFMutableDictionaryRef query = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFStringRef scope = CFStringCreateWithCString(NULL, account, kCFStringEncodingUTF8);
    CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
    CFDictionarySetValue(query, kSecAttrService, CFSTR("com.yijie.ai.seedream"));
    CFDictionarySetValue(query, kSecAttrAccount, scope);
    CFRelease(scope);
    return query;
}
static OSStatus readImageKey(const char *account, char *buf, size_t capacity, size_t *size) {
    CFMutableDictionaryRef query = imageKeyQuery(account);
    CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
    CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitOne);
    CFTypeRef result = NULL;
    OSStatus status = SecItemCopyMatching(query, &result);
    CFRelease(query);
    if (status != errSecSuccess) return status;
    CFIndex length = CFDataGetLength((CFDataRef)result);
    if (length < 1 || length > (CFIndex)capacity) status = errSecDecode;
    else {
        CFDataGetBytes((CFDataRef)result, CFRangeMake(0, length), (UInt8*)buf);
        *size = (size_t)length;
    }
    CFRelease(result);
    return status;
}
static OSStatus storeImageKey(const char *account, const void *key, size_t size) {
    CFMutableDictionaryRef query = imageKeyQuery(account);
    CFDataRef data = CFDataCreate(NULL, key, size);
    CFDictionarySetValue(query, kSecValueData, data);
    CFDictionarySetValue(query, kSecAttrAccessible, kSecAttrAccessibleWhenUnlockedThisDeviceOnly);
    OSStatus status = SecItemAdd(query, NULL);
    CFRelease(data);
    CFRelease(query);
    return status;
}
*/
import "C"

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os/exec"
	"strings"
	"unsafe"
)

// ArkCredential uses a separate Keychain account per Host home, so an isolated
// build cannot read the daily application's image key. No lookup at startup.
func ArkCredential(home string) func(context.Context) (string, error) {
	digest := sha256.Sum256([]byte(home))
	account := hex.EncodeToString(digest[:])
	gate := make(chan struct{}, 1)
	return func(ctx context.Context) (string, error) {
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-ctx.Done():
			return "", &ProviderError{Code: ErrorRequestCanceled}
		}
		if ctx.Err() != nil {
			return "", &ProviderError{Code: ErrorRequestCanceled}
		}
		scope := C.CString(account)
		defer C.free(unsafe.Pointer(scope))
		var buffer [16 << 10]byte
		defer clear(buffer[:])
		read := func() (string, C.OSStatus) {
			var size C.size_t
			status := C.readImageKey(scope, (*C.char)(unsafe.Pointer(&buffer[0])), C.size_t(len(buffer)), &size)
			return string(buffer[:int(size)]), status
		}
		key, status := read()
		if status == C.errSecSuccess {
			return key, nil
		}
		if status != C.errSecItemNotFound {
			return "", &ProviderError{Code: ErrorAuthentication}
		}
		// No shell, secret argument or inherited model environment. System Events
		// handles dismissal normally (including its own timeout).
		const prompt = `activate
set dialogResult to display dialog "为易界内置生图配置火山方舟 API Key。凭据将保存到本机钥匙串。" default answer "" with hidden answer buttons {"取消", "保存"} default button "保存" cancel button "取消" with title "易界 · 图片生成" giving up after 90
if gave up of dialogResult then return ""
return text returned of dialogResult`
		cmd := exec.Command("/usr/bin/osascript", "-e", prompt)
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		raw, err := cmd.Output()
		defer clear(raw)
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && strings.Contains(string(exit.Stderr), "(-128)") {
				return "", &ProviderError{Code: ErrorRequestCanceled}
			}
			return "", &ProviderError{Code: ErrorAuthentication}
		}
		key = strings.TrimSpace(string(raw))
		if ctx.Err() != nil || key == "" {
			return "", &ProviderError{Code: ErrorRequestCanceled}
		}
		if len(key) > len(buffer) || strings.ContainsAny(key, " \t\r\n\x00") {
			return "", &ProviderError{Code: ErrorInvalidRequest}
		}
		copy(buffer[:], key)
		status = C.storeImageKey(scope, unsafe.Pointer(&buffer[0]), C.size_t(len(key)))
		if status == C.errSecDuplicateItem {
			key, status = read()
		}
		if status != C.errSecSuccess {
			return "", &ProviderError{Code: ErrorAuthentication}
		}
		return key, nil
	}
}
