package rendering

import (
	"fmt"
	"image"
	"strings"

	"mistervision/internal/input/control"
	"mistervision/internal/ui"
)

// profiles draws provider-neutral viewer selection and masked PIN entry. It
// receives decoded avatars and a digit count, never URLs or secret input.
func (p *screenPainter) profiles() {
	s, c := p.scene.Setup, p.canvas
	c.Rect(0, 0, p.width, p.height, 0x0b0d13)
	if len(s.Profiles) == 0 {
		return
	}
	selected := max(0, min(s.Selected, len(s.Profiles)-1))
	var hints []controlHint
	if s.Kind == SetupPIN && !s.PINChecking {
		hints = append(hints, pairedHint(p.scene.Controls, control.Up, control.Down, "Move"), pairedHint(p.scene.Controls, control.Previous, control.Next, "Move"))
	} else if s.Kind == SetupProfiles && len(s.Profiles) > 1 {
		hints = append(hints, pairedHint(p.scene.Controls, control.Previous, control.Next, "Choose"))
	}
	if !s.PINChecking {
		hints = append(hints, hint(p.scene.Controls, control.Open, "Select"))
	}
	if s.Kind == SetupProfiles && s.AddUser {
		hints = append(hints, hint(p.scene.Controls, control.Select, "Add user"))
	}
	if s.Kind == SetupProfiles && s.Forget {
		hints = append(hints, hint(p.scene.Controls, control.Down, "Forget user"))
	}
	hints = append(hints, hint(p.scene.Controls, control.Back, "Back"))
	rows := controlRows(p.width, hints)
	bottom := controlsTop(p.bottom, rows) - 12
	top := p.safeY + 8
	if s.Kind == SetupPIN {
		profile := s.Profiles[selected]
		const avatarSize, avatarGap = 64, 12
		name := truncate(profile.Name, p.width-48-avatarSize-avatarGap, 2)
		x := (p.width - textWidth(name, 2) - avatarSize - avatarGap) / 2
		drawProfileAvatar(c, x, top, avatarSize, profile.Avatar, titleColor)
		c.TextScaled(x+avatarSize+avatarGap, top+8, name, titleColor, p.width-24, 2)
		center(c, top+40, "Please enter your PIN:", 0xcccccc, 1)
		mask := ""
		for i := 0; i < 4; i++ {
			if i < s.PINLength {
				mask += "* "
			} else {
				mask += "_ "
			}
		}
		center(c, top+56, strings.TrimSpace(mask), 0xffffff, 2)
		labels := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "Delete", "0", "Back"}
		keypadTop := top + 82
		rowHeight := min(22, max(16, (bottom-keypadTop-14)/4))
		for i, label := range labels {
			x, y := p.width/2-114+(i%3)*76, keypadTop+(i/3)*rowHeight
			color := uint32(dimColor)
			if i == s.PINKey && !s.PINChecking {
				c.Rect(x, y-3, 72, rowHeight-2, 0x283446)
				color = titleColor
			}
			c.Text(x+36-textWidth(label, 1)/2, y+2, label, color, x+72)
		}
	} else {
		center(c, top, "Who’s watching?", titleColor, 2)
		start := max(0, min(selected-1, len(s.Profiles)-3))
		count := min(3, len(s.Profiles))
		step := min(188, (p.width-48)/count)
		for i := start; i < min(len(s.Profiles), start+3); i++ {
			profile := s.Profiles[i]
			cx := p.width/2 + (i-start)*step - (count-1)*step/2
			w, h := 96, 48
			color := uint32(dimColor)
			if i == selected {
				w, h = 120, 60
				color = titleColor
			}
			y := top + 46 + (60-h)/2
			c.Rect(cx-w/2-3, y-2, w+6, h+4, color)
			c.Rect(cx-w/2, y, w, h, 0x283446)
			if profile.Avatar != nil {
				c.Image(profile.Avatar, cx-w/2, y, w, h)
			} else {
				name := []rune(profile.Name)
				if len(name) > 0 {
					label := strings.ToUpper(string(name[0]))
					c.TextScaled(cx-textWidth(label, 3)/2, y+h/2-12, label, 0xffffff, cx+w/2, 3)
				}
			}
			label := truncate(profile.Name, step-16, 1)
			c.Text(cx-textWidth(label, 1)/2, top+118, label, color, cx+step/2)
			if profile.Protected {
				c.Text(cx-textWidth("PIN required", 1)/2, top+132, "PIN required", dimColor, cx+step/2)
			}
		}
		if len(s.Profiles) > 3 {
			center(c, bottom-16, fmt.Sprintf("%d / %d", selected+1, len(s.Profiles)), dimColor, 1)
		}
	}
	if s.PINChecking {
		setupActivity(c, bottom-12, p.animation.Seconds)
	}
	if s.Message != "" {
		center(c, bottom-2, truncate(s.Message, p.width-48, 1), titleColor, 1)
	}
	drawControls(c, p.bottom, rows)
}

// profileLabelInset reserves the avatar and its gap before the name.
const profileLabelInset = 30

// drawProfileLabel uses the viewer's decoded avatar with a silhouette fallback.
// The image accounts for the CRT's doubled vertical pixels. Rendering never
// fetches artwork or blocks on the provider.
func drawProfileLabel(c *ui.Canvas, x, y int, name string, avatar image.Image, color uint32) {
	drawProfileAvatar(c, x, y-2, 24, avatar, color)
	c.Text(x+profileLabelInset, y, name, color, c.Width-24)
}

// drawProfileAvatar reserves a box with half as many logical rows as columns.
// Canvas.Image preserves the artwork’s physical aspect ratio. Missing artwork
// uses a silhouette scaled to the same box.
func drawProfileAvatar(c *ui.Canvas, x, y, size int, avatar image.Image, color uint32) {
	if avatar != nil {
		c.Image(avatar, x, y, size, size/2)
		return
	}
	c.Rect(x+10*size/24, y+2*size/24, 4*size/24, size/24, color)
	c.Rect(x+9*size/24, y+3*size/24, 6*size/24, 2*size/24, color)
	c.Rect(x+10*size/24, y+5*size/24, 4*size/24, size/24, color)
	c.Rect(x+8*size/24, y+7*size/24, 8*size/24, size/24, color)
	c.Rect(x+6*size/24, y+8*size/24, 12*size/24, 2*size/24, color)
}
