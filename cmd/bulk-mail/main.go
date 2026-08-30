package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/dvoulgaridis/bulk-mail/frontend"
	runtimeconfig "github.com/dvoulgaridis/bulk-mail/internal/config"
	"github.com/dvoulgaridis/bulk-mail/internal/database"
	"github.com/dvoulgaridis/bulk-mail/internal/local"
	"github.com/dvoulgaridis/bulk-mail/internal/server"
	"github.com/dvoulgaridis/bulk-mail/internal/store"
)

func main() {
	if exitCode := execute(os.Args[1:]); exitCode != 0 {
		os.Exit(exitCode)
	}
}

func execute(args []string) int {
	opts, err := parseOptions(args)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bulk-mail:", err)
		return 2
	}
	if opts.command != commandRun {
		if err := executeConfigCommand(opts); err != nil {
			fmt.Fprintln(os.Stderr, "bulk-mail:", err)
			return 1
		}
		return 0
	}

	closeLog, err := configureLogging(opts.projectRoot, opts.config.Logging)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bulk-mail: configure logging:", err)
		return 1
	}
	defer closeLog()
	if err := run(opts); err != nil {
		slog.Error("application stopped", "error", err)
		return 1
	}
	return 0
}

func run(opts options) error {
	cfg := opts.config

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	paths, err := local.ResolvePaths(opts.projectRoot, opts.workingDirectory, opts.dataDir)
	if err != nil {
		return fmt.Errorf("resolve paths: %w", err)
	}
	if err := local.EnsureDataDir(paths); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	temporarySpace, err := local.OpenTemporarySpace(paths.WorkingDirectory)
	if err != nil {
		return fmt.Errorf("initialize temporary storage: %w", err)
	}
	defer func() {
		if err := temporarySpace.Close(); err != nil {
			slog.Warn("remove temporary storage", "error", err)
		}
	}()
	db, err := database.Open(paths.DatabasePath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	repo := store.New(db)
	if err := repo.SeedDefaults(ctx); err != nil {
		return fmt.Errorf("seed defaults: %w", err)
	}
	uiFS, err := frontend.FS()
	if err != nil {
		return fmt.Errorf("load web UI: %w", err)
	}
	srv, err := server.New(repo, paths, uiFS, server.Options{
		MaxConcurrentTasks:      cfg.Tasks.MaxConcurrent,
		MaxQueuedTasks:          cfg.Tasks.MaxQueued,
		ShutdownTimeout:         cfg.Tasks.ShutdownTimeout.Duration,
		SendTimeout:             cfg.Delivery.SendTimeout.Duration,
		SMTPConnectTimeout:      cfg.Delivery.SMTPConnectTimeout.Duration,
		GoogleClientID:          cfg.Integrations.Google.ClientID,
		LibreOfficeExecutable:   cfg.Integrations.LibreOffice.Executable,
		LibreOfficeBatchTimeout: cfg.Integrations.LibreOffice.BatchTimeout.Duration,
		TemporaryPaths:          temporarySpace.Paths,
	})
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}
	listenAddress := net.JoinHostPort(
		strings.Trim(strings.TrimSpace(cfg.Server.Address), "[]"),
		fmt.Sprintf("%d", cfg.Server.Port),
	)
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listenAddress, err)
	}

	localURL := "http://" + listener.Addr().String() + "/"
	fmt.Printf("Bulk Mail is running at %s\n", localURL)
	fmt.Printf("Local data: %s\n", paths.DataDir)
	slog.Info(
		"server started",
		"url",
		localURL,
		"max_concurrent_tasks",
		cfg.Tasks.MaxConcurrent,
		"max_queued_tasks",
		cfg.Tasks.MaxQueued,
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(listener)
	}()

	if cfg.Server.OpenBrowser {
		go func() {
			time.Sleep(250 * time.Millisecond)
			if err := local.OpenBrowser(localURL, runtime.GOOS); err != nil {
				slog.Warn("open browser", "error", err)
			}
		}()
	}

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("server stopped", "error", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Tasks.ShutdownTimeout.Duration)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func configureLogging(projectRoot string, cfg runtimeconfig.Logging) (func(), error) {
	level := new(slog.LevelVar)
	switch strings.ToLower(strings.TrimSpace(cfg.Level)) {
	case "debug":
		level.Set(slog.LevelDebug)
	case "info":
		level.Set(slog.LevelInfo)
	case "warn":
		level.Set(slog.LevelWarn)
	case "error":
		level.Set(slog.LevelError)
	}

	writer := io.Writer(os.Stderr)
	closeLog := func() {}
	if strings.TrimSpace(cfg.File) != "" {
		path := strings.TrimSpace(cfg.File)
		if !filepath.IsAbs(path) {
			path = filepath.Join(projectRoot, path)
		}
		path, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		if err != nil {
			return nil, err
		}
		writer = io.MultiWriter(os.Stderr, file)
		closeLog = func() { _ = file.Close() }
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: level})))
	return closeLog, nil
}
