package diagnosis

import "time"

func (service *Service) runWorker() {
	defer close(service.done)
	ticker := time.NewTicker(service.flushInterval)
	defer ticker.Stop()
	batch := make([]Event, 0, service.batchSize)
	for {
		select {
		case event, ok := <-service.queue:
			if !ok {
				service.flush(batch)
				return
			}
			batch = append(batch, event)
			if len(batch) >= service.batchSize {
				service.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			service.flush(batch)
			batch = batch[:0]
		case <-service.ctx.Done():
			service.health.droppedEvent("diagnosis worker canceled", uint64(len(batch)+len(service.queue)))
			return
		}
	}
}

func (service *Service) flush(events []Event) {
	if len(events) == 0 {
		return
	}
	records := make([]Record, len(events))
	for i := range events {
		records[i] = recordFromEvent(events[i])
	}
	if err := service.repository.UpsertBatch(service.ctx, records); err != nil {
		service.health.failedBatch(err, uint64(len(records)))
		return
	}
	service.health.succeeded(service.now())
}

func recordFromEvent(event Event) Record {
	return Record{
		SessionDay: dayUTC(event.ObservedAt), ObservedAt: event.ObservedAt.UTC(),
		CorrelationKey: event.CorrelationKey, State: event.State, Category: event.Category,
		Code: event.Code, Stage: event.Stage, Source: event.Source,
		DeviceID: event.DeviceID, ChannelID: event.ChannelID, CallID: event.CallID,
		CSeq: event.CSeq, Method: event.Method, StatusCode: event.StatusCode,
		StreamID: event.StreamID, ResolvedAt: cloneUTC(event.ResolvedAt),
	}
}
