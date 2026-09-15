// Package remote defines display-independent media commands and queue state.
// Sources translate an external protocol. The application owns execution.
package remote

import "context"

// Source supplies commands for one authenticated application session.
// Run must honor cancellation and may reconnect internally. emit can block until
// the application consumes a command. Run must stop calling it before returning.
// Publish must not perform I/O or block on network work. It copies any retained
// slices and may run concurrently with Run. No method may mutate caller data.
type Source interface {
	Run(context.Context, func(Command))
	Publish(QueueState)
}

// Kind identifies a semantic command, independent of physical buttons or menus.
type Kind string

const (
	Play        Kind = "play"         // Replace or extend the playback queue.
	Pause       Kind = "pause"        // Pause without toggling an already paused player.
	Resume      Kind = "resume"       // Resume without toggling a running player.
	TogglePause Kind = "toggle-pause" // Toggle playback pause.
	Stop        Kind = "stop"         // Stop media without exiting the application.
	Next        Kind = "next"         // Select the next queue entry.
	Previous    Kind = "previous"     // Select the previous entry with one command.
	Seek        Kind = "seek"         // Seek to an absolute position. Live TV cannot seek.
	Repeat      Kind = "repeat"       // Set repeat mode.
	Shuffle     Kind = "shuffle"      // Set queue shuffle mode.
	Message     Kind = "message"      // Show a temporary on-screen message.
)

// Command is an owned value. Item IDs belong to the authenticated media catalog.
// Position uses 100-nanosecond ticks. Nil lets the application choose a start.
// PlayMode specifies queue replacement or insertion. StartIndex indexes the supplied IDs.
type Command struct {
	Kind         Kind
	IDs          []string
	PlayMode     PlayMode
	StartIndex   int
	Position     *int64
	Repeat       RepeatMode
	Shuffled     bool
	Header, Text string
}

// PlayMode determines how a Play command changes the current queue.
type PlayMode string

const (
	PlayNow     PlayMode = "now"     // Replace the queue and start its selected entry.
	PlayNext    PlayMode = "next"    // Insert immediately after the current entry.
	PlayLast    PlayMode = "last"    // Append to the queue.
	PlayShuffle PlayMode = "shuffle" // Start the supplied items in random order.
	PlayMix     PlayMode = "mix"     // Resolve a catalog-generated mix before playing.
)

// RepeatMode controls automatic advancement. Explicit Next ignores RepeatOne.
type RepeatMode string

const (
	RepeatNone RepeatMode = "RepeatNone" // Stop after the last entry.
	RepeatAll  RepeatMode = "RepeatAll"  // Wrap at queue boundaries.
	RepeatOne  RepeatMode = "RepeatOne"  // Replay the current entry after completion.
)

// Entry identifies one queue occurrence. Key distinguishes duplicate media IDs.
type Entry struct{ ID, Key string }

// QueueState is a snapshot for external controllers. Current is the occurrence
// key, not the media ID. An empty Current means nothing is selected.
type QueueState struct {
	Entries  []Entry
	Current  string
	Repeat   RepeatMode
	Shuffled bool
}
