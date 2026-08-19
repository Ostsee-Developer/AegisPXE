package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Ostsee-Developer/AegisPXE/internal/lifecycle"
	"github.com/Ostsee-Developer/AegisPXE/internal/reporter"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "daemon":
		err = runDaemon(ctx, os.Args[2:])
	case "event":
		err = runEvent(os.Args[2:])
	case "install-firstboot":
		err = runInstallFirstBoot(ctx, os.Args[2:])
	case "first-boot":
		err = runFirstBoot(ctx, os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "aegispxe-reporter: %v\n", err)
		os.Exit(1)
	}
}

func runDaemon(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("daemon", flag.ContinueOnError)
	configPath := flags.String("config", reporter.InstallerConfigPath, "reporter configuration path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := reporter.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	return reporter.RunDaemon(ctx, cfg)
}

func runEvent(args []string) error {
	flags := flag.NewFlagSet("event", flag.ContinueOnError)
	source := flags.String("source", string(lifecycle.SourceInstaller), "lifecycle event source")
	message := flags.String("message", "", "bounded lifecycle message")
	errorCode := flags.String("error-code", "", "stable error code for FAILED")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("event requires exactly one lifecycle stage")
	}
	stage := lifecycle.Stage(strings.ToUpper(strings.TrimSpace(flags.Arg(0))))
	sourceValue := lifecycle.Source(strings.ToLower(strings.TrimSpace(*source)))
	if !stage.Valid() || !sourceValue.Valid() {
		return errors.New("invalid lifecycle stage or source")
	}
	return reporter.QueueHookEvent(stage, sourceValue, *message, *errorCode)
}

func runInstallFirstBoot(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("install-firstboot", flag.ContinueOnError)
	configPath := flags.String("config", reporter.InstallerConfigPath, "reporter configuration path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("install-firstboot requires the installed system target path")
	}
	cfg, err := reporter.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	return reporter.InstallFirstBoot(ctx, cfg, flags.Arg(0))
}

func runFirstBoot(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("first-boot", flag.ContinueOnError)
	configPath := flags.String("config", reporter.SystemConfigPath, "reporter configuration path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("first-boot takes no positional arguments")
	}
	cfg, err := reporter.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	return reporter.RunFirstBoot(ctx, cfg)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: aegispxe-reporter <daemon|event|install-firstboot|first-boot> [options]")
}
