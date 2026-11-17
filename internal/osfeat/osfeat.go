// Package osfeat holds the features of offstage that depend on the operating
// system. Each system supplies its backends in its own file. Shared code calls
// the interfaces and does not check the system. A nil backend means that the
// feature is not supported here.
package osfeat

// Window is a window of the human's desktop, as a backend needs it.
type Window struct {
	Class string // app id
	Title string
}

// Prompts recognizes the windows that ask the human for a secret: polkit,
// keyring and pinentry prompts. No input tool may act on them.
type Prompts interface {
	IsPrompt(w Window) bool
}

// Dialogs keeps the file dialog of an application in its screen, away from
// the human's desktop.
type Dialogs interface {
	Env() map[string]string
}

// Set holds the backends of this system.
type Set struct {
	Prompts Prompts
	Dialogs Dialogs
}

// LaunchEnv returns the environment that the backends add to an application
// that starts in the screen.
func (s Set) LaunchEnv(screen string) map[string]string {
	env := map[string]string{}
	if s.Dialogs != nil {
		for k, v := range s.Dialogs.Env() {
			env[k] = v
		}
	}
	return env
}
