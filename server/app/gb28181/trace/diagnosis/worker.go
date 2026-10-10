package diagnosis

import "time"

// maxFlushRetries 批写失败的最大重试次数:超过后按有界丢弃策略处理
const maxFlushRetries = 5

func (service *Service) runWorker() {
	defer close(service.done)
	ticker := time.NewTicker(service.flushInterval)
	defer ticker.Stop()
	batch := make([]Event, 0, service.batchSize)
	failedAttempts := 0
	for {
		select {
		case event, ok := <-service.queue:
			if !ok {
				service.flush(batch)
				return
			}
			batch = append(batch, event)
			if len(batch) >= service.batchSize {
				batch, failedAttempts = service.flushBatch(batch, failedAttempts)
			}
		case <-ticker.C:
			batch, failedAttempts = service.flushBatch(batch, failedAttempts)
		case <-service.ctx.Done():
			service.health.droppedEvent("diagnosis worker canceled", uint64(len(batch)+len(service.queue)))
			return
		}
	}
}

// flushBatch 尝试写出一批事件。失败时保留 batch 供重试;重试超过上限
// 或 batch 超硬上限时按丢弃策略处理并计数,不让诊断盲区永久化
func (service *Service) flushBatch(batch []Event, failedAttempts int) ([]Event, int) {
	if len(batch) == 0 {
		return batch, 0
	}
	if service.flush(batch) {
		return batch[:0], 0
	}
	failedAttempts++
	if failedAttempts >= maxFlushRetries || len(batch) > service.batchSize*2 {
		service.health.droppedEvent("diagnosis batch dropped after failed retries", uint64(len(batch)))
		return batch[:0], 0
	}
	return batch, failedAttempts
}

func (service *Service) flush(events []Event) bool {
	if len(events) == 0 {
		return true
	}
	records := make([]Record, len(events))
	for i := range events {
		records[i] = recordFromEvent(events[i])
	}
	if err := service.repository.UpsertBatch(service.ctx, records); err != nil {
		service.health.failedBatch(err, uint64(len(records)))
		return false
	}
	service.health.succeeded(service.now())
	return true
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
