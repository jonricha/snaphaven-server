//go:build darwin

package main

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc
#cgo darwin LDFLAGS: -framework Cocoa -framework ServiceManagement

#import <Cocoa/Cocoa.h>
#import <ServiceManagement/ServiceManagement.h>
#include <stdlib.h>
#include <string.h>

static char* sh_strdup_ns(NSString *s) {
	if (s == nil) return NULL;
	const char *utf8 = [s UTF8String];
	if (utf8 == NULL) return NULL;
	return strdup(utf8);
}

static void sh_activate_app(void) {
	if (@available(macOS 14.0, *)) {
		[NSApp activate];
	} else {
		[[NSRunningApplication currentApplication] activateWithOptions:NSApplicationActivateAllWindows];
	}
}

static int sh_pick_folder(const char *initialDir, char **outPath, char **outBookmarkB64, char **outErr) {
	if (NSApp == nil || ![NSApp isRunning]) {
		*outErr = strdup("macOS GUI event loop is not running");
		return -1;
	}

	__block int result = 0; // 0 = cancelled, 1 = ok, -1 = error
	__block char *pathCStr = NULL;
	__block char *bmCStr = NULL;
	__block char *errCStr = NULL;

	NSString *initPath = (initialDir != NULL && initialDir[0] != '\0')
		? [NSString stringWithUTF8String:initialDir]
		: nil;

	void (^runPanel)(void) = ^{
		@autoreleasepool {
			NSApplicationActivationPolicy prevPolicy = [NSApp activationPolicy];
			if (prevPolicy != NSApplicationActivationPolicyRegular) {
				[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
			}
			sh_activate_app();

			NSOpenPanel *panel = [NSOpenPanel openPanel];
			panel.canChooseFiles = NO;
			panel.canChooseDirectories = YES;
			panel.canCreateDirectories = YES;
			panel.allowsMultipleSelection = NO;
			panel.prompt = @"Select Sync Folder";
			panel.message = @"Choose the folder where SnapHaven Server will store backed-up photos and videos.";
			[panel setLevel:NSModalPanelWindowLevel];
			if (initPath != nil) {
				panel.directoryURL = [NSURL fileURLWithPath:initPath];
			}

			NSModalResponse resp = [panel runModal];
			[panel close];
			if (prevPolicy != NSApplicationActivationPolicyRegular) {
				[NSApp setActivationPolicy:prevPolicy];
			}
			if (resp == NSModalResponseOK && panel.URL != nil) {
				NSURL *url = panel.URL;
				[url startAccessingSecurityScopedResource];
				NSError *bmErr = nil;
				NSData *bookmark = [url bookmarkDataWithOptions:NSURLBookmarkCreationWithSecurityScope
				                 includingResourceValuesForKeys:nil
				                                  relativeToURL:nil
				                                          error:&bmErr];
				if (bookmark == nil && bmErr != nil) {
					// Non-fatal if non-sandboxed or bookmark creation unsupported; still return path
					errCStr = sh_strdup_ns([bmErr localizedDescription]);
				} else if (bookmark != nil) {
					NSString *b64 = [bookmark base64EncodedStringWithOptions:0];
					bmCStr = sh_strdup_ns(b64);
				}
				pathCStr = sh_strdup_ns([url path]);
				result = 1;
			} else {
				result = 0;
			}
		}
	};

	if ([NSThread isMainThread]) {
		runPanel();
	} else {
		dispatch_sync(dispatch_get_main_queue(), runPanel);
	}

	*outPath = pathCStr;
	*outBookmarkB64 = bmCStr;
	*outErr = errCStr;
	return result;
}

static int sh_restore_bookmark(const char *b64, char **outPath, char **outErr) {
	@autoreleasepool {
		if (b64 == NULL || b64[0] == '\0') {
			return 0;
		}
		NSString *b64Str = [NSString stringWithUTF8String:b64];
		NSData *data = [[NSData alloc] initWithBase64EncodedString:b64Str options:0];
		if (data == nil) {
			*outErr = strdup("invalid base64 security-scoped bookmark");
			return -1;
		}
		BOOL isStale = NO;
		NSError *err = nil;
		NSURL *url = [NSURL URLByResolvingBookmarkData:data
		                                       options:NSURLBookmarkResolutionWithSecurityScope
		                                 relativeToURL:nil
		                           bookmarkDataIsStale:&isStale
		                                         error:&err];
		if (url == nil) {
			*outErr = sh_strdup_ns(err != nil ? [err localizedDescription] : @"failed to resolve bookmark");
			return -1;
		}
		[url startAccessingSecurityScopedResource];
		*outPath = sh_strdup_ns([url path]);
		return 1;
	}
}

static int sh_open_url(const char *target, int isFilePath, char **outErr) {
	@autoreleasepool {
		if (target == NULL) {
			*outErr = strdup("empty target");
			return -1;
		}
		NSString *str = [NSString stringWithUTF8String:target];
		NSURL *url = isFilePath ? [NSURL fileURLWithPath:str] : [NSURL URLWithString:str];
		if (url == nil) {
			*outErr = strdup("invalid URL or path");
			return -1;
		}
		BOOL ok = [[NSWorkspace sharedWorkspace] openURL:url];
		if (!ok) {
			*outErr = strdup("NSWorkspace openURL returned false");
			return -1;
		}
		return 0;
	}
}

static int sh_set_autostart(int enabled, char **outErr) {
	@autoreleasepool {
		if (@available(macOS 13.0, *)) {
			SMAppService *svc = [SMAppService mainAppService];
			if (!enabled && (svc.status == SMAppServiceStatusNotRegistered || svc.status == SMAppServiceStatusNotFound)) {
				return 0;
			}
			NSError *err = nil;
			BOOL ok = enabled ? [svc registerAndReturnError:&err] : [svc unregisterAndReturnError:&err];
			if (!ok && err != nil) {
				*outErr = sh_strdup_ns([err localizedDescription]);
				return -1;
			}
			return 0;
		} else {
			*outErr = strdup("SMAppService requires macOS 13.0 or later");
			return -1;
		}
	}
}

static void sh_show_about(const char *title, const char *msg) {
	if (NSApp == nil || ![NSApp isRunning]) {
		return;
	}
	NSString *nsTitle = title ? [NSString stringWithUTF8String:title] : @"SnapHaven Server";
	NSString *nsMsg = msg ? [NSString stringWithUTF8String:msg] : @"";

	dispatch_async(dispatch_get_main_queue(), ^{
		@autoreleasepool {
			sh_activate_app();
			NSAlert *alert = [[NSAlert alloc] init];
			alert.messageText = nsTitle;
			alert.informativeText = nsMsg;
			alert.alertStyle = NSAlertStyleInformational;
			[alert addButtonWithTitle:@"OK"];
			[alert.window setLevel:NSModalPanelWindowLevel];
			[alert runModal];
		}
	});
}
*/
import "C"

import (
	"errors"
	"log"
	"sync/atomic"
	"unsafe"
)

var pickingFolder atomic.Bool

func PickFolderPlatform(initialDir string) (selectedPath string, bookmarkBase64 string, cancelled bool, err error) {
	if !pickingFolder.CompareAndSwap(false, true) {
		return "", "", true, nil
	}
	defer pickingFolder.Store(false)

	cInit := C.CString(initialDir)
	defer C.free(unsafe.Pointer(cInit))

	var cPath, cBM, cErr *C.char
	rc := C.sh_pick_folder(cInit, &cPath, &cBM, &cErr)
	if cPath != nil {
		selectedPath = C.GoString(cPath)
		C.free(unsafe.Pointer(cPath))
	}
	if cBM != nil {
		bookmarkBase64 = C.GoString(cBM)
		C.free(unsafe.Pointer(cBM))
	}
	if cErr != nil {
		errMsg := C.GoString(cErr)
		C.free(unsafe.Pointer(cErr))
		if rc < 0 {
			return "", "", false, errors.New(errMsg)
		}
		log.Printf("Notice: Security-scoped bookmark warning: %s", errMsg)
	}
	if rc == 0 {
		return "", "", true, nil
	}
	return selectedPath, bookmarkBase64, false, nil
}

func RestoreSecurityScopedBookmarkPlatform(bookmarkBase64 string) (string, error) {
	if bookmarkBase64 == "" {
		return "", nil
	}
	cBM := C.CString(bookmarkBase64)
	defer C.free(unsafe.Pointer(cBM))

	var cPath, cErr *C.char
	rc := C.sh_restore_bookmark(cBM, &cPath, &cErr)
	var resolved string
	if cPath != nil {
		resolved = C.GoString(cPath)
		C.free(unsafe.Pointer(cPath))
	}
	if cErr != nil {
		errMsg := C.GoString(cErr)
		C.free(unsafe.Pointer(cErr))
		if rc < 0 {
			return "", errors.New(errMsg)
		}
	}
	return resolved, nil
}

func OpenURLPlatform(rawURL string) error {
	cStr := C.CString(rawURL)
	defer C.free(unsafe.Pointer(cStr))
	var cErr *C.char
	if C.sh_open_url(cStr, 0, &cErr) != 0 {
		if cErr != nil {
			msg := C.GoString(cErr)
			C.free(unsafe.Pointer(cErr))
			return errors.New(msg)
		}
		return errors.New("failed to open URL")
	}
	return nil
}

func OpenFolderPlatform(absPath string) error {
	cStr := C.CString(absPath)
	defer C.free(unsafe.Pointer(cStr))
	var cErr *C.char
	if C.sh_open_url(cStr, 1, &cErr) != 0 {
		if cErr != nil {
			msg := C.GoString(cErr)
			C.free(unsafe.Pointer(cErr))
			return errors.New(msg)
		}
		return errors.New("failed to open folder")
	}
	return nil
}

func SetAutoStartDarwinMAS(enabled bool) error {
	var flag C.int
	if enabled {
		flag = 1
	}
	var cErr *C.char
	if C.sh_set_autostart(flag, &cErr) != 0 {
		if cErr != nil {
			msg := C.GoString(cErr)
			C.free(unsafe.Pointer(cErr))
			return errors.New(msg)
		}
		return errors.New("failed to configure SMAppService login item")
	}
	return nil
}

func ShowAboutDialogPlatform(title, message string) {
	cTitle := C.CString(title)
	cMsg := C.CString(message)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cMsg))
	C.sh_show_about(cTitle, cMsg)
}
