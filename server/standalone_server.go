package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/app/gb28181"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/ginhelper"
	"uvplatform.cn/uvp-gb28181/internal/standalone/control"
	"uvplatform.cn/uvp-gb28181/internal/standalone/controlpipe"
)

// The launcher owns the process Job and coordinates ZLM between quiesce and
// finalize. Child process groups isolate Ctrl+C so only the launcher initiates
// this protocol. Closing the console or losing the launcher remains abnormal.
func runStandaloneServer(engine *gin.Engine, admission *ginhelper.StandaloneAdmission, setup *standaloneSetup) error {
	name, key, err := control.Endpoint(app.BasePath, "backend", app.ConfigYml.GetString("token.jwttokensignkey"))
	if err != nil {
		return err
	}
	pipe, err := controlpipe.Listen(name, key)
	if err != nil {
		return err
	}
	defer pipe.Close()
	listener, err := net.Listen("tcp", app.ConfigYml.GetString("httpserver.port"))
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{
		Handler:      engine,
		ReadTimeout:  time.Duration(app.ConfigYml.GetInt("httpserver.read_timeout")) * time.Second,
		WriteTimeout: time.Duration(app.ConfigYml.GetInt("httpserver.write_timeout")) * time.Second,
		IdleTimeout:  time.Duration(app.ConfigYml.GetInt("httpserver.idle_timeout")) * time.Second,
	}
	defer server.Close()
	if setup.handler.Phase() == "complete" {
		if err := setup.activate(); err != nil && !setup.started {
			return err
		}
	}
	ginhelper.PrintStartupBanner()
	httpDone := make(chan error, 1)
	go func() { httpDone <- server.Serve(listener) }()
	controlCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pipeDone := make(chan error, 1)
	phase := control.Ready
	go func() {
		pipeDone <- control.Serve(controlCtx, pipe, func(ctx context.Context, command control.Command) (control.Reply, error) {
			switch command {
			case control.Probe:
				return phase, nil
			case control.Quiesce:
				if phase == control.MediaReady {
					return phase, nil
				}
				if phase != control.Ready {
					return control.Failed, errors.New("backend shutdown phase invalid")
				}
				phase = control.Stopping
				admission.BeginDrain()
				err := admission.Wait(ctx)
				if err == nil {
					err = ginhelper.ShutdownScheduler(ctx)
				}
				if err == nil {
					err = gb28181.Quiesce(ctx)
				}
				if err != nil {
					phase = control.Failed
					return phase, err
				}
				phase = control.MediaReady
				return phase, nil
			case control.Finalize:
				if phase != control.MediaReady {
					return control.Failed, errors.New("backend has not quiesced")
				}
				// The caller has already waited for the exact ZLM process to exit. Leave
				// the recording indexer live until all arrived Hook requests finish.
				err := server.Shutdown(ctx)
				if err == nil {
					err = gb28181.StopContext(ctx)
				}
				if err == nil && app.CasbinV2 != nil {
					// Policy reload can still be reading SQLite after HTTP drains.
					// Join it before closing the shared database connection.
					if closer, ok := app.CasbinV2.(interface{ Shutdown(context.Context) error }); ok {
						err = closer.Shutdown(ctx)
					} else {
						err = errors.New("authorization policy loader cannot drain")
					}
				}
				if err == nil && app.Cache != nil {
					err = app.Cache.Close()
				}
				if err == nil && app.DB() != nil {
					db, dbErr := app.DB().DB()
					err = dbErr
					if err == nil {
						err = db.Close()
					}
				}
				if err != nil {
					phase = control.Failed
					return phase, err
				}
				phase = control.Finalized
				return phase, nil
			default:
				return control.Failed, errors.New("unsupported backend control command")
			}
		})
	}()
	for {
		select {
		case err := <-pipeDone:
			if err != nil {
				return err
			}
			if phase != control.Finalized {
				return errors.New("backend control listener closed before finalization")
			}
			return nil
		case err := <-httpDone:
			if !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			// HTTP Shutdown finishes inside the control handler; its reply and final
			// writes must complete before main exits, even though Serve returned first.
			httpDone = nil
		}
	}
}
