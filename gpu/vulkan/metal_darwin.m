//go:build darwin

#import <Cocoa/Cocoa.h>
#import <QuartzCore/CAMetalLayer.h>

// pixSetupMetalLayer ensures the window's content view is backed by a
// CAMetalLayer and returns it, so a VkSurfaceKHR can be created from it.
void* pixSetupMetalLayer(void* nsWindowPtr) {
    NSWindow* window = (NSWindow*)nsWindowPtr;
    NSView* view = [window contentView];
    if ([view.layer isKindOfClass:[CAMetalLayer class]]) {
        return (void*)view.layer;
    }
    CAMetalLayer* layer = [CAMetalLayer layer];
    [view setWantsLayer:YES];
    [view setLayer:layer];
    return (void*)layer;
}
