package main

import (
	"errors"
	"flag"
	"testing"
)

func TestLaunchOptionsRejectConflictingModes(t *testing.T) {
	t.Setenv("MISTERFIN_FB", "")
	t.Setenv("MISTERFIN_FRAME_OUT", "")
	for _, args := range [][]string{
		{"unexpected"}, {"-hold=-1s"}, {"-wait", "-hold=1s"},
		{"-browse", "-wait"}, {"-browse", "-hold=1s"},
		{"-terminal-player=helper.py"},
		{"-browse", "-headless=640x240", "-terminal-player=helper.py"},
		{"-browse", "-headless=640x240", "-output=frame", "-terminal-player=helper.py", "-player=other"},
		{"-browse", "-audio-player=helper.py"},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("accepted conflicting options: %v", args)
		}
	}
}

func TestLaunchOptionsUseEnvironmentWithoutRetainingPreviousParse(t *testing.T) {
	t.Setenv("MISTERFIN_FB", "640x240")
	t.Setenv("MISTERFIN_FRAME_OUT", "frame")
	o, err := parseOptions([]string{"-browse", "-terminal-player=helper.py"})
	if err != nil || !o.browse || o.headless != "640x240" || o.output != "frame" {
		t.Fatalf("%+v: %v", o, err)
	}
	o, err = parseOptions([]string{"-headless=640x288"})
	if err != nil || o.browse || o.terminalPlayer != "" || o.headless != "640x288" {
		t.Fatalf("parse retained previous flags: %+v: %v", o, err)
	}
	if _, err = parseOptions([]string{"-help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help: %v", err)
	}
}

func TestInputConfigFlagOverridesEnvironment(t *testing.T) {
	t.Setenv("MISTERFIN_INPUT_CONFIG", "/tmp/controller.json")
	o, err := parseOptions(nil)
	if err != nil || o.inputConfig != "/tmp/controller.json" {
		t.Fatalf("environment default: %+v: %v", o, err)
	}
	o, err = parseOptions([]string{"-input-config", "/tmp/other-controller.json"})
	if err != nil || o.inputConfig != "/tmp/other-controller.json" {
		t.Fatalf("explicit path: %+v: %v", o, err)
	}
}

func TestSoundConfigOverride(t *testing.T) {
	t.Setenv("MISTERFIN_SOUND_CONFIG", "/tmp/sounds.json")
	o, err := parseOptions(nil)
	if err != nil || o.soundConfig != "/tmp/sounds.json" {
		t.Fatalf("environment: %+v %v", o, err)
	}
	o, err = parseOptions([]string{"-sound-config", "/tmp/quiet.json"})
	if err != nil || o.soundConfig != "/tmp/quiet.json" {
		t.Fatalf("flag: %+v %v", o, err)
	}
}

func TestSettingsPathFlagOverridesEnvironment(t *testing.T) {
	t.Setenv("MISTERFIN_SETTINGS", "/tmp/from-env.json")
	o, err := parseOptions(nil)
	if err != nil || o.settingsPath != "/tmp/from-env.json" {
		t.Fatal("settings environment lost", err)
	}
	o, err = parseOptions([]string{"-settings", "/tmp/from-flag.json", "-migrate-settings"})
	if err != nil || o.settingsPath != "/tmp/from-flag.json" || !o.migrateSettings {
		t.Fatal("settings flags lost", err)
	}
}
