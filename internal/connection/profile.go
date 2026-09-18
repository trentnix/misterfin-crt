package connection

import (
	"context"
	"image"
)

// ProfileAction identifies the viewer action offered by a session or requested
// for a connection attempt. The zero value keeps the current viewer.
type ProfileAction uint8

const (
	ProfileUnchanged ProfileAction = iota // No viewer change is offered or requested.
	ProfileChoose                         // Choose an existing viewer.
	ProfileAdd                            // Authenticate an additional viewer.
	ProfileForget                         // Remove the provider's saved sign-in from this device.
)

// Profile is a public viewing identity. Avatar is immutable display artwork.
// Protected requires provider authentication before opening the profile.
type Profile struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Protected bool        `json:"protected"`
	Avatar    image.Image `json:"-"`
	// AvatarKey changes with provider artwork. It contains no credentials.
	AvatarKey string `json:"-"`
}

// ProfilePrompt offers authorized identities and an optional PIN retry. Selected
// is an index into Profiles. PIN opens the selected profile's numeric keypad.
// Message must contain public instructions, never a provider response or PIN.
type ProfilePrompt struct {
	Profiles []Profile
	Avatars  ProfileAvatars // Optional artwork loaded independently of selection.
	Selected int
	PIN      bool
	Message  string
	// AddUser offers a separate action to authenticate another identity.
	AddUser bool
	// Forget offers removal of an independently saved user, not server membership.
	Forget bool
}

// ProfileSelection is a private reply to a profile prompt. PIN must never enter
// presentations, logs, URLs, or persistent state. The provider verifies the ID.
type ProfileSelection struct {
	ID  string
	PIN string
	// Action is normally ProfileUnchanged (select ID), ProfileAdd, or ProfileForget.
	Action ProfileAction
}

// ProfileAvatars loads public artwork for offered profiles. Implementations must
// honor cancellation and allow concurrent calls. Returned images are immutable.
// The browser bounds concurrency and keeps failures from blocking sign-in.
type ProfileAvatars interface {
	Load(context.Context, string) (image.Image, error)
}
