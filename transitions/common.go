package transitions

// Collection of common statuses
const (
	StatusNew       = "New"
	StatusBooting   = "Booting"
	StatusRunning   = "Running"
	StatusReloading = "Reloading"
	StatusStopping  = "Stopping"
	StatusStopped   = "Stopped"
	StatusError     = "Error"
	StatusUnknown   = "Unknown"
)

// Typical is a common set of transitions, useful as a guide. Each key is the current
// state, and the value is a list of valid next states the FSM can transition to.
var Typical = MustNew(map[string][]string{
	StatusNew:       {StatusBooting, StatusError},
	StatusBooting:   {StatusRunning, StatusError},
	StatusRunning:   {StatusReloading, StatusStopping, StatusError},
	StatusReloading: {StatusRunning, StatusError},
	StatusStopping:  {StatusStopped, StatusError},
	StatusStopped:   {StatusNew, StatusError},
	StatusError:     {StatusError, StatusStopping, StatusStopped},
	StatusUnknown:   {StatusUnknown},
})
