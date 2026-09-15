package main

import (
	"os"
	"syscall"

	misterupdate "misterfin-crt/internal/mister/update"
)

const installationRoot = "/media/fat/misterfin-crt"
const installationLauncher = "/media/fat/Scripts/MiSTerFin-CRT.sh"

// installedUpdater only enables replacement of the standard, paired MiSTer
// installation. Desktop and custom test locations retain manual installation.
func installedUpdater(o launchOptions, executable string) *misterupdate.Installer {
	if o.headless != "" || !o.browse || executable != installationRoot+"/misterfin-crt" || o.player != installationRoot+"/mplayer-arm" {
		return nil
	}
	return misterupdate.New(installationRoot, installationLauncher)
}

// recoverUpdate runs before settings, display setup, or decoder selection. If
// rollback replaced the client, re-exec it so its code matches the restored pair.
func recoverUpdate(o launchOptions) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	installer := installedUpdater(o, executable)
	if installer == nil {
		return nil
	}
	restored, err := installer.Recover()
	if err != nil {
		return err
	}
	if restored {
		_ = os.Setenv("MISTERFIN_CRT_UPDATE_RECOVERED", "1")
		return syscall.Exec(executable, os.Args, os.Environ())
	}
	return nil
}
