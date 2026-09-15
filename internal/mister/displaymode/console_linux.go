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

// A complete key press lets Main process both make and break events. F12 first
// leaves its framebuffer disabled, so F9 enables it instead of toggling it off.
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

// Main owns framebuffer activation. Keep the temporary keyboard alive until
// Main acknowledges F9 by selecting VT1. A fixed delay can expire before Main
// discovers the keyboard, leaving its OSD visible over the running client.
static int console_enable(void) {
    int error = 0, created = 0, fd = -1;
    struct vt_stat state;
    struct uinput_user_dev dev;
    const char *paths[] = {"/dev/tty1", "/dev/tty2"};
    const char clear[] = "\033[0m\033[40m\033[2J\033[3J\033[H";
    // Scripts runs on VT2, which may still be in graphics mode. Linux refuses
    // automatic VT switches from graphics mode and Main waits for that switch.
    // Save both modes and release graphics before asking Main to select VT1.
    for (int i = 0; i < 2; i++) {
        console_fds[i] = open(paths[i], O_RDWR | O_CLOEXEC);
        if (console_fds[i] < 0) return errno;
        if (ioctl(console_fds[i], KDGETMODE, &console_modes[i]) < 0) return errno;
        console_saved[i] = 1;
        if (write(console_fds[i], clear, sizeof(clear) - 1) != sizeof(clear) - 1) return errno ? errno : EIO;
        if (ioctl(console_fds[i], KDSETMODE, KD_TEXT) < 0) return errno;
    }
    if (ioctl(console_fds[0], VT_GETSTATE, &state) < 0) return errno;
    previous_vt = state.v_active;
    // VT2 makes Main's transition to VT1 observable even for SSH launches.
    if (ioctl(console_fds[0], VT_ACTIVATE, 2) < 0) return errno;
    fd = open("/dev/uinput", O_WRONLY | O_CLOEXEC);
    if (fd < 0) return errno;
    memset(&dev, 0, sizeof(dev));
    strcpy(dev.name, "MiSTerFin display setup");
    dev.id.bustype = BUS_VIRTUAL;
    dev.id.vendor = 0x1;
    dev.id.product = 0x1;
    if (ioctl(fd, UI_SET_EVBIT, EV_KEY) < 0 || ioctl(fd, UI_SET_EVBIT, EV_SYN) < 0 ||
        ioctl(fd, UI_SET_KEYBIT, KEY_F9) < 0 || ioctl(fd, UI_SET_KEYBIT, KEY_F12) < 0 ||
        ioctl(fd, UI_SET_KEYBIT, KEY_A) < 0 || ioctl(fd, UI_SET_KEYBIT, KEY_Q) < 0 ||
        write(fd, &dev, sizeof(dev)) != sizeof(dev) || ioctl(fd, UI_DEV_CREATE) < 0) {
        error = errno ? errno : EIO;
        goto done;
    }
    created = 1;
    error = ETIMEDOUT;
    for (int attempt = 0; attempt < 6; attempt++) {
        usleep(500000);
        int key_error = console_key(fd, KEY_F12);
        if (!key_error) key_error = console_key(fd, KEY_F9);
        if (key_error) { error = key_error; break; }
        if (ioctl(console_fds[0], VT_GETSTATE, &state) < 0) { error = errno; break; }
        if (state.v_active == 1) {
            // Allow Main to finish hiding its OSD before the supervisor stops it.
            usleep(100000);
            error = 0;
            break;
        }
    }
done:
    if (created) ioctl(fd, UI_DEV_DESTROY);
    close(fd);
    return error;
}

// Restore the original console even if the client was killed before its
// framebuffer destructor ran. The supervisor retains this descriptor.
static int console_restore(void) {
    const char clear[] = "\033[0m\033[40m\033[2J\033[3J\033[H";
    int error = 0;
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

// enableConsole asks the freshly loaded menu core to display the Linux console.
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
