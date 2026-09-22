package gb28181

import (
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
	openapimedia "uvplatform.cn/uvp-gb28181/app/openapi/media"
	openapiptz "uvplatform.cn/uvp-gb28181/app/openapi/ptz"
)

// This facade is process-lifetime. Reload changes its admitted generation only
// after the previous generation's actual OpenAPI calls have returned.
var openAPILivePlayer = openapimedia.NewLivePlayerRuntime()
var openAPIMediaRoot = openapimedia.NewRuntimeRoot()
var openAPIPTZRoot = openapiptz.NewRuntimeRoot()

func OpenAPILivePlayer() *openapimedia.LivePlayerRuntime { return openAPILivePlayer }

func OpenAPIMediaRuntime() *openapimedia.RuntimeRoot { return openAPIMediaRoot }

func OpenAPIPTZRuntime() *openapiptz.RuntimeRoot { return openAPIPTZRoot }

type openAPIMediaRootCandidate struct {
	provider   *openapimedia.NodeQualificationProvider
	dispatcher *openapimedia.GatewayDispatcher
	verifier   *playauth.Signer
	grants     *playauth.OpenAPIGrantService
}

func prepareOpenAPIMediaRoot(db *gorm.DB, signer *playauth.Signer) (*openAPIMediaRootCandidate, play.QualifiedNodeValidator, error) {
	if validator := openAPIMediaRoot.QualifiedNodeValidator(); validator != nil {
		return nil, validator, nil
	}
	bindings, err := LoadStartupOpenAPIControlBindingsOnce()
	if err != nil || db == nil || signer == nil || zlmRegistry == nil || bindings == nil {
		return nil, nil, openapimedia.ErrLiveApplicationUnavailable
	}
	provider := openapimedia.NewNodeQualificationProvider(zlmRegistry, bindings, openapiconfig.NewNodeRuntimeStore(db, time.Now))
	grants, err := playauth.NewOpenAPIGrantService(db, signer, openapimedia.NewNodeAuthority(), time.Now)
	if err != nil {
		return nil, nil, openapimedia.ErrLiveApplicationUnavailable
	}
	application := openapimedia.NewLiveApplication(provider, openAPILivePlayer, grants, true)
	candidate := &openAPIMediaRootCandidate{
		provider:   provider,
		dispatcher: openapimedia.NewGatewayDispatcher(application),
		verifier:   signer,
		grants:     grants,
	}
	return candidate, provider.PlayValidator(), nil
}

func publishOpenAPIMediaRoot(candidate *openAPIMediaRootCandidate) error {
	if candidate == nil {
		return nil
	}
	// Hooks see this still-unready root before Gateway admission opens. Publish
	// then makes both boundaries observe the same complete dependency bundle.
	gbroutes.SetOpenAPIMediaAuthorization(openAPIMediaRoot, openAPIMediaRoot, openAPIMediaRoot)
	if err := openAPIMediaRoot.Publish(candidate.dispatcher, candidate.verifier, candidate.grants, candidate.grants, candidate.provider.PlayValidator()); err != nil {
		return err
	}
	return nil
}
