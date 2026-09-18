package plex

// User-facing failure and recovery text belongs here. Keep error classification
// and message selection at the call sites. These constants contain no private data.

// Plex connection, discovery, and profile recovery.
const (
	titleConnectFailed             = "Can't connect to Plex"
	messageConnectFailed           = "Check the server address and network connection.\nMake sure Plex Media Server is running, then retry."
	messageDiscoveredConnectFailed = "Check your server and internet connection, then retry.\nPress Back to choose a server again."
	titleProfileFailed             = "Can’t open profile"
	messageProfileFailed           = "Check your internet connection and Plex Home settings, then retry.\nYour previous connection has been kept."
	messageCodeExpired             = "Request a new code, then approve it at plex.tv/link."
	messageSignInRejected          = "Plex rejected your sign-in. Link your account again.\nThe account must have access to this server."
	titleRecoveryFailed            = "Plex server unavailable"
	messageRecoveryFailed          = "Check your server and network, then retry.\nYour saved connection and sign-in have been kept.\nPress Back to choose a server."
	titleNoServers                 = "No Plex servers"
	messageNoServers               = "This account has no available media servers.\nCheck server sharing and your Plex account, then retry."
	titleServersUnavailable        = "Plex servers unavailable"
	messageServersUnavailable      = "Check that your server is running and its network addresses are correct.\nRetry, or configure its address in settings.json."
	titleDiscoveryFailed           = "Can't find Plex servers"
	messageDiscoveryFailed         = "Check your internet connection and Plex account, then retry.\nYour saved connection has not been cleared."
	messageConfigInvalid           = "Set server.url to your Plex server's HTTP or HTTPS address.\nFor example: http://192.168.1.10:32400"
	messagePINIncorrect            = "Incorrect PIN. Try again."
)
