package browser

import (
	"fmt"
	"strings"

	"misterfin-crt/internal/jellyfin"
)

func subtitle(i jellyfin.Item) (string, uint32) {
	color := uint32(0x585858)
	switch i.Type {
	case "TvChannel", "LiveTvChannel":
		if i.CurrentProgram.Name != "" {
			return i.CurrentProgram.Name, color
		}
		return "No guide information", color
	case "MusicArtist":
		return positiveCount(i.ChildCount, "album"), color
	case "MusicAlbum":
		parts := []string{}
		if i.ProductionYear > 0 {
			parts = append(parts, fmt.Sprint(i.ProductionYear))
		}
		if tracks := positiveCount(i.ChildCount, "track"); tracks != "" {
			parts = append(parts, tracks)
		}
		return strings.Join(parts, " - "), color
	case "Series":
		seasons := positiveCount(i.ChildCount, "season")
		if seasons != "" && i.RecursiveItemCount > 0 {
			seasons += " - " + positiveCount(i.RecursiveItemCount, "episode")
		}
		return seasons, color
	case "Audio":
		return runtime(i.RunTimeTicks), color
	}
	s := ""
	if i.RunTimeTicks > 0 {
		s = fmt.Sprintf("%d min", i.RunTimeTicks/600000000)
	}
	if i.UserData.Played {
		s += " - watched"
		color = 0x40cc40
	} else if i.UserData.PlaybackPositionTicks > 0 {
		s += " - resume " + runtime(i.UserData.PlaybackPositionTicks)
		color = 0xffc040
	}
	return strings.TrimPrefix(s, " - "), color
}

func itemTitle(i jellyfin.Item) string {
	s := i.Name
	if i.ContinueAction != "" && i.Type == "Episode" && i.SeriesName != "" {
		s = i.SeriesName + " - " + s
	}
	if jellyfin.IsLive(i) {
		number := i.Number
		if number == "" {
			number = i.ChannelNumber
		}
		if number != "" {
			return number + "  " + s
		}
		return s
	}
	if i.IsFolder || i.Type == "Series" || i.Type == "Season" || i.Type == "MusicArtist" || i.Type == "MusicAlbum" {
		s = "> " + s
	}
	if i.ProductionYear > 0 && (i.Type == "Movie" || i.Type == "MusicVideo" || i.Type == "Video" || i.Type == "Series") {
		s += fmt.Sprintf(" (%d)", i.ProductionYear)
	}
	return s
}

// positiveCount follows the C list metadata: omit unknown counts and pluralize.
func positiveCount(count int, name string) string {
	if count <= 0 {
		return ""
	}
	if count != 1 {
		name += "s"
	}
	return fmt.Sprintf("%d %s", count, name)
}

// continueSubtitle distinguishes starting an episode from resuming saved progress.
func continueSubtitle(item jellyfin.Item) string {
	parts := []string{}
	if item.ContinueAction == "resume" {
		parts = append(parts, "Resume", runtime(item.UserData.PlaybackPositionTicks))
	} else {
		parts = append(parts, "Next")
	}
	if item.Type == "Episode" {
		if item.ParentIndexNumber != nil && item.IndexNumber != nil {
			parts = append(parts, fmt.Sprintf("S%d E%d", *item.ParentIndexNumber, *item.IndexNumber))
		} else if item.IndexNumber != nil {
			parts = append(parts, fmt.Sprintf("Episode %d", *item.IndexNumber))
		}
	}
	return strings.Join(parts, " · ")
}
