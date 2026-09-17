package connection

// Choice describes a connection route without exposing credentials or backend
// implementations. Help is nonempty when setup is needed before it can be used.
type Choice struct {
	ID, Name, Description string
	Help                  string
	Children              []Choice
}

// Change requests a different connection after the current browser has canceled
// and cleaned up its session. Application assembly resolves ID to a Connector.
type Change struct {
	ID string
	// ReturnID identifies the last connected route if the new attempt is canceled.
	// Empty means there is no connected browser to return to.
	ReturnID string
}

// Error identifies the control-flow request without including server details.
func (*Change) Error() string { return "change connection" }
