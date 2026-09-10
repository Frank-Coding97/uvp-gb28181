package play

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGenerationFloorNeverReusesPersistedGeneration(t *testing.T) {
	service := &Service{}
	service.SetGenerationFloor(41)
	require.Equal(t, uint64(42), service.nextGeneration.Add(1))
	service.SetGenerationFloor(7)
	require.Equal(t, uint64(43), service.nextGeneration.Add(1))
}
