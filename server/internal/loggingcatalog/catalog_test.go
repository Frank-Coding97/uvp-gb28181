package loggingcatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// TestLoggingCatalogRegistry is the C09 gate. It walks the production tree and
// compares it with the reviewed registry, so a new event, a new field name, a
// non-snake_case key, or a dropped locating field fails here instead of
// accumulating quietly.
func TestLoggingCatalogRegistry(t *testing.T) {
	registry, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Events) < 300 || len(registry.Fields) < 100 {
		t.Fatalf("registry looks truncated: %d events, %d fields", len(registry.Events), len(registry.Fields))
	}
	findings, err := Findings(serverRoot(t), registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		t.Error(finding.String())
	}
}

// TestLoggingCatalogRegenerate rewrites registry.json from source. It is the
// deliberate half of the baseline: the gate never writes, so an addition only
// lands after someone runs this and reads the diff.
//
//	cd server && UVP_LOGGING_CATALOG_UPDATE=1 go test ./internal/loggingcatalog/ -run TestLoggingCatalogRegenerate -count=1
func TestLoggingCatalogRegenerate(t *testing.T) {
	if os.Getenv("UVP_LOGGING_CATALOG_UPDATE") != "1" {
		t.Skip("set UVP_LOGGING_CATALOG_UPDATE=1 to rewrite registry.json from source")
	}
	observed, err := Observe(serverRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	// A regeneration must never drop a reviewed locating_free_exceptions entry:
	// those carry a human judgement, and that list is only allowed to shrink by
	// editing it by hand.
	if previous, err := Embedded(); err == nil {
		for event, reason := range previous.LocatingFreeExceptions {
			if _, kept := observed.LocatingFreeExceptions[event]; !kept {
				observed.LocatingFreeExceptions[event] = reason
			}
		}
	}
	data, err := json.MarshalIndent(observed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath(t), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("rewrote %s: %d events, %d fields, %d exceptions",
		registryPath(t), len(observed.Events), len(observed.Fields), len(observed.LocatingFreeExceptions))
}

// TestLoggingCatalogMirrorsScanScript keeps the gate and the report tool on one
// definition of "carrying a locating field" and one definition of "the event
// name repeats its level". They are two implementations of the same idea on
// purpose - one is type-aware, the other carries the heuristics the team has
// been reading for months - so they have to be checked against each other
// instead of trusted to agree.
func TestLoggingCatalogMirrorsScanScript(t *testing.T) {
	path := filepath.Join(filepath.Dir(serverRoot(t)), "docs", "logging-governance", "scan-logging.py")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)

	block := between(script, "IDENT_BASE = {", "}")
	if block == "" {
		t.Fatalf("IDENT_BASE block not found in %s", path)
	}
	reported := make(map[string]bool)
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if index := strings.Index(line, "#"); index >= 0 {
			line = strings.TrimSpace(line[:index])
		}
		if !strings.HasPrefix(line, `"`) {
			continue
		}
		for _, match := range regexp.MustCompile(`"([A-Za-z_]+)"`).FindAllStringSubmatch(line, -1) {
			reported[match[1]] = true
		}
	}
	if len(reported) == 0 {
		t.Fatalf("IDENT_BASE parsed empty from %s", path)
	}
	for name := range locatingFields {
		if !reported[name] {
			t.Errorf("locatingFields has %q but scan-logging.py IDENT_BASE does not; keep both lists in step", name)
		}
	}
	for name := range reported {
		if !locatingFields[name] {
			t.Errorf("scan-logging.py IDENT_BASE has %q but locatingFields does not; keep both lists in step", name)
		}
	}

	echo := regexp.MustCompile(`EVENT_LEVEL_ECHO_RE = re\.compile\(r"\\\.\(([^)]*)\)\$"\)`).FindStringSubmatch(script)
	if echo == nil {
		t.Fatalf("EVENT_LEVEL_ECHO_RE not found in %s", path)
	}
	fromScript := make(map[string]bool)
	for _, name := range strings.Split(echo[1], "|") {
		fromScript[strings.TrimSpace(name)] = true
	}
	if len(fromScript) != len(levelEchoSegments) {
		t.Fatalf("level echo segments differ: script %v, gate %v", sortedKeys(fromScript), sortedKeys(levelEchoSegments))
	}
	for name := range fromScript {
		if !levelEchoSegments[name] {
			t.Errorf("scan-logging.py treats %q as a level echo but the gate does not", name)
		}
	}
}

// TestLoggingCatalogRules pins each rule with a fixture that isolates it. Every
// case carries its own minimal registry: locating_field_lost and
// stale_field_dictionary_entry are repo-wide rules, so a shared registry would
// let one fixture's leftovers fail another fixture.
func TestLoggingCatalogRules(t *testing.T) {
	twoSites := `package fixture

import "go.uber.org/zap"

func emitWith(logger *zap.Logger, deviceID string) {
	logger.Info("fixture", zap.String("event", "%s"), zap.String("device_id", deviceID))
}

func emitWithout(logger *zap.Logger) {
	logger.Info("fixture", zap.String("event", "%s"), zap.Bool("ok", true))
}`
	cases := []struct {
		name     string
		source   string
		registry Registry
		want     []FindingKind
	}{{
		name: "registered call site is clean",
		source: `package fixture

import "go.uber.org/zap"

func emit(logger *zap.Logger, deviceID string) {
	logger.Info("fixture", zap.String("event", "fixture.ok"), zap.String("device_id", deviceID))
}`,
		registry: registryWith(map[string][]string{"fixture.ok": {"device_id"}}, "event", "device_id"),
	}, {
		name: "unknown event",
		source: `package fixture

import "go.uber.org/zap"

func emit(logger *zap.Logger, deviceID string) {
	logger.Info("fixture", zap.String("event", "fixture.brand_new"), zap.String("device_id", deviceID))
}`,
		registry: registryWith(map[string][]string{}, "event", "device_id"),
		want:     []FindingKind{KindUnregisteredEvent},
	}, {
		name: "event named after its level",
		source: `package fixture

import "go.uber.org/zap"

func emit(logger *zap.Logger, deviceID string) {
	logger.Warn("fixture", zap.String("event", "fixture.upload.warn"), zap.String("device_id", deviceID))
}`,
		registry: registryWith(map[string][]string{}, "event", "device_id"),
		want:     []FindingKind{KindEventLevelEcho},
	}, {
		// The division of labour: an event value this package cannot read is
		// loggingcontract's dynamic_event finding (and, for the two reviewed
		// forwarding adapters, a SHA-sealed review). Reporting it here as well
		// would make one fact fail two gates.
		name: "event that no constant resolves is left to the type-aware gate",
		source: `package fixture

import "go.uber.org/zap"

func emit(logger *zap.Logger, name string) {
	logger.Info("fixture", zap.String("event", name))
}`,
		registry: registryWith(map[string][]string{}, "event"),
	}, {
		name: "event read through a package constant",
		source: `package fixture

import "go.uber.org/zap"

const fixtureEventOK = "fixture.ok"

func emit(logger *zap.Logger, deviceID string) {
	logger.Info("fixture", zap.String("event", fixtureEventOK), zap.String("device_id", deviceID))
}`,
		registry: registryWith(map[string][]string{"fixture.ok": {"device_id"}}, "event", "device_id"),
	}, {
		name: "field key is not snake_case",
		source: `package fixture

import "go.uber.org/zap"

func emit(logger *zap.Logger, deviceID string) {
	logger.Info("fixture", zap.String("event", "fixture.ok"), zap.String("deviceId", deviceID))
}`,
		registry: registryWith(map[string][]string{"fixture.ok": {"device_id"}}, "event"),
		want:     []FindingKind{KindFieldNameFormat},
	}, {
		name: "field key is not in the dictionary",
		source: `package fixture

import "go.uber.org/zap"

func emit(logger *zap.Logger, deviceCode string) {
	logger.Info("fixture", zap.String("event", "fixture.ok"), zap.String("device_id", deviceCode), zap.String("device_code", deviceCode))
}`,
		registry: registryWith(map[string][]string{"fixture.ok": {"device_id"}}, "event", "device_id"),
		want:     []FindingKind{KindFieldNotInDictionary},
	}, {
		name:     "new call site without a locating field",
		source:   strings.ReplaceAll(twoSites, "%s", "fixture.ok"),
		registry: registryWith(map[string][]string{"fixture.ok": {"device_id"}}, "event", "device_id", "ok"),
		want:     []FindingKind{KindLocatingMissing},
	}, {
		name:     "reviewed exception allows the locating-free branch",
		source:   strings.ReplaceAll(twoSites, "%s", "fixture.allowed"),
		registry: allowedRegistry("fixture.allowed"),
	}, {
		name: "event that carries no locating field anywhere is exempt",
		source: `package fixture

import "go.uber.org/zap"

func emit(logger *zap.Logger) {
	logger.Info("fixture", zap.String("event", "fixture.unlocated"), zap.Bool("ok", true))
}`,
		registry: registryWith(map[string][]string{"fixture.unlocated": {}}, "event", "ok"),
	}, {
		name: "fields assembled in a variable are out of reach",
		source: `package fixture

import "go.uber.org/zap"

func emit(logger *zap.Logger, deviceID string) {
	fields := []zap.Field{zap.String("event", "fixture.ok")}
	if deviceID != "" {
		fields = append(fields, zap.String("device_id", deviceID))
	}
	logger.Info("fixture", fields...)
}`,
		registry: registryWith(map[string][]string{}),
	}, {
		name: "locating field is gone from every call site",
		source: `package fixture

import "go.uber.org/zap"

func emit(logger *zap.Logger, streamID string) {
	logger.Info("fixture", zap.String("event", "fixture.ok"), zap.String("stream_id", streamID))
}`,
		registry: registryWith(map[string][]string{"fixture.ok": {"device_id"}}, "event", "stream_id"),
		want:     []FindingKind{KindLocatingLost},
	}, {
		name: "dictionary entry that nothing uses",
		source: `package fixture

import "go.uber.org/zap"

func emit(logger *zap.Logger, deviceID string) {
	logger.Info("fixture", zap.String("event", "fixture.ok"), zap.String("device_id", deviceID))
}`,
		registry: registryWith(map[string][]string{"fixture.ok": {"device_id"}}, "event", "device_id", "device_code"),
		want:     []FindingKind{KindStaleFieldEntry},
	}, {
		// C04 hit this first: play.reconcile.probe_failed was WARN in two
		// branches and DEBUG in a third. One name, two severities - any count
		// taken by event name is then a lie. The fix is to split the name.
		name: "one event logged at two levels",
		source: `package fixture

import "go.uber.org/zap"

func emitWarn(logger *zap.Logger, deviceID string) {
	logger.Warn("fixture", zap.String("event", "fixture.ok"), zap.String("device_id", deviceID))
}

func emitDebug(logger *zap.Logger, deviceID string) {
	logger.Debug("fixture", zap.String("event", "fixture.ok"), zap.String("device_id", deviceID))
}`,
		registry: registryWith(map[string][]string{"fixture.ok": {"device_id"}}, "event", "device_id"),
		want:     []FindingKind{KindEventLevelDivergence},
	}, {
		// The rule reports divergence, not repetition: the same level at every
		// call site is exactly what a stable event name looks like.
		name: "one event logged at one level across call sites is clean",
		source: `package fixture

import "go.uber.org/zap"

func emitFirst(logger *zap.Logger, deviceID string) {
	logger.Warn("fixture", zap.String("event", "fixture.ok"), zap.String("device_id", deviceID))
}

func emitSecond(logger *zap.Logger, deviceID string) {
	logger.Warn("fixture", zap.String("event", "fixture.ok"), zap.String("device_id", deviceID))
}`,
		registry: registryWith(map[string][]string{"fixture.ok": {"device_id"}}, "event", "device_id"),
	}}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			findings := findingsFor(t, testCase.source, testCase.registry)
			got := make([]FindingKind, 0, len(findings))
			for _, finding := range findings {
				got = append(got, finding.Kind)
			}
			want := append([]FindingKind{}, testCase.want...)
			sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
			sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
			if len(got) != len(want) {
				t.Fatalf("findings = %v, want %v", findings, want)
			}
			for index := range got {
				if got[index] != want[index] {
					t.Fatalf("findings = %v, want %v", findings, want)
				}
			}
		})
	}
}

func registryWith(events map[string][]string, fields ...string) Registry {
	return Registry{
		Events:                 events,
		LocatingFreeExceptions: map[string]string{},
		Fields:                 fields,
	}
}

func allowedRegistry(event string) Registry {
	registry := registryWith(map[string][]string{event: {"device_id"}}, "event", "device_id", "ok")
	registry.LocatingFreeExceptions = map[string]string{event: "fixture: reviewed conditional branch"}
	return registry
}

func findingsFor(t *testing.T, source string, registry Registry) []Finding {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	findings, err := Findings(root, registry)
	if err != nil {
		t.Fatal(err)
	}
	return findings
}

func serverRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func registryPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	return filepath.Join(filepath.Dir(file), "registry.json")
}

func between(text, start, end string) string {
	from := strings.Index(text, start)
	if from < 0 {
		return ""
	}
	from += len(start)
	to := strings.Index(text[from:], end)
	if to < 0 {
		return ""
	}
	return text[from : from+to]
}

func sortedKeys(keys map[string]bool) []string {
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
