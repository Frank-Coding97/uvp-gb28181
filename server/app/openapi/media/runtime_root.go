package media

import (
	"context"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type RuntimePlayVerifier interface {
	AuthenticateOpenAPI(string) (playauth.OpenAPIClaims, error)
}

type RuntimeViewerBinder interface {
	BindViewer(context.Context, string, playauth.OpenAPIViewerBindRequest) (models.Viewer, error)
}

type RuntimeFlowObserver interface {
	ObserveFlow(context.Context, playauth.OpenAPIFlowReport) error
}

// RuntimeRoot is the process-lifetime OpenAPI media boundary. It is published
// once so Gateway and Hooks cannot observe different signer/grant generations.
// SIP reload replaces only the LivePlayerRuntime behind its fixed dispatcher.
type RuntimeRoot struct {
	mu         sync.RWMutex
	dispatcher auth.MediaDispatcher
	verifier   RuntimePlayVerifier
	binder     RuntimeViewerBinder
	observer   RuntimeFlowObserver
	validator  play.QualifiedNodeValidator
}

var _ auth.MediaDispatcher = (*RuntimeRoot)(nil)

func NewRuntimeRoot() *RuntimeRoot { return &RuntimeRoot{} }

func (r *RuntimeRoot) Publish(
	dispatcher auth.MediaDispatcher,
	verifier RuntimePlayVerifier,
	binder RuntimeViewerBinder,
	observer RuntimeFlowObserver,
	validator play.QualifiedNodeValidator,
) error {
	if r == nil || interfaceIsNil(dispatcher) || !dispatcher.Ready() || interfaceIsNil(verifier) ||
		interfaceIsNil(binder) || interfaceIsNil(observer) || interfaceIsNil(validator) {
		return ErrLiveApplicationUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dispatcher != nil || r.verifier != nil || r.binder != nil || r.observer != nil || r.validator != nil {
		return ErrLiveApplicationUnavailable
	}
	r.dispatcher = dispatcher
	r.verifier = verifier
	r.binder = binder
	r.observer = observer
	r.validator = validator
	return nil
}

func (r *RuntimeRoot) Ready() bool {
	dispatcher := r.mediaDispatcher()
	return dispatcher != nil && dispatcher.Ready()
}

func (r *RuntimeRoot) Prepare(ctx context.Context, target auth.MediaTarget) (auth.MediaTicket, error) {
	dispatcher := r.mediaDispatcher()
	if dispatcher == nil || !dispatcher.Ready() {
		return "", ErrLiveApplicationUnavailable
	}
	return dispatcher.Prepare(ctx, target)
}

func (r *RuntimeRoot) Apply(ctx context.Context, request auth.MediaAdmittedRequest) (auth.MediaAuthorization, error) {
	dispatcher := r.mediaDispatcher()
	if dispatcher == nil || !dispatcher.Ready() {
		return auth.MediaAuthorization{}, ErrLiveApplicationUnavailable
	}
	return dispatcher.Apply(ctx, request)
}

func (r *RuntimeRoot) AuthenticateOpenAPI(token string) (playauth.OpenAPIClaims, error) {
	if r == nil || !r.Ready() {
		return playauth.OpenAPIClaims{}, playauth.ErrOpenAPIGrantUnavailable
	}
	r.mu.RLock()
	verifier := r.verifier
	r.mu.RUnlock()
	if interfaceIsNil(verifier) {
		return playauth.OpenAPIClaims{}, playauth.ErrOpenAPIGrantUnavailable
	}
	return verifier.AuthenticateOpenAPI(token)
}

func (r *RuntimeRoot) BindViewer(ctx context.Context, token string, request playauth.OpenAPIViewerBindRequest) (models.Viewer, error) {
	if r == nil || !r.Ready() {
		return models.Viewer{}, playauth.ErrOpenAPIGrantUnavailable
	}
	r.mu.RLock()
	binder := r.binder
	r.mu.RUnlock()
	if interfaceIsNil(binder) {
		return models.Viewer{}, playauth.ErrOpenAPIGrantUnavailable
	}
	return binder.BindViewer(ctx, token, request)
}

func (r *RuntimeRoot) ObserveFlow(ctx context.Context, report playauth.OpenAPIFlowReport) error {
	if r == nil {
		return playauth.ErrOpenAPIGrantUnavailable
	}
	r.mu.RLock()
	observer := r.observer
	r.mu.RUnlock()
	if interfaceIsNil(observer) {
		return playauth.ErrOpenAPIGrantUnavailable
	}
	return observer.ObserveFlow(ctx, report)
}

func (r *RuntimeRoot) QualifiedNodeValidator() play.QualifiedNodeValidator {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.validator
}

func (r *RuntimeRoot) mediaDispatcher() auth.MediaDispatcher {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dispatcher
}
