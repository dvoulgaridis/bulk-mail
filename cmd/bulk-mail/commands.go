package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	runtimeconfig "github.com/dvoulgaridis/bulk-mail/internal/config"
	"github.com/dvoulgaridis/bulk-mail/internal/local"
)

type command string

const (
	commandRun            command = "run"
	commandConfigInit     command = "config-init"
	commandConfigShow     command = "config-show"
	commandConfigValidate command = "config-validate"
)

type options struct {
	command          command
	projectRoot      string
	workingDirectory string
	configPath       string
	dataDir          string
	config           runtimeconfig.Config
}

func parseOptions(args []string) (options, error) {
	workingDirectory, err := local.WorkingDirectory()
	if err != nil {
		return options{}, err
	}
	projectRoot, err := local.ProjectRoot(workingDirectory)
	if err != nil {
		return options{}, fmt.Errorf("locate project root: %w", err)
	}
	action, args, err := parseCommand(args)
	if err != nil {
		return options{}, err
	}
	configPath, err := resolveConfigPath(projectRoot, configArgument(args))
	if err != nil {
		return options{}, err
	}

	if action == commandConfigInit {
		if err := parseConfigCommandFlags("config init", args, &configPath, projectRoot); err != nil {
			return options{}, err
		}
		return options{
			command:          action,
			projectRoot:      projectRoot,
			workingDirectory: workingDirectory,
			configPath:       configPath,
		}, nil
	}

	cfg, found, err := runtimeconfig.Load(configPath)
	if err != nil {
		return options{}, fmt.Errorf("load %s: %w", configPath, err)
	}
	result := options{
		command:          action,
		projectRoot:      projectRoot,
		workingDirectory: workingDirectory,
		configPath:       configPath,
		config:           cfg,
	}

	switch action {
	case commandConfigShow:
		if err := parseConfigCommandFlags("config show", args, &result.configPath, projectRoot); err != nil {
			return options{}, err
		}
		if err := result.config.ApplyEnvironment(); err != nil {
			return options{}, err
		}
	case commandConfigValidate:
		if err := parseConfigCommandFlags("config validate", args, &result.configPath, projectRoot); err != nil {
			return options{}, err
		}
		if !found {
			return options{}, fmt.Errorf("configuration file does not exist: %s", configPath)
		}
	case commandRun:
		if err := result.config.ApplyEnvironment(); err != nil {
			return options{}, err
		}
		if err := parseRunFlags(args, &result); err != nil {
			return options{}, err
		}
	default:
		return options{}, fmt.Errorf("unsupported command %q", action)
	}

	if err := result.config.Validate(); err != nil {
		return options{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return result, nil
}

func parseCommand(args []string) (command, []string, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return commandRun, args, nil
	}
	switch args[0] {
	case "run":
		return commandRun, args[1:], nil
	case "config":
		if len(args) < 2 {
			return "", nil, errors.New("config command requires init, show, or validate")
		}
		switch args[1] {
		case "init":
			return commandConfigInit, args[2:], nil
		case "show":
			return commandConfigShow, args[2:], nil
		case "validate":
			return commandConfigValidate, args[2:], nil
		default:
			return "", nil, fmt.Errorf("unknown config command %q", args[1])
		}
	default:
		return "", nil, fmt.Errorf("unknown command %q", args[0])
	}
}

func parseRunFlags(args []string, result *options) error {
	flags := flag.NewFlagSet("bulk-mail run", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	configPath := flags.String("config", result.configPath, "configuration file; relative paths use the project root")
	dataDir := flags.String("data-dir", "", "data directory; relative paths use the project root")
	address := flags.String("address", result.config.Server.Address, "loopback HTTP address")
	port := flags.Int("port", result.config.Server.Port, "local HTTP port")
	noBrowser := flags.Bool("no-browser", false, "do not open the web UI automatically")
	maxConcurrent := flags.Int("max-concurrent", result.config.Tasks.MaxConcurrent, "maximum concurrent campaign tasks")
	maxQueued := flags.Int("max-queued", result.config.Tasks.MaxQueued, "maximum queued campaign tasks")
	shutdownTimeout := flags.Duration(
		"shutdown-timeout",
		result.config.Tasks.ShutdownTimeout.Duration,
		"graceful shutdown timeout",
	)
	logLevel := flags.String("log-level", result.config.Logging.Level, "logging level: debug, info, warn, or error")
	logFile := flags.String(
		"log-file",
		result.config.Logging.File,
		"optional log file; relative paths use the project root",
	)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage:")
		fmt.Fprintln(flags.Output(), "  bulk-mail [run] [options]")
		fmt.Fprintln(flags.Output(), "  bulk-mail config init [--config path]")
		fmt.Fprintln(flags.Output(), "  bulk-mail config show [--config path]")
		fmt.Fprintln(flags.Output(), "  bulk-mail config validate [--config path]")
		fmt.Fprintln(flags.Output(), "\nRun options:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}

	resolvedConfigPath, err := resolveConfigPath(result.projectRoot, *configPath)
	if err != nil {
		return err
	}
	result.configPath = resolvedConfigPath
	result.dataDir = *dataDir
	result.config.Server.Address = *address
	result.config.Server.Port = *port
	if *noBrowser {
		result.config.Server.OpenBrowser = false
	}
	result.config.Tasks.MaxConcurrent = *maxConcurrent
	result.config.Tasks.MaxQueued = *maxQueued
	result.config.Tasks.ShutdownTimeout.Duration = *shutdownTimeout
	result.config.Logging.Level = *logLevel
	result.config.Logging.File = *logFile
	return nil
}

func parseConfigCommandFlags(name string, args []string, configPath *string, projectRoot string) error {
	flags := flag.NewFlagSet("bulk-mail "+name, flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	path := flags.String("config", *configPath, "configuration file; relative paths use the project root")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: bulk-mail %s [--config path]\n\nOptions:\n", name)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	resolved, err := resolveConfigPath(projectRoot, *path)
	if err != nil {
		return err
	}
	*configPath = resolved
	return nil
}

func executeConfigCommand(opts options) error {
	switch opts.command {
	case commandConfigInit:
		if err := runtimeconfig.WriteDefault(opts.configPath); err != nil {
			return fmt.Errorf("create %s: %w", opts.configPath, err)
		}
		fmt.Printf("Created %s\n", opts.configPath)
	case commandConfigShow:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "    ")
		if err := encoder.Encode(opts.config); err != nil {
			return fmt.Errorf("print configuration: %w", err)
		}
	case commandConfigValidate:
		fmt.Printf("Configuration is valid: %s\n", opts.configPath)
	default:
		return fmt.Errorf("unsupported config command %q", opts.command)
	}
	return nil
}

func configArgument(args []string) string {
	for index, argument := range args {
		if argument == "-config" || argument == "--config" {
			if index+1 < len(args) {
				return args[index+1]
			}
			return ""
		}
		if value, ok := strings.CutPrefix(argument, "-config="); ok {
			return value
		}
		if value, ok := strings.CutPrefix(argument, "--config="); ok {
			return value
		}
	}
	return ""
}

func resolveConfigPath(projectRoot, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return defaultConfigPath(projectRoot)
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(projectRoot, value)
	}
	return filepath.Abs(value)
}

func defaultConfigPath(projectRoot string) (string, error) {
	if executable, err := os.Executable(); err == nil {
		candidate, err := filepath.Abs(filepath.Join(filepath.Dir(executable), runtimeconfig.Filename))
		if err != nil {
			return "", err
		}
		info, err := os.Stat(candidate)
		if err == nil {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("executable configuration is not a regular file: %s", candidate)
			}
			return candidate, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect executable configuration: %w", err)
		}
	}
	return filepath.Abs(filepath.Join(projectRoot, runtimeconfig.Filename))
}
