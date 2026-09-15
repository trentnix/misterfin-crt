package main

import "testing"

func TestUpdaterOnlyTargetsPermanentPair(t *testing.T) {
	good := launchOptions{browse: true, player: installationRoot + "/mplayer-arm"}
	if installedUpdater(good, installationRoot+"/misterfin-crt") == nil {
		t.Fatal("permanent pair disabled")
	}
	for _, kind := range []string{"headless", "preview", "custom-player", "custom-client"} {
		t.Run(kind, func(t *testing.T) {
			o := good
			executable := installationRoot + "/misterfin-crt"
			switch kind {
			case "headless":
				o.headless = "640x240"
			case "preview":
				o.browse = false
			case "custom-player":
				o.player = "/tmp/player"
			case "custom-client":
				executable = "/tmp/client"
			}
			if installedUpdater(o, executable) != nil {
				t.Fatal("enabled updater for a different installation")
			}
		})
	}
}
