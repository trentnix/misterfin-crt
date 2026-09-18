package connection

// SignInStorageTitle names a failure to persist or restore authentication.
const SignInStorageTitle = "Can't save or read sign-in"

// SignInStorageMessage explains how to recover access to private sign-in storage.
const SignInStorageMessage = "Make sure the sign-in folder is readable and writable, then retry."

// CodeExpiredTitle names an expired account-linking code.
const CodeExpiredTitle = "Code expired"

// SignInRequiredTitle identifies rejected authentication.
const SignInRequiredTitle = "Sign-in required"

// ConfigurationTitle identifies invalid connection settings.
const ConfigurationTitle = "Check your configuration"

// AddressRecoveryMessage explains automatic address rediscovery.
const AddressRecoveryMessage = "The saved address is unavailable.\nChecking for a new address."

// Account decisions and interrupted local credential cleanup.
const (
	SignInCanceledTitle      = "Sign-in canceled"
	SignInCanceledMessage    = "No replacement sign-in was saved.\nRetry, or choose another connection."
	SignOutIncompleteTitle   = "Signed out, but cleanup needs attention"
	SignOutIncompleteMessage = "The previous sign-in will not be reused.\nMake the sign-in folder writable, then retry cleanup."
)

// Retained connection selection failures.
const (
	titleConnectionNotSaved   = "Can't remember connection"
	messageConnectionNotSaved = "Make sure the sign-in folder is writable, then retry."
)
