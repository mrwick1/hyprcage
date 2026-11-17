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

// Audio gives each screen its own audio output, so that its apps do not
// play into the human's speakers.
type Audio interface {
	Ensure(screen string) error
	Env(screen string) map[string]string
	Remove(screen string) error
	Prune(live map[string]bool) error // remove the outputs of screens not in live
	Observe(screen string) (AudioState, error)
}

// AudioState is the state of a screen's audio output.
type AudioState struct {
	Sink    string `json:"sink"`
	State   string `json:"state"`   // RUNNING, IDLE, SUSPENDED, or absent before the first launch
	Streams int    `json:"streams"` // streams that play into the output
}

// Playing reports whether a stream plays into the output and is not paused.
func (a AudioState) Playing() bool { return a.State == "RUNNING" }

// Set holds the backends of this system.
type Set struct {
	Prompts Prompts
	Dialogs Dialogs
	Audio   Audio
}

// LaunchEnv returns the environment that the backends add to an application
// that starts in the screen. It creates the screen's audio output first.
// When that fails, the app plays into the human's default output, as before.
func (s Set) LaunchEnv(screen string) map[string]string {
	env := map[string]string{}
	if s.Dialogs != nil {
		for k, v := range s.Dialogs.Env() {
			env[k] = v
		}
	}
	if s.Audio != nil && s.Audio.Ensure(screen) == nil {
		for k, v := range s.Audio.Env(screen) {
			env[k] = v
		}
	}
	return env
}
