package main

// User-facing failure and recovery text belongs here. Keep error classification
// and message selection at the call sites. These constants contain no private data.

// Startup configuration and resource fallbacks.
const (
	messageSavedConnectionUnavailable = "Saved connection is unavailable. Using configured startup."
	messageSavedConnectionUnreadable  = "Saved connection choice could not be read. Using configured startup."
	messageDiagnosticsInvalid         = "Check diagnostics settings. Logging is off."
	messageDiagnosticsUnavailable     = "Cannot open diagnostic log. Check its path and permissions. Logging is off."
	messageSoundsUnavailable          = "Navigation sounds unavailable. Continuing without feedback."
	messageMusicInvalid               = "Check music configuration and assets. Music backgrounds are off."
	messageTitleInvalid               = "Could not load title settings. Using MiSTerVision."
	messageCarouselInvalid            = "Invalid carousel options use their defaults."
	messageBackgroundUnavailable      = "Custom background unavailable. Using normal artwork."
	messageSoundsInvalid              = "Check sound settings. Navigation sounds are off."
)

// Command-line failures and update recovery.
const (
	messageUpdateRolledBack           = "An interrupted update was rolled back."
	messageDiagnosticsStopped         = "Diagnostics stopped: could not write log."
	messageArgumentsInvalid           = "unexpected arguments or negative hold duration"
	messageWaitHoldConflict           = "use either -wait or -hold"
	messageBrowseWaitConflict         = "-browse cannot be combined with -wait or -hold"
	messageTerminalPlayerArguments    = "-terminal-player requires -browse, -headless, and -output, without -player"
	messageAudioPlayerArguments       = "-audio-player requires headless browsing without -player"
	messageMigrationNotNeeded         = "server settings already exist; migration is not needed"
	messageConnectionSelectionInvalid = "unknown connection selection"
)
