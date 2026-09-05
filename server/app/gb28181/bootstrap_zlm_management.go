package gb28181

import (
	"go.uber.org/zap"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	gbzlmmanagement "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	gbzlmrepo "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
	gbzlmsvc "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// zlmManagementCoreRuntime owns process-wide dependencies that do not depend
// on an active SIP UAC. Business adapters are rebuilt separately across SIP
// reloads so no controller retains a stopped playback/talk/recording runtime.
type zlmManagementCoreRuntime struct {
	registry        *node.Registry
	executor        *gbzlmmanagement.NodeExecutor
	runtime         *gbzlmmanagement.RuntimeReader
	ledger          *gbzlmrepo.ManagedResourceRepo
	restart         *gbzlmsvc.RestartCoordinator
	overview        *gbzlmmanagement.OverviewSampler
	ffmpegTemplates gbzlmmanagement.FFmpegTemplateRegistry
}

type zlmManagementBusinessRuntime struct {
	liveSessions           gbzlmmanagement.LiveSessionReader
	liveLocations          gbzlmmanagement.LiveLocationReader
	playbackSessions       gbzlmmanagement.DevicePlaybackReader
	talkSessions           gbzlmmanagement.TalkSessionReader
	recordingSessions      gbzlmmanagement.RecordingSessionReader
	recordingPlanLeases    gbzlmmanagement.LeasePresenceReader
	recordingChannels      gbzlmmanagement.GBChannelReader
	recordingSessionLookup gbzlmmanagement.RecordingSessionLookup
	recordingService       gbzlmmanagement.ExistingRecordingService
	recordingLocation      gbzlmmanagement.GBStreamLocation
}

var zlmManagementCore *zlmManagementCoreRuntime

func setupZLMManagementCore(nodeService *gbzlmsvc.NodeService, restart *gbzlmsvc.RestartCoordinator) {
	clearZLMManagementController()
	if zlmManagementCore != nil {
		if zlmManagementCore.overview != nil {
			zlmManagementCore.overview.Close()
		}
		if zlmManagementCore.restart != nil && zlmManagementCore.restart != restart {
			zlmManagementCore.restart.Close()
		}
	}
	zlmManagementCore = nil
	if zlmRegistry == nil || nodeService == nil || restart == nil {
		return
	}

	executor := gbzlmmanagement.NewNodeExecutor(zlmRegistry, zlm.NewClientForNode)
	runtime := gbzlmmanagement.NewRuntimeReader(executor)
	ledger := gbzlmrepo.NewManagedResourceRepo(app.DB())
	overview := gbzlmmanagement.NewOverviewSampler(
		gbzlmmanagement.NewOverviewService(gbzlmmanagement.OverviewDependencies{
			Registry: zlmRegistry,
			Runtime:  runtime,
			Media:    runtime,
		}),
		app.Cache,
	)
	overview.Start(func(err error) {
		if app.ZapLog != nil {
			app.ZapLog.Warn("ZLM 媒体实时速率采样失败", zap.Error(err))
		}
	})
	zlmManagementCore = &zlmManagementCoreRuntime{
		registry: zlmRegistry, executor: executor,
		runtime: runtime, ledger: ledger, restart: restart, overview: overview,
		// No production allow-list source exists yet. An explicit empty set
		// keeps FFmpeg listing available while create rejects every template
		// instead of accepting arbitrary command text.
		ffmpegTemplates: gbzlmmanagement.NewFFmpegTemplateSet(),
	}
	nodeService.SetNodeImpactProvider(gbzlmmanagement.NewRuntimeNodeImpactProvider(
		gbzlmmanagement.NewExecutorNodeImpactSnapshotReader(executor),
	))
	installZLMManagementController()
}

func currentZLMManagementBusinessRuntime() zlmManagementBusinessRuntime {
	return zlmManagementBusinessRuntime{
		liveSessions:           playSvc,
		liveLocations:          zlmLocationMap,
		playbackSessions:       playbackRegistry,
		talkSessions:           talkRepo,
		recordingSessions:      recordingRepo,
		recordingPlanLeases:    recordingPlanLeases,
		recordingChannels:      recordingRepo,
		recordingSessionLookup: recordingRepo,
		recordingService:       recordingSvc,
		recordingLocation:      zlmLocationMap,
	}
}

func installZLMManagementController() {
	bundle := newZLMManagementBundle(zlmManagementCore, currentZLMManagementBusinessRuntime())
	if bundle == nil {
		clearZLMManagementController()
		return
	}
	gbroutes.SetZLMManagementController(gbcontrollers.NewZLMManagementController(bundle))
	gbroutes.SetHomeDashboardOverview(bundle.Overview)
}

func clearZLMManagementController() {
	gbroutes.SetZLMManagementController(nil)
	gbroutes.SetHomeDashboardOverview(nil)
}

func teardownZLMManagementCore() {
	clearZLMManagementController()
	gbroutes.SetRestartStartedNotifier(nil)
	if zlmManagementCore != nil && zlmManagementCore.overview != nil {
		zlmManagementCore.overview.Close()
	}
	if zlmManagementCore != nil && zlmManagementCore.restart != nil {
		zlmManagementCore.restart.Close()
	}
	zlmManagementCore = nil
}

func newZLMManagementBundle(core *zlmManagementCoreRuntime, business zlmManagementBusinessRuntime) *gbcontrollers.ZLMManagementBundle {
	if core == nil || core.registry == nil || core.executor == nil || core.runtime == nil {
		return nil
	}

	sources := make([]gbzlmmanagement.OwnershipSource, 0, 6)
	if core.ledger != nil {
		sources = append(sources, gbzlmmanagement.NewManagedResourceOwnershipAdapter(core.ledger))
	}
	if business.liveSessions != nil {
		sources = append(sources, gbzlmmanagement.NewLiveOwnershipAdapter(business.liveSessions, business.liveLocations))
	}
	if business.playbackSessions != nil {
		sources = append(sources, gbzlmmanagement.NewDevicePlaybackOwnershipAdapter(business.playbackSessions))
	}
	if business.talkSessions != nil {
		// GbTalkSession does not persist vhost. Passing no claimed vhost keeps
		// matching talk evidence visible but intentionally uncertain/fail-closed.
		sources = append(sources, gbzlmmanagement.NewTalkOwnershipAdapter(business.talkSessions, ""))
	}
	if business.recordingSessions != nil {
		sources = append(sources, gbzlmmanagement.NewRecordingSessionOwnershipAdapter(business.recordingSessions))
	}
	if business.recordingPlanLeases != nil {
		sources = append(sources, gbzlmmanagement.NewRecordingPlanOwnershipAdapter(business.recordingPlanLeases))
	}
	ownership := gbzlmmanagement.NewOwnershipResolver(gbzlmmanagement.OwnershipDependencies{
		Presence: gbzlmmanagement.NewRuntimePresenceReader(core.runtime),
		Sources:  sources,
	})

	bundle := &gbcontrollers.ZLMManagementBundle{
		Overview: core.overview,
		Streams: gbzlmmanagement.NewStreamService(gbzlmmanagement.StreamServiceDependencies{
			Registry:     core.registry,
			Runtime:      core.runtime,
			Fresh:        nil,
			NodeExecutor: core.executor,
			Ownership:    ownership,
		}),
		Sessions: gbzlmmanagement.NewSessionService(gbzlmmanagement.SessionServiceDependencies{
			Registry:     core.registry,
			Runtime:      core.runtime,
			Fresh:        nil,
			NodeExecutor: core.executor,
		}),
	}

	if core.ledger != nil {
		proxyExecutor := gbzlmmanagement.NewNodeProxyExecutor(core.executor)
		ingressCapabilities := gbzlmmanagement.NewIngressCapabilityReader(core.runtime)
		bundle.Proxies = gbzlmmanagement.NewProxyService(gbzlmmanagement.ProxyDependencies{
			Executor: proxyExecutor, Ledger: core.ledger, Ownership: ownership, Capability: core.runtime,
		})

		managedOnly := []gbzlmmanagement.OwnershipSource{
			gbzlmmanagement.NewManagedResourceOwnershipAdapter(core.ledger),
		}
		ffmpegClient := gbzlmmanagement.NewFFmpegNodeClientAdapter(core.executor)
		ffmpegOwnership := gbzlmmanagement.NewOwnershipResolver(gbzlmmanagement.OwnershipDependencies{
			Presence: gbzlmmanagement.NewFFmpegPresenceReader(ffmpegClient), Sources: managedOnly,
		})
		templates := core.ffmpegTemplates
		if templates == nil {
			templates = gbzlmmanagement.NewFFmpegTemplateSet()
		}
		bundle.FFmpeg = gbzlmmanagement.NewFFmpegService(gbzlmmanagement.FFmpegDependencies{
			Client: ffmpegClient, Templates: templates, Ledger: core.ledger, Ownership: ffmpegOwnership,
			Capability: ingressCapabilities,
		})

		rtpClient := gbzlmmanagement.NewNodeRTPClientAdapter(core.executor)
		rtpOwnership := gbzlmmanagement.NewOwnershipResolver(gbzlmmanagement.OwnershipDependencies{
			Presence: gbzlmmanagement.NewRTPPresenceReader(rtpClient), Sources: managedOnly,
		})
		bundle.RTP = gbzlmmanagement.NewRTPService(gbzlmmanagement.RTPDependencies{
			Client: rtpClient, Ledger: core.ledger, Ownership: rtpOwnership,
			Capability: ingressCapabilities,
		})
	}

	if business.recordingService != nil && business.recordingChannels != nil && business.recordingLocation != nil {
		bundle.Recording = gbzlmmanagement.NewRecordingOps(gbzlmmanagement.RecordingOpsConfig{
			Executor:                 core.executor,
			Resolver:                 ownership,
			ChannelResolver:          gbzlmmanagement.NewGBChannelResolver(business.recordingChannels, business.recordingLocation),
			ExistingRecordingService: business.recordingService,
			SessionLookup:            business.recordingSessionLookup,
		})
	}
	return bundle
}
