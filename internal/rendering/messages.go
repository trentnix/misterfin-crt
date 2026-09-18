package rendering

// User-facing failure and recovery text belongs here. Keep error classification
// and message selection at the call sites. These constants contain no private data.

// Unavailable resources and manual update guidance.
const (
	messagePhotoFailed            = "Could not load this photo."
	messageUpdateManual           = "Install the release ZIP manually on this device."
	messageNoUpdateBundle         = "No installation bundle is available."
	messageUpdateCheckUnavailable = "Update checks unavailable"
)
