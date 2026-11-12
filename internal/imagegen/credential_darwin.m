//go:build darwin && cgo

#import <Foundation/Foundation.h>
#import <LocalAuthentication/LocalAuthentication.h>
#import <Security/Security.h>

// Only an explicitly attended operator action may request system authorization.
// Ordinary generation forbids UI; no path creates/updates/unlocks a Keychain item.
OSStatus readImageKey(const char *account, int allowInteraction, char *buffer, size_t capacity, size_t *size) {
    @autoreleasepool {
        // The file-based macOS Keychain shim can ignore LAContext. Disable
        // legacy interaction for normal generation too; this does not unlock a
        // Keychain or change any item's ACL/accessibility. Protected items fail.
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
        OSStatus interaction = SecKeychainSetUserInteractionAllowed(allowInteraction != 0);
#pragma clang diagnostic pop
        if (interaction != errSecSuccess) return interaction;
        LAContext *authentication = [[LAContext alloc] init];
        authentication.interactionNotAllowed = allowInteraction ? NO : YES;
        NSDictionary *query = @{
            (id)kSecClass: (id)kSecClassGenericPassword,
            (id)kSecAttrService: @"com.yijie.ai.seedream",
            (id)kSecAttrAccount: [NSString stringWithUTF8String:account],
            (id)kSecReturnData: @YES,
            (id)kSecMatchLimit: (id)kSecMatchLimitOne,
            (id)kSecUseAuthenticationContext: authentication
        };
        CFTypeRef result = NULL;
        OSStatus status = SecItemCopyMatching((CFDictionaryRef)query, &result);
        [authentication release];
        if (status != errSecSuccess) return status;
        if (result == NULL || CFGetTypeID(result) != CFDataGetTypeID()) {
            if (result != NULL) CFRelease(result);
            return errSecDecode;
        }
        CFIndex length = CFDataGetLength((CFDataRef)result);
        if (length < 1 || length > (CFIndex)capacity) {
            status = errSecDecode;
        } else {
            CFDataGetBytes((CFDataRef)result, CFRangeMake(0, length), (UInt8 *)buffer);
            *size = (size_t)length;
        }
        CFRelease(result);
        return status;
    }
}
