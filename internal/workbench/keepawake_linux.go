//go:build linux

package workbench

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// Linux: sleep is held by a logind block inhibitor (a file descriptor that
// holds it until closed) and, where the desktop has it (KDE), the session's
// org.freedesktop.PowerManagement inhibition, which its power manager
// consults before an idle suspend. The screen lock is held by
// org.freedesktop.ScreenSaver (KDE and other freedesktop desktops), else by
// the GNOME session manager's idle inhibition. Both session-bus inhibitions
// belong to the connection that asked, so each hold keeps its own connection
// open; it ends when the connection closes, with the workbench at the latest.

const (
	awakeApp    = "Agent Workbench"
	awakeCallBy = 5 * time.Second
)

var awakeWhy = map[string]string{
	awakeSleep: "Prevent system sleep is on in Agent Workbench's settings",
	awakeLock:  "Prevent screen lock is on in Agent Workbench's settings",
}

func holdAwake(kind string) (awakeHold, string, error) {
	if kind == awakeSleep {
		return holdSystemAwake()
	}
	return holdScreenLock()
}

// linuxHold is what one switch holds: logind's descriptor, session-bus
// inhibitions (each with its release call), and the connection they need.
type linuxHold struct {
	fd       *os.File
	conn     *dbus.Conn
	releases []func()
}

func (h *linuxHold) Release() {
	for _, release := range h.releases {
		release()
	}
	if h.conn != nil {
		_ = h.conn.Close()
	}
	if h.fd != nil {
		_ = h.fd.Close()
	}
}

// Alive: logind keeps its inhibitor as long as the descriptor is open; the
// session-bus ones last as long as the connection.
func (h *linuxHold) Alive() bool {
	return h.conn == nil || h.conn.Connected()
}

func awakeCall(obj dbus.BusObject, method string, args ...any) *dbus.Call {
	ctx, cancel := context.WithTimeout(context.Background(), awakeCallBy)
	defer cancel()
	return obj.CallWithContext(ctx, method, 0, args...)
}

// sessionInhibit asks a session-bus service for an inhibition that returns a
// cookie, and gives the call that ends it.
func sessionInhibit(conn *dbus.Conn, service, path, iface, release string, args ...any) (func(), error) {
	obj := conn.Object(service, dbus.ObjectPath(path))
	var cookie uint32
	if err := awakeCall(obj, iface+".Inhibit", args...).Store(&cookie); err != nil {
		return nil, err
	}
	return func() { _ = awakeCall(obj, iface+"."+release, cookie).Err }, nil
}

func holdSystemAwake() (awakeHold, string, error) {
	h := &linuxHold{}
	var via []string
	var errs []string
	if fd, err := logindInhibit(); err != nil {
		errs = append(errs, "logind: "+err.Error())
	} else {
		h.fd = fd
		via = append(via, "logind")
	}
	if conn, err := dbus.ConnectSessionBus(); err != nil {
		errs = append(errs, "session bus: "+err.Error())
	} else if release, err := sessionInhibit(conn, "org.freedesktop.PowerManagement",
		"/org/freedesktop/PowerManagement/Inhibit", "org.freedesktop.PowerManagement.Inhibit",
		"UnInhibit", awakeApp, awakeWhy[awakeSleep]); err != nil {
		// Only some desktops have it; logind's inhibitor is the main one.
		_ = conn.Close()
		if !unknownService(err) {
			errs = append(errs, "power management: "+err.Error())
		}
	} else {
		h.conn, h.releases = conn, []func(){release}
		via = append(via, "desktop power management")
	}
	if len(via) == 0 {
		return nil, "", errors.New(strings.Join(errs, "; "))
	}
	return h, strings.Join(via, " + "), nil
}

func logindInhibit() (*os.File, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	// The inhibitor lives as long as the descriptor, not the connection.
	defer conn.Close()
	var fd dbus.UnixFD
	err = awakeCall(conn.Object("org.freedesktop.login1", "/org/freedesktop/login1"),
		"org.freedesktop.login1.Manager.Inhibit",
		"sleep", awakeApp, awakeWhy[awakeSleep], "block").Store(&fd)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "logind-inhibit"), nil
}

func holdScreenLock() (awakeHold, string, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, "", errors.New("could not reach the session bus: " + err.Error())
	}
	release, err := sessionInhibit(conn, "org.freedesktop.ScreenSaver",
		"/org/freedesktop/ScreenSaver", "org.freedesktop.ScreenSaver",
		"UnInhibit", awakeApp, awakeWhy[awakeLock])
	if err == nil {
		return &linuxHold{conn: conn, releases: []func(){release}}, "screen saver", nil
	}
	first := err
	// GNOME: the session manager's idle inhibition (flag 8) keeps the session
	// from going idle, which is what locks and blanks the screen.
	release, err = sessionInhibit(conn, "org.gnome.SessionManager",
		"/org/gnome/SessionManager", "org.gnome.SessionManager",
		"Uninhibit", awakeApp, uint32(0), awakeWhy[awakeLock], uint32(8))
	if err == nil {
		return &linuxHold{conn: conn, releases: []func(){release}}, "GNOME session", nil
	}
	_ = conn.Close()
	if unknownService(first) && unknownService(err) {
		return nil, "", errors.New("this desktop offers no screen saver inhibition on the session bus")
	}
	return nil, "", errors.New("screen saver: " + first.Error() + "; GNOME session: " + err.Error())
}

func unknownService(err error) bool {
	var e dbus.Error
	return errors.As(err, &e) && (e.Name == "org.freedesktop.DBus.Error.ServiceUnknown" ||
		e.Name == "org.freedesktop.DBus.Error.UnknownObject" ||
		e.Name == "org.freedesktop.DBus.Error.UnknownMethod")
}
