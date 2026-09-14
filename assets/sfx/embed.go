// Package sfx contains the inherited MiSTerFin navigation and confirmation clips.
package sfx

import _ "embed"

// Navigation is a 48 kHz stereo PCM WAV. Callers must not modify it.
//
//go:embed nav.wav
var Navigation []byte

// Confirmation is a 48 kHz stereo PCM WAV. Callers must not modify it.
//
//go:embed confirm.wav
var Confirmation []byte
