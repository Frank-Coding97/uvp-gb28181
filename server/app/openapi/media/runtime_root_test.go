package media

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type runtimeRootDispatcher struct {
	ready   bool
	prepare auth.MediaTicket
	applied auth.MediaAdmittedRequest
}

func (d *runtimeRootDispatcher) Ready() bool { return d != nil && d.ready }
func (d *runtimeRootDispatcher) Prepare(context.Context, auth.MediaTarget) (auth.MediaTicket, error) {
	if !d.Ready() {
		return "", ErrLiveApplicationUnavailable
	}
	return d.prepare, nil
}
func (d *runtimeRootDispatcher) Apply(_ context.Context, request auth.MediaAdmittedRequest) (auth.MediaAuthorization, error) {
	d.applied = request
	return auth.MediaAuthorization{AuthorizationID: "authorization-1"}, nil
}

type runtimeRootVerifier struct{ token string }

func (v *runtimeRootVerifier) AuthenticateOpenAPI(token string) (playauth.OpenAPIClaims, error) {
	v.token = token
	return playauth.OpenAPIClaims{}, nil
}

type runtimeRootGrantBoundary struct {
	bound playauth.OpenAPIViewerBindRequest
	flow  playauth.OpenAPIFlowReport
}

func (b *runtimeRootGrantBoundary) BindViewer(_ context.Context, _ string, request playauth.OpenAPIViewerBindRequest) (models.Viewer, error) {
	b.bound = request
	return models.Viewer{Identifier: request.Identifier}, nil
}
func (b *runtimeRootGrantBoundary) ObserveFlow(_ context.Context, report playauth.OpenAPIFlowReport) error {
	b.flow = report
	return nil
}

func TestOpenAPIRuntimeRootRejectsUntilCompleteBundlePublishedOnce(t *testing.T) {
	root := NewRuntimeRoot()
	require.False(t, root.Ready())
	_, err := root.Prepare(context.Background(), auth.MediaTarget{})
	require.ErrorIs(t, err, ErrLiveApplicationUnavailable)
	_, err = root.Apply(context.Background(), auth.MediaAdmittedRequest{})
	require.ErrorIs(t, err, ErrLiveApplicationUnavailable)
	_, err = root.AuthenticateOpenAPI("token-before-publish")
	require.ErrorIs(t, err, playauth.ErrOpenAPIGrantUnavailable)
	_, err = root.BindViewer(context.Background(), "token", playauth.OpenAPIViewerBindRequest{})
	require.ErrorIs(t, err, playauth.ErrOpenAPIGrantUnavailable)
	require.ErrorIs(t, root.ObserveFlow(context.Background(), playauth.OpenAPIFlowReport{}), playauth.ErrOpenAPIGrantUnavailable)
	require.Nil(t, root.QualifiedNodeValidator())

	dispatcher := &runtimeRootDispatcher{ready: true, prepare: "ticket-1"}
	verifier := &runtimeRootVerifier{}
	grants := &runtimeRootGrantBoundary{}
	validator := play.QualifiedNodeValidatorFunc(func(context.Context, play.Request, play.NodeQualificationSnapshot) error { return nil })
	require.NoError(t, root.Publish(dispatcher, verifier, grants, grants, validator))
	require.True(t, root.Ready())
	require.NotNil(t, root.QualifiedNodeValidator())

	ticket, err := root.Prepare(context.Background(), auth.MediaTarget{})
	require.NoError(t, err)
	require.Equal(t, auth.MediaTicket("ticket-1"), ticket)
	admitted := auth.MediaAdmittedRequest{GrantID: "grant-1", Ticket: ticket}
	_, err = root.Apply(context.Background(), admitted)
	require.NoError(t, err)
	require.Equal(t, admitted, dispatcher.applied)
	_, err = root.AuthenticateOpenAPI("same-root-token")
	require.NoError(t, err)
	require.Equal(t, "same-root-token", verifier.token)
	bind := playauth.OpenAPIViewerBindRequest{Identifier: "viewer-1"}
	_, err = root.BindViewer(context.Background(), "same-root-token", bind)
	require.NoError(t, err)
	require.Equal(t, bind, grants.bound)
	flow := playauth.OpenAPIFlowReport{Identifier: "viewer-1"}
	require.NoError(t, root.ObserveFlow(context.Background(), flow))
	require.Equal(t, flow, grants.flow)
	dispatcher.ready = false
	_, err = root.AuthenticateOpenAPI("token-during-reload")
	require.ErrorIs(t, err, playauth.ErrOpenAPIGrantUnavailable, "reload must reject reconnect with an already-issued token")
	_, err = root.BindViewer(context.Background(), "token-during-reload", bind)
	require.ErrorIs(t, err, playauth.ErrOpenAPIGrantUnavailable, "retirement between verification and binding must fail closed")
	require.NoError(t, root.ObserveFlow(context.Background(), flow), "final flow observations still settle existing viewers")
	dispatcher.ready = true

	require.ErrorIs(t, root.Publish(dispatcher, verifier, grants, grants, validator), ErrLiveApplicationUnavailable)
}

func TestOpenAPIRuntimeRootReadinessTracksLivePlayerRetirement(t *testing.T) {
	player := NewLivePlayerRuntime()
	application := NewLiveApplication(&qualificationFake{}, player, &grantFake{}, true)
	dispatcher := NewGatewayDispatcher(application)
	require.False(t, dispatcher.Ready(), "a configured facade without a published player must reject admission")
	require.NoError(t, player.Publish(&playerFake{}))
	require.True(t, dispatcher.Ready())
	require.NoError(t, player.Retire(context.Background()))
	require.False(t, dispatcher.Ready(), "reload retirement must close admission before replacement")
}

func TestOpenAPIRuntimeRootRejectsIncompleteOrUnreadyBundle(t *testing.T) {
	root := NewRuntimeRoot()
	dispatcher := &runtimeRootDispatcher{ready: true}
	verifier := &runtimeRootVerifier{}
	grants := &runtimeRootGrantBoundary{}
	validator := play.QualifiedNodeValidatorFunc(func(context.Context, play.Request, play.NodeQualificationSnapshot) error { return nil })
	for name, publish := range map[string]func() error{
		"dispatcher": func() error { return root.Publish(nil, verifier, grants, grants, validator) },
		"unready":    func() error { return root.Publish(&runtimeRootDispatcher{}, verifier, grants, grants, validator) },
		"verifier":   func() error { return root.Publish(dispatcher, nil, grants, grants, validator) },
		"binder":     func() error { return root.Publish(dispatcher, verifier, nil, grants, validator) },
		"observer":   func() error { return root.Publish(dispatcher, verifier, grants, nil, validator) },
		"validator":  func() error { return root.Publish(dispatcher, verifier, grants, grants, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, publish(), ErrLiveApplicationUnavailable)
			require.False(t, root.Ready())
		})
	}
}
