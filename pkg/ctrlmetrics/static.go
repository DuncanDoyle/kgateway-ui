package ctrlmetrics

// Static is a Source over fixed results, one per install ID, for a caller that
// collects once with [Collector.Collect] and computes over it (D-053). It
// never ages a result: the engine reads one older than its MetricsMaxAge as
// stale, as it reads the Collector's. It never waits or collects again; when a
// later pass needs fresher metrics, the caller collects and replaces it.
type Static map[string]Result

// Get implements Source.
func (s Static) Get(id string) (Result, bool) {
	r, ok := s[id]
	return r, ok
}
