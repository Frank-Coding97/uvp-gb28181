package play

// SetGenerationFloor is called before runtime publication and recovery so a
// newly allocated generation cannot equal a recorder's persisted generation.
// It never lowers a running service's counter.
func (s *Service) SetGenerationFloor(floor uint64) {
	for {
		current := s.nextGeneration.Load()
		if current >= floor || s.nextGeneration.CompareAndSwap(current, floor) {
			return
		}
	}
}
