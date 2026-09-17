//go:build linux && cgo

package displaymode

/*
#include <errno.h>
#include <fcntl.h>
#include <linux/uinput.h>
#include <linux/kd.h>
#include <linux/vt.h>
#include <string.h>
#include <sys/ioctl.h>
#include <unistd.h>

static int console_fds[2] = {-1, -1};
static int console_modes[2] = {KD_TEXT, KD_TEXT};
static int console_saved[2];
static int previous_vt;
static int keyboard_fd = -1;
static int keyboard_created;

// A complete key press lets Main process both make and break events.
static int console_key(int fd, int key) {
    struct input_event events[2];
    memset(events, 0, sizeof(events));
    events[0].type = EV_KEY;
    events[0].code = key;
    events[0].value = 1;
    events[1].type = EV_SYN;
    if (write(fd, events, sizeof(events)) != sizeof(events)) return errno ? errno : EIO;
    usleep(50000);
    events[0].value = 0;
    if (write(fd, events, sizeof(events)) != sizeof(events)) return errno ? errno : EIO;
    usleep(50000);
    return 0;
}

// Prepare input before the core load so Main can discover it while switching
// modes. Clear both console buffers before the new core can display either one.
static int console_prepare(void) {
    int fd = -1;
    struct vt_stat state;
    struct uinput_user_dev dev;
    const char *paths[] = {"/dev/tty1", "/dev/tty2"};
    const char clear[] = "\033[0m\033[40m\033[2J\033[3J\033[H";
    for (int i = 0; i < 2; i++) {
        console_fds[i] = open(paths[i], O_RDWR | O_CLOEXEC);
        if (console_fds[i] < 0) return errno;
        if (ioctl(console_fds[i], KDGETMODE, &console_modes[i]) < 0) return errno;
        console_saved[i] = 1;
        if (write(console_fds[i], clear, sizeof(clear) - 1) != sizeof(clear) - 1) return errno ? errno : EIO;
        // Scripts may leave VT2 in graphics mode, which blocks Main's VT switch.
        if (ioctl(console_fds[i], KDSETMODE, KD_TEXT) < 0) return errno;
    }
    if (ioctl(console_fds[0], VT_GETSTATE, &state) < 0) return errno;
    previous_vt = state.v_active;
    fd = open("/dev/uinput", O_WRONLY | O_CLOEXEC);
    if (fd < 0) return errno;
    keyboard_fd = fd;
    memset(&dev, 0, sizeof(dev));
    strcpy(dev.name, "MiSTerFin display setup");
    dev.id.bustype = BUS_VIRTUAL;
    dev.id.vendor = 0x1;
    dev.id.product = 0x1;
    if (ioctl(fd, UI_SET_EVBIT, EV_KEY) < 0 || ioctl(fd, UI_SET_EVBIT, EV_SYN) < 0 ||
        ioctl(fd, UI_SET_KEYBIT, KEY_F9) < 0 ||
        ioctl(fd, UI_SET_KEYBIT, KEY_A) < 0 || ioctl(fd, UI_SET_KEYBIT, KEY_Q) < 0 ||
        write(fd, &dev, sizeof(dev)) != sizeof(dev) || ioctl(fd, UI_DEV_CREATE) < 0) {
        return errno ? errno : EIO;
    }
    keyboard_created = 1;
    return 0;
}

// Remove the temporary keyboard before the application's input reader starts.
// Restoration also calls this after a partial preparation or failed handoff.
static int console_close_keyboard(void) {
    int error = 0;
    if (keyboard_created && ioctl(keyboard_fd, UI_DEV_DESTROY) < 0) error = errno;
    if (keyboard_fd >= 0 && close(keyboard_fd) < 0 && !error) error = errno;
    keyboard_fd = -1;
    keyboard_created = 0;
    return error;
}

// A freshly loaded menu core starts with console output disabled. F9 enables
// it directly, without F12 exposing the OSD first. Retry only until Main selects
// VT1. Keeping the keyboard alive and checking the VT handles delayed discovery.
static int console_enable(void) {
    struct vt_stat state;
    if (keyboard_fd < 0 || !keyboard_created) return ENODEV;
    // VT2 makes the acknowledgment observable even for SSH launches.
    if (ioctl(console_fds[0], VT_ACTIVATE, 2) < 0) return errno;
    for (int attempt = 0; attempt < 6; attempt++) {
        int error = console_key(keyboard_fd, KEY_F9);
        if (error) return error;
        // Main selects VT1 before enabling scanout and hiding its OSD. Allow
        // those operations to finish before the supervisor can stop Main.
        for (int poll = 0; poll < 5; poll++) {
            usleep(100000);
            if (ioctl(console_fds[0], VT_GETSTATE, &state) < 0) return errno;
            if (state.v_active == 1) {
                usleep(100000);
                return console_close_keyboard();
            }
        }
    }
    return ETIMEDOUT;
}

// Restore the original console even if the client was killed before its
// framebuffer destructor ran. The supervisor retains this descriptor.
static int console_restore(void) {
    const char clear[] = "\033[0m\033[40m\033[2J\033[3J\033[H";
    int error = console_close_keyboard();
    // Release the client's graphics mode even if the child crashed. Restore
    // the original active VT before restoring any saved graphics modes.
    for (int i = 0; i < 2; i++) {
        if (!console_saved[i]) continue;
        if (write(console_fds[i], clear, sizeof(clear) - 1) != sizeof(clear) - 1 && !error) error = errno ? errno : EIO;
        if (ioctl(console_fds[i], KDSETMODE, KD_TEXT) < 0 && !error) error = errno;
    }
    if (previous_vt && ioctl(console_fds[0], VT_ACTIVATE, previous_vt) < 0 && !error) error = errno;
    for (int i = 0; i < 2; i++) {
        if (console_saved[i] && ioctl(console_fds[i], KDSETMODE, console_modes[i]) < 0 && !error) error = errno;
        if (console_fds[i] >= 0 && close(console_fds[i]) < 0 && !error) error = errno;
        console_fds[i] = -1;
        console_saved[i] = 0;
    }
    previous_vt = 0;
    return error;
}

*/
import "C"
import (
	"fmt"
	"syscall"
)

// prepareConsole clears terminal text and registers the temporary input device
// before the core switch. The caller must restoreConsole even after an error.
func prepareConsole() error {
	if code := C.console_prepare(); code != 0 {
		return fmt.Errorf("prepare MiSTer framebuffer: %w", syscall.Errno(code))
	}
	return nil
}

// enableConsole asks the ready menu core to display the Linux console.
func enableConsole() error {
	if code := C.console_enable(); code != 0 {
		return fmt.Errorf("enable MiSTer framebuffer: %w", syscall.Errno(code))
	}
	return nil
}

// restoreConsole releases the supervisor's console lease after child cleanup.
func restoreConsole() error {
	if code := C.console_restore(); code != 0 {
		return fmt.Errorf("restore MiSTer console: %w", syscall.Errno(code))
	}
	return nil
}
