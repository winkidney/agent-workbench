package workbench

import "strings"

// Several workbenches can run for one user, each with its own data and
// address. An instance's name names its service, the units of its engines
// and previews (bound to that service), and its sign-in cookie: browsers
// share cookies between ports of one host name.

// DefaultInstance is the workbench that keeps its data in the default
// place, as every install did before there were others.
const DefaultInstance = "agent-workbench"

var (
	instance      = DefaultInstance
	sessionCookie = "workbench_session"
)

// SetInstance names this workbench; serve calls it before anything starts.
func SetInstance(name string) {
	if name == "" {
		name = DefaultInstance
	}
	instance = name
	Service = name + ".service"
	sessionCookie = cookieFor(name)
}

// Instance is this workbench's name.
func Instance() string { return instance }

// cookieFor keeps the default instance's cookie, so its devices stay
// signed in, and gives others their own.
func cookieFor(name string) string {
	if name == DefaultInstance {
		return "workbench_session"
	}
	suffix := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, strings.TrimPrefix(name, DefaultInstance+"-"))
	return "workbench_session_" + suffix
}
