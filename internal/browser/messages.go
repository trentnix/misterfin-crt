package browser

// User-facing failure and recovery text belongs here. Keep error classification
// and message selection at the call sites. These constants contain no private data.

// Request recovery guidance.
const (
	messageRetry                = "Try again."
	messageSignInAgain          = "Sign in again through About > Connections."
	messageTunerUnavailable     = "Try another channel or check that your tuner is available."
	messageConversionFailed     = "The server could not prepare this video. Check its playback and conversion settings."
	messageRequestTimeout       = "The server took too long to respond. Try again."
	messageServerUnavailable    = "Check your network and that the media server is running, then retry."
	messageItemMissing          = "It may have been moved or removed. Go back and refresh the list."
	messageServerFailed         = "The media server reported a problem. Try again or check the server."
	messageListFailed           = "Could not load this list."
	messageStreamFailed         = "Could not open this stream."
	messagePlaybackChangeFailed = "Could not change playback."
	messageRemoteItemsFailed    = "Could not load the requested items."
)

// Playback failures and nonfatal reporting warnings.
const (
	titlePlaybackNotStarted    = "Playback didn't start"
	titlePlaybackInterrupted   = "Playback interrupted"
	titleProgressFailed        = "Progress update failed"
	messagePlayerUnavailable   = "The media player is unavailable. Check the player configuration or reinstall the application and matching player."
	messageTrackUnavailable    = "The selected audio or subtitle track is no longer available. Reopen the item and choose another track."
	messageStartupTimeout      = "The stream did not start in time. Try playing it again. If this keeps happening, check your media server."
	messagePlayerNotStarted    = "The player could not start this stream. Try again. If it keeps failing, check playback on your media server."
	messagePlaybackInterrupted = "The player stopped unexpectedly. Try playing this item again."
	messageProgressFailed      = "Could not update your playback progress. Your resume position may be out of date."
	messagePictureFailed       = "Could not change the picture. Try again."
	messageSubtitleFailed      = "Could not load these subtitles. Try again or choose another track."
	messageControlFailed       = "The player could not apply that control. Try again."
	messageWaitForPlayback     = "Wait for playback before changing options"
	messageWaitForSubtitles    = "Wait for subtitles before changing options"
	messagePlayerBusy          = "Player is busy. Try again."
	messageUnsupportedPlayback = "Playback for this item type is not available yet."
)

// Browsing, selection, and queue failures.
const (
	messageListChanged           = "The list changed on the server. Go back and reopen it."
	messageLoadingCanceled       = "Loading canceled."
	messageFolderDepth           = "Maximum folder depth reached"
	messageContinueIncomplete    = "Continue Watching incomplete."
	messageContinueItemsFailed   = "Some Continue Watching items could not load."
	messageDetailsFailed         = "Could not load details. Use Retry to try again."
	messageCountFailed           = "Could not count this library. You can still open it."
	messagePhotoFailed           = "Could not load this photo."
	messageShuffleMoreFailed     = "Could not load more shuffle tracks. Try Next again."
	messageShuffleStartFailed    = "Could not start shuffle. Close this message and choose Shuffle again."
	messageMusicBackgroundFailed = "Background unavailable. Check music assets."
	messageQueueLimit            = "There are too many items to play at once. Choose fewer than 10001 items and try again."
)

// Update recovery and installation prerequisites.
const (
	messageNoRelease                = "No public release available."
	messageUpdateCheckFailed        = "Could not check for updates. Check your internet connection, then try Check updates again."
	messageUpdateStopPlayback       = "Stop playback before installing an update."
	messageUpdateRecovery           = "Recovery needed. Exit and relaunch MiSTerVision."
	messageUpdateCanceled           = "Update canceled. Existing installation kept."
	messageUpdateManual             = "Install the release ZIP manually. This updater cannot install it. Existing installation kept."
	messageUpdateNoSpace            = "Not enough free space. Free space on the SD card, then retry. Existing installation kept."
	messageUpdateReadOnly           = "Could not write the update. Check that the SD card is writable, then retry. Existing installation kept."
	messageUpdateStorageFailed      = "Could not read or write update files. Check the SD card, then retry. Existing installation kept."
	messageUpdateVerificationFailed = "The update could not be verified. Download it again. Existing installation kept."
	messageUpdateDownloadFailed     = "Could not download the update. Check your internet connection, then retry. Existing installation kept."
	messageUpdateInstallFailed      = "Could not install the update. Retry or install the release ZIP manually. Existing installation kept."
)

// Connection fallback.
const (
	titleServerUnavailable = "Server unavailable"
	messageNoConnection    = "No server connection is configured."
	messageRecoveredSignIn = "Damaged sign-in was backed up. Connected successfully."
)

// messageNeighborFailed formats a direction and media kind, such as next track.
const messageNeighborFailed = "Could not load the %s %s. Try again."

// Unavailable playback options.
const (
	messageNoCaptionData          = "No closed-caption data received."
	messageLiveAudioUnavailable   = "Live TV audio selection is not available."
	messageLivePictureUnavailable = "This player cannot change Live TV picture mode."
)

// titleRemotePlayback identifies failures while resolving or extending a remote queue.
const titleRemotePlayback = "Remote playback"
