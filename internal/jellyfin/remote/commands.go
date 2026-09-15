// Package remote implements Jellyfin's authenticated session control source.
package remote

import (
	"encoding/json"
	"strconv"
	"strings"

	"misterfin-crt/internal/remote"
)

// decode translates supported protocol messages. Unknown commands are ignored.
func decode(data []byte) (remote.Command, bool) {
	var envelope struct {
		MessageType string
		Data        json.RawMessage
	}
	if json.Unmarshal(data, &envelope) != nil {
		return remote.Command{}, false
	}
	var cmd remote.Command
	switch envelope.MessageType {
	case "Play":
		var p struct {
			ItemIDs            []string
			PlayCommand        string
			StartIndex         int
			StartPositionTicks *int64
		}
		if json.Unmarshal(envelope.Data, &p) != nil || len(p.ItemIDs) == 0 || len(p.ItemIDs) > 10000 || p.StartIndex < 0 || p.StartIndex >= len(p.ItemIDs) {
			return cmd, false
		}
		modes := map[string]remote.PlayMode{"PlayNow": remote.PlayNow, "PlayNext": remote.PlayNext, "PlayLast": remote.PlayLast, "PlayShuffle": remote.PlayShuffle, "PlayInstantMix": remote.PlayMix}
		mode, ok := modes[p.PlayCommand]
		if !ok {
			return cmd, false
		}
		for _, id := range p.ItemIDs {
			if id == "" || len(id) > 128 {
				return cmd, false
			}
		}
		if p.StartPositionTicks != nil && *p.StartPositionTicks < 0 {
			return cmd, false
		}
		cmd = remote.Command{Kind: remote.Play, IDs: p.ItemIDs, PlayMode: mode, StartIndex: p.StartIndex, Position: p.StartPositionTicks}
	case "Playstate":
		var p struct {
			Command           string
			SeekPositionTicks *int64
		}
		if json.Unmarshal(envelope.Data, &p) != nil {
			return cmd, false
		}
		kinds := map[string]remote.Kind{"playpause": remote.TogglePause, "pause": remote.Pause, "unpause": remote.Resume, "stop": remote.Stop, "nexttrack": remote.Next, "previoustrack": remote.Previous, "seek": remote.Seek}
		var ok bool
		cmd.Kind, ok = kinds[strings.ToLower(p.Command)]
		if !ok {
			return cmd, false
		}
		cmd.Position = p.SeekPositionTicks
		if cmd.Kind == remote.Seek && (cmd.Position == nil || *cmd.Position < 0) {
			return cmd, false
		}
	case "GeneralCommand":
		var p struct {
			Name      string
			Arguments map[string]string
		}
		if json.Unmarshal(envelope.Data, &p) != nil {
			return cmd, false
		}
		switch p.Name {
		case "DisplayMessage":
			cmd.Kind = remote.Message
			cmd.Header = clean(p.Arguments["Header"], 80)
			cmd.Text = clean(p.Arguments["Text"], 400)
			if cmd.Text == "" {
				return cmd, false
			}
		case "SetRepeatMode":
			cmd.Kind = remote.Repeat
			cmd.Repeat = remote.RepeatMode(p.Arguments["RepeatMode"])
			if cmd.Repeat != remote.RepeatNone && cmd.Repeat != remote.RepeatAll && cmd.Repeat != remote.RepeatOne {
				return cmd, false
			}
		case "SetShuffleQueue":
			mode := p.Arguments["ShuffleMode"]
			if mode != "Shuffle" && mode != "Sorted" {
				return cmd, false
			}
			cmd.Kind = remote.Shuffle
			cmd.Shuffled = mode == "Shuffle"
		case "SetPlaybackOrder":
			mode := p.Arguments["PlaybackOrder"]
			if mode != "Shuffle" && mode != "Default" {
				return cmd, false
			}
			cmd.Kind = remote.Shuffle
			cmd.Shuffled = mode == "Shuffle"
		default:
			return cmd, false
		}
	default:
		return cmd, false
	}
	return cmd, true
}

func clean(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if r < ' ' || r == 127 {
			return ' '
		}
		return r
	}, s)
	return string([]rune(s)[:min(len([]rune(s)), limit)])
}

// keepAliveInterval reads Jellyfin's requested heartbeat interval in seconds.
func keepAliveInterval(data []byte) int {
	var p struct {
		MessageType string
		Data        json.RawMessage
	}
	if json.Unmarshal(data, &p) != nil || p.MessageType != "ForceKeepAlive" {
		return 0
	}
	var seconds int
	if json.Unmarshal(p.Data, &seconds) != nil {
		var value string
		if json.Unmarshal(p.Data, &value) != nil {
			return 0
		}
		seconds, _ = strconv.Atoi(value)
	}
	if seconds <= 0 {
		return 0
	}
	return max(1, min(20, seconds/2))
}
