package main

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/app/gb28181"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/ginhelper"
	"uvplatform.cn/uvp-gb28181/bootstrap"
	"uvplatform.cn/uvp-gb28181/internal/standalone/bootstrapcredential"
	"uvplatform.cn/uvp-gb28181/internal/standalone/installation"
	"uvplatform.cn/uvp-gb28181/internal/standalone/installationhttp"
)

type standaloneSetup struct {
	handler *installationhttp.Handler
	store   *installation.Store
	mu      sync.Mutex
	started bool
}

func prepareStandaloneSetup(engine *gin.Engine) (*standaloneSetup, error) {
	store := installation.NewStore(app.DB())
	state, err := store.State(context.Background())
	if err != nil {
		return nil, errors.New("standalone installation state unavailable")
	}
	token, err := bootstrapcredential.Read(os.Stdin)
	if err != nil {
		return nil, errors.New("standalone bootstrap credential unavailable")
	}
	verifier, err := bootstrapcredential.NewVerifier(token)
	token = ""
	if err != nil {
		return nil, err
	}
	origin, err := standaloneSetupOrigin(app.ConfigYml.GetString("httpserver.port"))
	if err != nil {
		return nil, err
	}
	s := &standaloneSetup{store: store}
	s.handler, err = installationhttp.New(origin, string(state.Phase), verifier,
		func(ctx context.Context, username, password string) error {
			if !validStandaloneAdmin(username, password, app.ConfigYml.GetInt("safe.minpasswordlength"), app.ConfigYml.GetBool("safe.requirespecialchar")) {
				return installationhttp.ErrInvalidInput
			}
			_, err := store.CreateAdmin(ctx, username, password)
			return err
		},
		func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			// The first-install process has no periodic policy loader or admitted
			// business requests. Publish the committed policy before allowing login.
			if app.CasbinV2 == nil || app.CasbinV2.GetEnforcer() == nil {
				return errors.New("authorization unavailable")
			}
			return app.CasbinV2.GetEnforcer().LoadPolicy()
		})
	if err != nil {
		return nil, err
	}
	gate := ginhelper.InstallationGate(s.handler.AllowedPhase)
	engine.Use(func(c *gin.Context) {
		c.Set("standalone.installation_phase", s.handler.Phase())
		c.Set("standalone.credential_accepted", s.handler.CredentialAccepted())
		c.Set("standalone.installation_ready", s.handler.Ready())
		gate(c)
	})
	// Register before operation logging: credentials and passwords never enter
	// the generic business request recorder.
	s.handler.Register(engine)
	controller := gbcontrollers.NewSetupController(app.DB(), gb28181.SIPRuntimeStatus(), nil, s.activate)
	controller.SetConfigSaver(s.saveSIP)
	gbroutes.SetSetupController(controller)
	if state.Phase != installation.PhaseComplete {
		gb28181.SIPRuntimeStatus().MarkUnconfigured()
	}
	return s, nil
}

func (s *standaloneSetup) saveSIP(ctx context.Context, request gbsetup.SaveSIPConfigRequest) (gbsetup.SIPConfigView, error) {
	state, err := s.store.State(ctx)
	if err != nil {
		return gbsetup.SIPConfigView{}, err
	}
	if state.Phase == installation.PhaseComplete {
		// A previous commit may have succeeded even if activation or the response
		// failed. Retrying must use the normal settings path, never recreate users.
		return gbsetup.NewSIPConfigService(app.DB()).Save(ctx, request)
	}
	return s.store.CompleteSIP(ctx, request)
}

func (s *standaloneSetup) activate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.store.State(context.Background())
	if err != nil || state.Phase != installation.PhaseComplete {
		return errors.New("installation has not completed")
	}
	if s.started {
		return gb28181.ReloadSIP()
	}
	if err := bootstrap.StartRuntimeJobs(); err != nil {
		return err
	}
	gb28181.Start()
	s.started = true
	if err := s.handler.SetPhase("complete"); err != nil {
		return err
	}
	if gb28181.SIPRuntimeStatus().Snapshot().State == gbsetup.RuntimeFailed {
		return errors.New("SIP activation failed; inspect runtime status")
	}
	return nil
}
