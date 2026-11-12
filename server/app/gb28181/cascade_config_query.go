package gb28181

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadeConfigQuery struct {
	XMLName    xml.Name `xml:"Query"`
	CmdType    string   `xml:"CmdType"`
	SN         int      `xml:"SN"`
	DeviceID   string   `xml:"DeviceID"`
	ConfigType string   `xml:"ConfigType"`
}

type cascadeConfigQueryResult struct {
	body      []byte
	authorize func(context.Context) error
}

type cascadeConfigQueryFunc func(context.Context, cascadeDeviceStatusTarget, string, string) (cascadeConfigQueryResult, error)

func newCascadeConfigQueryHandler(store *repository.GormRepository, clients *cascadePlatformClientFactory) func(*sip.Request, sip.ServerTransaction) bool {
	handler, _ := newCascadeConfigQueryHandlerWithQuery(store, clients, queryCascadeConfig)
	return handler
}

func newCascadeConfigQueryHandlerWithQuery(store *repository.GormRepository, clients *cascadePlatformClientFactory, execute cascadeConfigQueryFunc) (func(*sip.Request, sip.ServerTransaction) bool, func()) {
	var running sync.WaitGroup
	handler := func(req *sip.Request, tx sip.ServerTransaction) bool {
		var query cascadeConfigQuery
		if manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), req.Body(), &query) != nil || query.CmdType != manscdp.CmdConfigDownload {
			return false
		}
		reject := func(code int, reason string) bool {
			_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		platforms, err := store.ListPlatforms(ctx)
		if err != nil {
			return reject(503, "Configuration query unavailable")
		}
		platform, ambiguous := findCascadeControlPeer(req, platforms)
		if platform == nil && !ambiguous {
			return false
		}
		if ambiguous || platform == nil || !platform.Enabled {
			return reject(403, "Configuration query upstream rejected")
		}
		types, err := manscdp.NormalizeCascadeConfigTypes(strings.Split(query.ConfigType, "/"))
		if err != nil || query.SN <= 0 || !validCascadeGBID(query.DeviceID) {
			return reject(400, "Invalid configuration query")
		}
		toID := ""
		if req.To() != nil {
			toID = req.To().Address.User
		}
		if !cascadeDeviceStatusRequestTargetsMatch(platform.LocalDeviceID, query.DeviceID, toID, req.Recipient.User) {
			return reject(403, "Configuration query target mismatch")
		}
		projection, err := store.ProjectionSnapshot(ctx, platform.ID)
		if err != nil || projection == nil || !projection.Platform.Enabled || projection.Platform.ConfigRevision != platform.ConfigRevision || projection.Platform.ProjectionRevision != platform.ProjectionRevision {
			return reject(403, "Configuration query authorization changed")
		}
		publication, err := buildCascadeCatalogSnapshot(ctx, store, &projection.Platform)
		if err != nil || publication.Revision != projection.Revision {
			return reject(503, "Configuration query publication unavailable")
		}
		target, ok := resolveCascadeDeviceStatusTarget(*projection, publication, query.DeviceID)
		if !ok || target.IsPlatform {
			return reject(403, "Configuration query target is not published")
		}
		client, err := clients.NewClient(projection.Platform)
		if err != nil {
			return reject(503, "Configuration query response unavailable")
		}
		if tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)) != nil {
			return true
		}
		cseq := uint32(0)
		if req.CSeq() != nil {
			cseq = req.CSeq().SeqNo
		}
		key := strings.Replace(cascadeDeviceStatusIdempotencyKey(platform.ID, cascadeCallID(req), cseq, query.DeviceID), "cascade-device-status:", "cascade-config-query:", 1)
		snapshot, profile := projection.Platform, cascadeWireProfile(projection.Platform)
		running.Add(1)
		go func() {
			defer running.Done()
			queryCtx, queryCancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer queryCancel()
			if !cascadeDeviceStatusAuthorizationCurrent(queryCtx, store, snapshot) {
				return
			}
			// One operation per type resolves otherwise untyped empty replies,
			// and keeps BasicParam on its device scope. Reads may run together;
			// upstream responses remain serial and use the original SN.
			type responseResult struct {
				body      []byte
				err       error
				authorize func(context.Context) error
			}
			results := make([]responseResult, len(types))
			slots := make(chan struct{}, 4)
			var pending sync.WaitGroup
			for index, kind := range types {
				pending.Add(1)
				go func() {
					defer pending.Done()
					select {
					case slots <- struct{}{}:
					case <-queryCtx.Done():
						results[index].err = queryCtx.Err()
						return
					}
					defer func() { <-slots }()
					if !cascadeDeviceStatusAuthorizationCurrent(queryCtx, store, snapshot) {
						results[index].err = fmt.Errorf("configuration publication changed")
						return
					}
					source, err := execute(queryCtx, target, kind, key+":"+kind)
					body := source.body
					if err == nil {
						var parsed *manscdp.CascadeConfigReadResponse
						parsed, err = manscdp.ParseCascadeConfigReadResponse(body, manscdp.ConfigDownloadExpectation{}, kind)
						if err == nil {
							var routingAuthorize func(context.Context) error
							parsed, routingAuthorize, err = restoreCascadeSnapshotConfig(queryCtx, app.DB(), snapshot, target, parsed)
							if routingAuthorize != nil {
								sourceAuthorize := source.authorize
								source.authorize = func(ctx context.Context) error {
									if sourceAuthorize == nil {
										return fmt.Errorf("configuration source authorization missing")
									}
									if err := sourceAuthorize(ctx); err != nil {
										return err
									}
									return routingAuthorize(ctx)
								}
							}
						}
						if err == nil {
							body, err = parsed.Build(profile, query.DeviceID, query.SN)
						}
					}
					if err == nil && strings.EqualFold(snapshot.Transport, "UDP") && !client.(*cascadePlatformClient).transactions.MessageFitsUDP(body) {
						err = fmt.Errorf("complete configuration response exceeds UDP budget")
					}
					results[index] = responseResult{body, err, source.authorize}
				}()
			}
			pending.Wait()
			for index, result := range results {
				body := result.body
				sendCtx, sendCancel := context.WithTimeout(context.Background(), 3*time.Second)
				if result.err == nil {
					if result.authorize == nil {
						result.err = fmt.Errorf("configuration source authorization missing")
					} else {
						result.err = result.authorize(sendCtx)
					}
				}
				if result.err != nil {
					app.Log(queryCtx).Warn("级联配置读取失败", zap.String("event", "cascade.config.query_failed"), zap.Uint64("platform_id", snapshot.ID), zap.String("published_target_id", query.DeviceID), zap.String("config_type", types[index]), zap.Error(result.err))
					var err error
					body, err = manscdp.MarshalProfiledXML(profile, struct {
						XMLName  xml.Name `xml:"Response"`
						CmdType  string   `xml:"CmdType"`
						SN       int      `xml:"SN"`
						DeviceID string   `xml:"DeviceID"`
						Result   string   `xml:"Result"`
					}{CmdType: query.CmdType, SN: query.SN, DeviceID: query.DeviceID, Result: "ERROR"})
					if err != nil {
						sendCancel()
						return
					}
				}
				if !cascadeDeviceStatusAuthorizationCurrent(sendCtx, store, snapshot) {
					sendCancel()
					return
				}
				sent := client.(*cascadePlatformClient).transactions.SendMessage(sendCtx, body, fmt.Sprintf("config-query-%d-%d", snapshot.ID, time.Now().UnixNano()))
				sendCancel()
				if !sent.Success {
					return
				}
			}
		}()
		return true
	}
	return handler, running.Wait
}

func queryCascadeConfig(ctx context.Context, target cascadeDeviceStatusTarget, configType, key string) (cascadeConfigQueryResult, error) {
	service, db := ptzService, app.DB()
	if service == nil || db == nil {
		return cascadeConfigQueryResult{}, fmt.Errorf("configuration query runtime unavailable")
	}
	loader := control.NewGormTargetLoader(db)
	load := loader.Load
	if service.RequiresDeviceAuthorization() {
		load = loader.LoadAuthorized
	}
	loaded, err := load(ctx, target.SourceDeviceID, target.SourceChannelID)
	if err != nil {
		return cascadeConfigQueryResult{}, err
	}
	body, err := service.ConfigForCascade(ctx, loaded, configType, key)
	if err != nil {
		return cascadeConfigQueryResult{}, err
	}
	authorize := func(checkCtx context.Context) error {
		current, err := load(checkCtx, target.SourceDeviceID, target.SourceChannelID)
		if err != nil || current.DeviceEpoch != loaded.DeviceEpoch || current.DeviceCode != loaded.DeviceCode || current.ChannelCode != loaded.ChannelCode {
			return fmt.Errorf("configuration query source authorization changed")
		}
		return nil
	}
	if err := authorize(ctx); err != nil {
		return cascadeConfigQueryResult{}, err
	}
	return cascadeConfigQueryResult{body: body, authorize: authorize}, nil
}
