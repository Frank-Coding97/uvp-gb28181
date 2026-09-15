package trace

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/trace/diagnosis"
)

type diagnosisRuntimeRepository struct {
	pruneCutoff time.Time
}

func (*diagnosisRuntimeRepository) UpsertBatch(context.Context, []diagnosis.Record) error { return nil }
func (*diagnosisRuntimeRepository) Query(context.Context, diagnosis.DiagnosisFilter) ([]diagnosis.Record, error) {
	return nil, nil
}
func (*diagnosisRuntimeRepository) FindByEvidence(context.Context, time.Time, time.Time, string, uint32) ([]diagnosis.Record, error) {
	return nil, nil
}
func (repository *diagnosisRuntimeRepository) Prune(_ context.Context, cutoff time.Time, _ int) (int64, error) {
	repository.pruneCutoff = cutoff
	return 1, nil
}

func TestDiagnosisHealthMergedAndSinkExposed(t *testing.T) {
	repository := &diagnosisRuntimeRepository{}
	service, err := diagnosis.NewService(repository, diagnosis.ServiceConfig{QueueCapacity: 4, BatchSize: 1})
	require.NoError(t, err)
	module := NewModuleWithDiagnosis(gbconfig.TraceConfig{QueueCapacity: 4, RetentionDays: 7}, &recordingStore{}, testPayloadCipher(), service)

	health := module.Health()
	require.Equal(t, diagnosis.HealthReady, health.Diagnosis.State)
	require.IsType(t, service, module.DiagnosticSink())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, module.Shutdown(ctx))
}

func TestDiagnosisHealthDisabledWithoutService(t *testing.T) {
	module := NewModule(gbconfig.TraceConfig{QueueCapacity: 1}, &recordingStore{}, testPayloadCipher())
	require.Equal(t, diagnosis.HealthDisabled, module.Health().Diagnosis.State)
	require.IsType(t, diagnosis.NoopSink{}, DiagnosisSinkFromRuntime(module))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, module.Shutdown(ctx))
}
