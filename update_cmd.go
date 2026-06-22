package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/update"
)

func updateCommand(args []string) {
	fs := flag.NewFlagSet("suna update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	checkOnly := fs.Bool("check", false, "check for updates without installing")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, daemonErr := queryDaemonStatus(ctx)
	cancel()
	if !*checkOnly && daemonErr == nil {
		fmt.Fprintln(os.Stderr, "Error: sunad is still running.")
		fmt.Fprintln(os.Stderr, "Please exit the TUI and run `suna stop`, then retry `suna update`.")
		os.Exit(1)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	opts := update.Options{DataDir: config.DefaultDataDir(), Stdout: os.Stdout}
	if *checkOnly {
		latest, err := update.Check(ctx, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking update: %s\n", err)
			os.Exit(1)
		}
		printUpdateStatus(latest)
		return
	}

	latest, err := update.Install(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating Suna: %s\n", err)
		os.Exit(1)
	}
	if !latest.UpdateNeeded {
		printUpdateStatus(latest)
		return
	}
	fmt.Printf("\nSuna updated to %s. Run `suna` to start the new version.\n", latest.LatestVersion)
}

func printUpdateStatus(latest update.Latest) {
	fmt.Printf("Current version: %s\n", latest.CurrentVersion)
	fmt.Printf("Latest version:  %s\n", latest.LatestVersion)
	if latest.ReleaseURL != "" {
		fmt.Printf("Release:         %s\n", latest.ReleaseURL)
	}
	if latest.UpdateNeeded {
		fmt.Println("Update available. Run `suna update` to install it.")
		return
	}
	fmt.Println("Already up to date.")
}
