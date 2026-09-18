package connection

// User-facing failure and recovery text belongs here. Keep error classification
// and message selection at the call sites. These constants contain no private data.

// Jellyfin connection, discovery, and sign-in recovery.
const (
	titleConnectFailed          = "Can't connect to Jellyfin"
	messageConnectFailed        = "Check your server address and network connection.\nMake sure Jellyfin is running, then retry."
	titleRecoveryFailed         = "Your Jellyfin server is unavailable"
	messageRecoveryFailed       = "Check that your server is running, then retry.\nYour saved server and sign-in have been kept."
	titleDiscoveryFailed        = "Can't find Jellyfin servers"
	messageDiscoveryFailed      = "Check Jellyfin and your local network, then retry.\nOr set server.url in settings.json."
	titleNoServers              = "No Jellyfin servers found"
	titleSavedServerInvalid     = "Check saved server"
	messageSavedServerInvalid   = "Make sure this file is valid and its folder is writable.\nRestore or remove the file, or configure server.url."
	messageConfigInvalid        = "Use an HTTP or HTTPS server address on the first line.\nCheck any optional settings, then retry."
	titleSetupNeeded            = "Setup needed"
	messageSetupNeeded          = "Create this file and add your Jellyfin server address.\nFor example: http://192.168.1.10:8096"
	titleConfigUnreadable       = "Can't read configuration"
	messageConfigUnreadable     = "Make sure this file exists and is readable, then retry."
	messageCodeExpired          = "Request a new code, then approve it in Jellyfin."
	titleQuickConnectDisabled   = "Quick Connect is disabled"
	messageQuickConnectDisabled = "Enable Quick Connect on your Jellyfin server, then retry.\nOr add an API key and username to your configuration."
	titleUsernameInvalid        = "Check your username"
	messageUsernameInvalid      = "The configured username was not found on the server.\nCheck the configured username, then retry."
	messageSignInRejected       = "Jellyfin rejected your sign-in. Sign in again.\nIf you use an API key, check it in your configuration."
	messageSignInRecovered      = "Saved sign-in was damaged and backed up.\nOpen Quick Connect in Jellyfin and approve this code."
)
