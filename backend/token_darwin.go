//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

static CFMutableDictionaryRef agent_query(const char *account) {
 CFMutableDictionaryRef q=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFStringRef a=CFStringCreateWithCString(NULL,account,kCFStringEncodingUTF8);
 CFDictionarySetValue(q,kSecClass,kSecClassGenericPassword);
 CFDictionarySetValue(q,kSecAttrService,CFSTR("io.github.fathermrsing.sql-agent.mcp-token"));
 CFDictionarySetValue(q,kSecAttrAccount,a);
 CFRelease(a);
 return q;
}
static OSStatus agent_has(const char *account) {
 CFMutableDictionaryRef q=agent_query(account);
 CFDictionarySetValue(q,kSecMatchLimit,kSecMatchLimitOne);
 CFDictionarySetValue(q,kSecReturnAttributes,kCFBooleanTrue);
 CFDictionarySetValue(q,kSecUseAuthenticationUI,kSecUseAuthenticationUIFail);
 CFTypeRef item=NULL;
 OSStatus s=SecItemCopyMatching(q,&item);
 if(item) CFRelease(item);
 CFRelease(q);return s;
}
static OSStatus agent_get(const char *account, void **bytes, int *length) {
 CFMutableDictionaryRef q=agent_query(account);
 CFDictionarySetValue(q,kSecMatchLimit,kSecMatchLimitOne);
 CFDictionarySetValue(q,kSecReturnData,kCFBooleanTrue);
 CFTypeRef item=NULL;
 OSStatus s=SecItemCopyMatching(q,&item);
 if(s==errSecSuccess && item && CFGetTypeID(item)==CFDataGetTypeID()) {
  CFDataRef data=(CFDataRef)item;CFIndex n=CFDataGetLength(data);
  if(n<=0 || n>8192) s=errSecDecode;
  else { *bytes=malloc(n);if(!*bytes) s=errSecAllocate;else {memcpy(*bytes,CFDataGetBytePtr(data),n);*length=(int)n;} }
 } else if(s==errSecSuccess) s=errSecDecode;
 if(item) CFRelease(item);
 CFRelease(q);return s;
}
static OSStatus agent_set(const char *account,const void *bytes,int length) {
 CFMutableDictionaryRef q=agent_query(account);
 CFDataRef data=CFDataCreate(NULL,bytes,length);
 CFMutableDictionaryRef attrs=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFDictionarySetValue(attrs,kSecValueData,data);
 OSStatus s=SecItemUpdate(q,attrs);
 if(s==errSecItemNotFound) {
  CFDictionarySetValue(q,kSecValueData,data);
  CFDictionarySetValue(q,kSecAttrLabel,CFSTR("DBX SQL Agent MCP Bearer Token"));
  s=SecItemAdd(q,NULL);
 }
 CFRelease(attrs);CFRelease(data);CFRelease(q);return s;
}
static OSStatus agent_delete(const char *account) {
 CFMutableDictionaryRef q=agent_query(account);
 OSStatus s=SecItemDelete(q);CFRelease(q);return s;
}
static void agent_free(void *bytes,int length) {
 if(bytes) {memset(bytes,0,length);free(bytes);}
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type macTokenVault struct{}

func newTokenVault() tokenVault       { return macTokenVault{} }
func (macTokenVault) supported() bool { return true }
func keychainError(s C.OSStatus) error {
	if s == C.errSecSuccess {
		return nil
	}
	// Only a numeric status is exposed: never include credentials or native output.
	return fmt.Errorf("macOS 钥匙串操作失败（OSStatus %d）；请检查系统授权或本次手动填写 Token", int32(s))
}
func (macTokenVault) has(account string) (bool, error) {
	a := C.CString(account)
	defer C.free(unsafe.Pointer(a))
	s := C.agent_has(a)
	if s == C.errSecItemNotFound {
		return false, nil
	}
	return s == C.errSecSuccess, keychainError(s)
}
func (macTokenVault) get(account string) (string, error) {
	a := C.CString(account)
	defer C.free(unsafe.Pointer(a))
	var bytes unsafe.Pointer
	var length C.int
	s := C.agent_get(a, &bytes, &length)
	defer C.agent_free(bytes, length)
	if s == C.errSecItemNotFound {
		return "", nil
	}
	if err := keychainError(s); err != nil {
		return "", err
	}
	return string(C.GoBytes(bytes, length)), nil
}
func (macTokenVault) set(account, token string) error {
	a := C.CString(account)
	defer C.free(unsafe.Pointer(a))
	data := C.CBytes([]byte(token))
	defer C.agent_free(data, C.int(len(token)))
	return keychainError(C.agent_set(a, data, C.int(len(token))))
}
func (macTokenVault) delete(account string) error {
	a := C.CString(account)
	defer C.free(unsafe.Pointer(a))
	s := C.agent_delete(a)
	if s == C.errSecItemNotFound {
		return nil
	}
	return keychainError(s)
}
