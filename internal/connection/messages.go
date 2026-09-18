package connection

// SignInStorageTitle names a failure to persist or restore authentication.
const SignInStorageTitle = "Can't save or read sign-in"

// SignInStorageMessage explains recovery without clearing saved credentials.
const SignInStorageMessage = "Make sure this folder is writable, then retry.\nYour saved sign-in has not been cleared."

// CodeExpiredTitle names an expired account-linking code.
const CodeExpiredTitle = "Code expired"

// SignInRequiredTitle identifies rejected authentication.
const SignInRequiredTitle = "Sign-in required"

// ConfigurationTitle identifies invalid connection settings.
const ConfigurationTitle = "Check your configuration"

// AddressRecoveryMessage explains automatic address rediscovery.
const AddressRecoveryMessage = "The saved address is unavailable.\nChecking for a new address."

// Retained connection selection failures.
const (
	titleConnectionNotSaved   = "Can't remember connection"
	messageConnectionNotSaved = "Make sure the sign-in folder is writable, then retry."
)
