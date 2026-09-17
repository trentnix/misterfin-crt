"""Exercise console handoff with a graphics-mode Scripts console and delayed input."""
import pathlib
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


class InterlacedConsoleTest(unittest.TestCase):
    def test_handoff_acknowledgment_and_crash_restoration(self):
        source = (ROOT / "internal/mister/displaymode/console_linux.go").read_text()
        native = source.split("/*", 1)[1].split("*/", 1)[0]
        includes, implementation = native.split("static int console_fds", 1)
        harness = includes + r'''
#include <assert.h>
#include <stdarg.h>
static int active = 2, modes[2] = {KD_TEXT, KD_GRAPHICS};
static int key_writes, destroyed, acknowledged, never_ready, short_write, create_failure;
static int mock_open(const char *path, int flags, ...) {
    if (!strcmp(path, "/dev/tty1")) return 10;
    if (!strcmp(path, "/dev/tty2")) return 11;
    assert(!strcmp(path, "/dev/uinput"));
    return 9;
}
static int mock_close(int fd) { return 0; }
static int mock_usleep(useconds_t duration) { return 0; }
static int mock_ioctl(int fd, unsigned long request, ...) {
    va_list args;
    va_start(args, request);
    if (request == KDGETMODE) *va_arg(args, int *) = modes[fd - 10];
    else if (request == KDSETMODE) modes[fd - 10] = va_arg(args, int);
    else if (request == VT_GETSTATE) va_arg(args, struct vt_stat *)->v_active = active;
    else if (request == VT_ACTIVATE) {
        int target = va_arg(args, int);
        // A graphics console blocks the switch that Main waits to complete.
        if (modes[active - 1] == KD_TEXT) active = target;
    } else if (request == UI_DEV_CREATE && create_failure) {
        va_end(args);
        errno = EIO;
        return -1;
    } else if (request == UI_SET_KEYBIT) {
        assert(va_arg(args, int) != KEY_F12);
    } else if (request == UI_DEV_DESTROY) destroyed++;
    va_end(args);
    return 0;
}
static ssize_t mock_write(int fd, const void *data, size_t size) {
    if (fd != 9 || size == sizeof(struct uinput_user_dev)) return size;
    if (short_write) { errno = 0; return size - 1; }
    const struct input_event *event = data;
    key_writes++;
    assert(event->code == KEY_F9);
    // Drop the first F9 press, as if Main discovered input late.
    if (key_writes > 2 && !never_ready && event->code == KEY_F9 && event->value) {
        assert(modes[active - 1] == KD_TEXT);
        active = 1;
        acknowledged++;
    }
    return size;
}
#define open mock_open
#define close mock_close
#define ioctl mock_ioctl
#define write mock_write
#define usleep mock_usleep
''' + "static int console_fds" + implementation + r'''
int main(void) {
    assert(console_prepare() == 0);
    // Preparation must not toggle the menu or change the active console.
    assert(active == 2 && key_writes == 0 && keyboard_created);
    assert(console_enable() == 0);
    assert(active == 1 && acknowledged == 1 && key_writes == 4);
    assert(destroyed == 1 && modes[0] == KD_TEXT && modes[1] == KD_TEXT);
    // Simulate a child crash while the visible console is still in graphics.
    modes[0] = KD_GRAPHICS;
    assert(console_restore() == 0);
    assert(active == 2 && modes[0] == KD_TEXT && modes[1] == KD_GRAPHICS);
    assert(console_restore() == 0);
    assert(destroyed == 1 && keyboard_fd == -1);

    never_ready = 1;
    key_writes = 0;
    assert(console_prepare() == 0);
    assert(console_enable() == ETIMEDOUT);
    assert(key_writes == 12 && destroyed == 1);
    assert(console_restore() == 0);
    assert(active == 2 && modes[0] == KD_TEXT && modes[1] == KD_GRAPHICS);

    short_write = 1;
    assert(console_prepare() == 0);
    assert(console_enable() == EIO);
    assert(console_restore() == 0);
    assert(active == 2 && modes[0] == KD_TEXT && modes[1] == KD_GRAPHICS);
    assert(destroyed == 3);

    // Failed device registration still restores both saved console modes.
    create_failure = 1;
    assert(console_prepare() == EIO);
    assert(console_restore() == 0);
    assert(destroyed == 3 && keyboard_fd == -1);
    assert(active == 2 && modes[0] == KD_TEXT && modes[1] == KD_GRAPHICS);
    return 0;
}
'''
        with tempfile.TemporaryDirectory() as directory:
            program = pathlib.Path(directory) / "console.c"
            binary = pathlib.Path(directory) / "console"
            program.write_text(harness)
            subprocess.run(["cc", "-D_GNU_SOURCE", "-fsanitize=undefined", str(program), "-o", str(binary)], check=True)
            subprocess.run([str(binary)], check=True)
