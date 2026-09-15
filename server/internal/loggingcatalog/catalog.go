// Package loggingcatalog owns the event registry, the field dictionary, and
// the gate that keeps production call sites aligned with them.
//
// Why a package of its own (C09 in docs/logging-governance/content-plan.md):
// internal/loggingcontract is a type-aware policy engine (go/packages, 1700+
// lines in policy.go, 4500+ lines in the package). The rules here are purely
// syntactic and need no type information, so they live beside it instead of
// growing it. Nothing in this package imports loggingcontract and vice versa.
//
// The registry is a frozen baseline. It exists so that adding an event, adding
// a field name, or dropping a locating field becomes a visible, reviewable diff
// instead of an invisible accumulation. Regeneration is possible (see
// TestLoggingCatalogRegenerate) and that is on purpose: the diff *is* the
// review. Editing the registry to silence a finding without reading the diff is
// the one way to defeat this gate.
package loggingcatalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed registry.json
var registryJSON []byte

// FindingKind identifies one catalog finding.
type FindingKind string

const (
	// KindUnregisteredEvent: the event value is not in the registry.
	KindUnregisteredEvent FindingKind = "unregistered_event"
	// KindEventLevelEcho: the event name ends with a level word (.warn/.error).
	KindEventLevelEcho FindingKind = "event_level_echo"
	// KindFieldNotInDictionary: the field key is well formed but not registered.
	KindFieldNotInDictionary FindingKind = "field_not_in_dictionary"
	// KindFieldNameFormat: the field key is not lower_snake_case.
	KindFieldNameFormat FindingKind = "field_name_not_snake_case"
	// KindLocatingMissing: this call site carries no locating field while other
	// call sites of the same event do.
	KindLocatingMissing FindingKind = "locating_field_missing"
	// KindLocatingLost: a locating field the registry records for an event is
	// no longer carried by any call site of that event.
	KindLocatingLost FindingKind = "locating_field_lost"
	// KindStaleFieldEntry: a dictionary entry that no longer appears anywhere.
	KindStaleFieldEntry FindingKind = "stale_field_dictionary_entry"
	// KindEventLevelDivergence: one event name is logged at more than one level.
	KindEventLevelDivergence FindingKind = "event_level_divergence"
)

// Finding is a source-only diagnostic. It never contains evaluated values.
type Finding struct {
	Kind        FindingKind
	File        string
	Line        int
	Event       string
	Field       string
	Description string
}

func (f Finding) String() string {
	location := f.File
	if f.Line != 0 {
		location = fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	return fmt.Sprintf("%s %s %s", location, f.Kind, f.Description)
}

// Registry is the single source of truth for event names, field names, and the
// locating fields each event is known to carry.
type Registry struct {
	// Events maps an event name to every locating field that at least one of
	// its call sites carries (union across call sites). An empty list means no
	// call site carries a locating field at all.
	Events map[string][]string `json:"events"`
	// LocatingFreeExceptions names events that are allowed to have call sites
	// without a locating field even though other call sites of the same event
	// do carry one (conditional branches, typically). The value is the reason,
	// and it is required.
	LocatingFreeExceptions map[string]string `json:"locating_free_exceptions,omitempty"`
	// Fields is the field-key dictionary; matching is exact, so a renamed key
	// is a new key.
	Fields []string `json:"fields"`
}

// Embedded returns the registry that ships with the repository.
func Embedded() (Registry, error) {
	var registry Registry
	if err := json.Unmarshal(registryJSON, &registry); err != nil {
		return Registry{}, fmt.Errorf("decode embedded logging registry: %w", err)
	}
	if len(registry.Events) == 0 || len(registry.Fields) == 0 {
		return Registry{}, fmt.Errorf("embedded logging registry is empty")
	}
	return registry, nil
}

// Site is one structured logging call site found in production source.
type Site struct {
	File string
	Line int
	// Event is the resolved event name; empty when the call site carries no
	// event argument at all.
	Event string
	// EventReference is the argument text when an event argument exists but
	// cannot be resolved to a string constant by this package.
	EventReference string
	// Level is the zap method the call site goes through (Info, Warn, ...).
	// Empty when the call site never reached the level methods.
	Level string
	// Keys are the field keys written at this call site, in source order.
	Keys []string
}

// Locating returns the locating field keys carried by this call site.
func (s Site) Locating() []string {
	out := make([]string, 0, len(s.Keys))
	for _, key := range s.Keys {
		if locatingFields[normalize(key)] {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// Observe derives a registry from source. It is the generator behind
// TestLoggingCatalogRegenerate and never runs as part of the gate.
func Observe(root string) (Registry, error) {
	sites, err := inspect(root)
	if err != nil {
		return Registry{}, err
	}
	registry := Registry{
		Events:                 make(map[string][]string),
		LocatingFreeExceptions: make(map[string]string),
		Fields:                 make([]string, 0),
	}
	fields := make(map[string]bool)
	for _, site := range sites {
		for _, key := range site.Keys {
			fields[key] = true
		}
		if site.Event == "" || site.EventReference != "" {
			continue
		}
		merged := append(append([]string{}, registry.Events[site.Event]...), site.Locating()...)
		registry.Events[site.Event] = dedupeLocating(merged)
	}
	for key := range fields {
		registry.Fields = append(registry.Fields, key)
	}
	sort.Strings(registry.Fields)
	return registry, nil
}

// Findings compares production source under root with the registry.
//
// Rules, and what each one is for:
//
//	unregistered_event        a new event must be a deliberate addition
//	event_level_echo          an event name must say what happened, not how loud
//	field_not_in_dictionary   do not invent a second name for one meaning
//	field_name_not_snake_case locking in the C08 rename (camelCase 58 -> 0)
//	locating_field_missing    a new call site must stay locatable
//	locating_field_lost       an event must not lose a dimension it had
//	stale_field_dictionary_entry  the dictionary must not keep dead names
//	event_level_divergence    one event name must not mean two severities
//
// What is deliberately NOT here: "the event value must be a compile-time
// constant". That is loggingcontract's dynamic_event rule, and adapters that
// legitimately derive the event name from data are reviewed by SHA in
// reviewed_adapters.json. A call site this package cannot read is skipped.
func Findings(root string, registry Registry) ([]Finding, error) {
	sites, err := inspect(root)
	if err != nil {
		return nil, err
	}
	findings := make([]Finding, 0)
	dictionary := make(map[string]bool, len(registry.Fields))
	for _, key := range registry.Fields {
		dictionary[key] = true
	}
	// observed is the union of locating fields per event, as found in source.
	observed := make(map[string]map[string]bool)
	for _, site := range sites {
		if site.Event == "" || site.EventReference != "" {
			continue
		}
		if observed[site.Event] == nil {
			observed[site.Event] = make(map[string]bool)
		}
		for _, name := range site.Locating() {
			observed[site.Event][normalize(name)] = true
		}
	}
	usedFields := make(map[string]bool)

	for _, site := range sites {
		for _, key := range site.Keys {
			usedFields[key] = true
			// Report the format violation alone when the key is not even
			// lower_snake_case: the fix (rename) makes the dictionary question
			// moot, and one actionable finding beats two for one cause.
			if !fieldNamePattern.MatchString(key) {
				findings = append(findings, Finding{
					Kind: KindFieldNameFormat, File: site.File, Line: site.Line,
					Field: key, Description: "field key must be lower_snake_case with ASCII letters, digits and underscores",
				})
				continue
			}
			if !dictionary[key] {
				findings = append(findings, Finding{
					Kind: KindFieldNotInDictionary, File: site.File, Line: site.Line,
					Field: key, Description: "field key is not registered; reuse the dictionary name for this meaning or register the new one deliberately",
				})
			}
		}

		if site.EventReference != "" {
			// An event the gate cannot read is deliberately not reported here.
			// That fact already has an owner: loggingcontract's dynamic_event
			// rule plus the SHA-sealed reviews in reviewed_adapters.json (which
			// is how RepeatKey.Event and ZapJobLogger.emit were reviewed). Two
			// gates reporting one fact produces a fake failure - fixing one
			// turns the other red.
			continue
		}
		if site.Event == "" {
			continue
		}

		registered, known := registry.Events[site.Event]
		if lastSegment(site.Event) != "" && levelEchoSegments[lastSegment(site.Event)] {
			findings = append(findings, Finding{
				Kind: KindEventLevelEcho, File: site.File, Line: site.Line, Event: site.Event,
				Description: "event name ends with a level word; name the event after what happened",
			})
			continue
		}
		if !known {
			findings = append(findings, Finding{
				Kind: KindUnregisteredEvent, File: site.File, Line: site.Line, Event: site.Event,
				Description: "event is not in the registry; register it in internal/loggingcatalog/registry.json (only when the four-column entry from C01 is settled)",
			})
			continue
		}
		if len(registered) == 0 {
			// The event carries no locating field anywhere: exempt by record.
			continue
		}
		if len(site.Locating()) != 0 {
			continue
		}
		if _, allowed := registry.LocatingFreeExceptions[site.Event]; allowed {
			continue
		}
		findings = append(findings, Finding{
			Kind: KindLocatingMissing, File: site.File, Line: site.Line, Event: site.Event,
			Description: "call site carries no locating field while other call sites of this event do; add the field or add a reviewed entry to locating_free_exceptions",
		})
	}

	for event, registered := range registry.Events {
		found := observed[event]
		for _, name := range registered {
			key := normalize(name)
			if found[key] {
				continue
			}
			findings = append(findings, Finding{
				Kind: KindLocatingLost, Event: event, Field: name,
				Description: "no call site of this event carries this locating field any more; restore it or record the removal in registry.json",
			})
		}
	}
	for _, finding := range eventLevelDivergences(sites) {
		findings = append(findings, finding)
	}
	for _, key := range registry.Fields {
		if usedFields[key] {
			continue
		}
		findings = append(findings, Finding{
			Kind: KindStaleFieldEntry, Field: key,
			Description: "field key no longer appears in production source; prune it from registry.json so the name cannot come back unnoticed",
		})
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		if findings[i].Event != findings[j].Event {
			return findings[i].Event < findings[j].Event
		}
		return findings[i].Field < findings[j].Field
	})
	return findings, nil
}

// eventLevelDivergences reports each event name that is logged at more than one
// level, once per event.
//
// This is a rule, not a baseline it needs no registry entry: an event name that
// means two different severities cannot be aggregated, so any count you take by
// name is a lie. C04 hit this first - `play.reconcile.probe_failed` was WARN in
// two branches and DEBUG in a third. The fix is always to split the name by
// outcome, never to pick a winner among levels.
//
// Scope: only call sites whose event value this package can resolve to a string
// constant. Adapters that derive the level from runtime data (the HTTP access
// log, the scheduler job logger) write their event through a `fields` slice and
// are invisible here; they are also invisible to every other rule in this file.
func eventLevelDivergences(sites []Site) []Finding {
	type eventLevels struct {
		levels map[string]bool
		sites  []Site
	}
	byEvent := make(map[string]*eventLevels)
	for _, site := range sites {
		if site.Event == "" || site.EventReference != "" || site.Level == "" {
			continue
		}
		entry := byEvent[site.Event]
		if entry == nil {
			entry = &eventLevels{levels: make(map[string]bool)}
			byEvent[site.Event] = entry
		}
		entry.levels[site.Level] = true
		entry.sites = append(entry.sites, site)
	}
	events := make([]string, 0, len(byEvent))
	for event := range byEvent {
		events = append(events, event)
	}
	sort.Strings(events)

	findings := make([]Finding, 0)
	for _, event := range events {
		entry := byEvent[event]
		if len(entry.levels) < 2 {
			continue
		}
		sort.Slice(entry.sites, func(i, j int) bool {
			if entry.sites[i].File != entry.sites[j].File {
				return entry.sites[i].File < entry.sites[j].File
			}
			return entry.sites[i].Line < entry.sites[j].Line
		})
		levels := make([]string, 0, len(entry.levels))
		for level := range entry.levels {
			levels = append(levels, level)
		}
		sort.Strings(levels)
		where := make([]string, 0, len(levels))
		for _, level := range levels {
			for _, site := range entry.sites {
				if site.Level == level {
					where = append(where, fmt.Sprintf("%s@%s:%d", strings.ToLower(level), site.File, site.Line))
					break
				}
			}
		}
		first := entry.sites[0]
		findings = append(findings, Finding{
			Kind: KindEventLevelDivergence, File: first.File, Line: first.Line, Event: event,
			Description: fmt.Sprintf("event is logged at %d levels (%s); give each outcome its own event name so aggregation by name stays truthful",
				len(levels), strings.Join(where, ", ")),
		})
	}
	return findings
}

// fieldNamePattern is the one allowed field-key shape. It is a rule, not a
// baseline: it needs no registry entry, because the dictionary is generated
// from source and every generated key already satisfies it.
var fieldNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// levelEchoSegments mirrors scan-logging.py's EVENT_LEVEL_ECHO_RE. Reading a
// level out of the suffix is a naming defect: the suffix says how loud the line
// is, not what happened, and renaming the level silently renames the event.
//
// "panic" is deliberately absent: http.panic and play.reconcile.panic use
// .panic as the event ("the process fell over"), not as a level.
var levelEchoSegments = map[string]bool{
	"info": true, "warn": true, "error": true, "debug": true, "fatal": true,
}

// locatingFields mirrors scan-logging.py's IDENT_BASE. Both lists are checked
// against each other by TestLoggingCatalogMirrorsScanScript: the report and the
// gate must agree on what "carrying a locating field" means, or the numbers in
// docs/logging-governance/README.md stop describing the code the gate protects.
var locatingFields = map[string]bool{
	"deviceid": true, "devicecode": true, "channelid": true, "channelcode": true,
	"callid": true, "platformid": true, "streamid": true, "nodeid": true,
	"serverid": true, "mediaserverid": true, "requestid": true, "operationid": true,
	"jobid": true, "workorderid": true, "orderid": true, "sn": true,
	"endpoint": true, "route": true, "userid": true, "subscriptionid": true,
	"traceid": true, "correlationid": true,
	// peer = the peer identity observed in this message ("host:port/transport
	// from=user"). Cascade is a pure SIP path, so "who is talking to me" is the
	// only identity clue when the platform cannot be recognised.
	"peer": true,
	// source_ip = the peer address of a ZLM hook callback. The hook path has no
	// SIP call_id and no inherited HTTP request id.
	"sourceip": true,
	// C08 (2026-09-15) non-device locating dimensions: who / which role /
	// which template / which file. The bar was not lowered - each one still has
	// to answer "which object do I look at first when this fails". State-like
	// keys (phase / status / dialect / a generic path) stay out.
	"username": true,
	"roleid":   true, "parentroleid": true,
	"menuid":       true,
	"templatename": true, "templatepath": true, "filepath": true,
	"operatorid": true,
	"uploadid":   true,
	"tablename":  true,
}

// loggerMethods are the zap level methods a structured call site goes through.
var loggerMethods = map[string]bool{
	"Debug": true, "Info": true, "Warn": true, "Error": true,
	"DPanic": true, "Panic": true, "Fatal": true,
}

// zapPackageImport gates the whole walk: without it a method called Info on
// some unrelated type would be read as a log line.
const zapPackageImport = "go.uber.org/zap"

var (
	skipSourceDirs = map[string]bool{"vendor": true, "node_modules": true, "testdata": true, ".git": true}
	skipSourcePath = []string{
		"/internal/loggingacceptance",
		"/internal/loggingcontract",
		"/internal/loggingcatalog",
	}
)

func inspect(root string) ([]Site, error) {
	files, err := sourceFiles(root)
	if err != nil {
		return nil, err
	}
	byDir := make(map[string][]string)
	for _, file := range files {
		dir := filepath.Dir(file)
		byDir[dir] = append(byDir[dir], file)
	}
	fset := token.NewFileSet()
	sites := make([]Site, 0)
	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		bindings := make(map[string]string)
		parsed := make(map[string]*ast.File, len(byDir[dir]))
		for _, file := range byDir[dir] {
			node, parseErr := parser.ParseFile(fset, file, nil, 0)
			if parseErr != nil {
				return nil, fmt.Errorf("parse %s: %w", file, parseErr)
			}
			parsed[file] = node
			mergeBindings(bindings, stringBindings(node))
		}
		for _, file := range byDir[dir] {
			node := parsed[file]
			if !importsZap(node) {
				continue
			}
			rel, relErr := relativePath(root, file)
			if relErr != nil {
				return nil, relErr
			}
			sites = append(sites, fileSites(fset, node, rel, bindings)...)
		}
	}
	return sites, nil
}

func sourceFiles(root string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		slash := filepath.ToSlash(path)
		if entry.IsDir() {
			if skipSourceDirs[entry.Name()] {
				return fs.SkipDir
			}
			for _, suffix := range skipSourcePath {
				if strings.HasSuffix(slash, suffix) {
					return fs.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func relativePath(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func importsZap(file *ast.File) bool {
	for _, spec := range file.Imports {
		if spec.Path == nil {
			continue
		}
		if value, err := strconv.Unquote(spec.Path.Value); err == nil && value == zapPackageImport {
			return true
		}
	}
	return false
}

// ambiguousBinding marks a name that resolves to more than one string. Such a
// name is reported as unresolved rather than guessed at.
const ambiguousBinding = "\x00ambiguous"

func stringBindings(file *ast.File) map[string]string {
	out := make(map[string]string)
	record := func(name string, expression ast.Expr) {
		literal, ok := expression.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return
		}
		if previous, seen := out[name]; seen && previous != value {
			out[name] = ambiguousBinding
			return
		}
		out[name] = value
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.ValueSpec:
			for index, name := range typed.Names {
				if index >= len(typed.Values) {
					break
				}
				record(name.Name, typed.Values[index])
			}
		case *ast.AssignStmt:
			if typed.Tok != token.DEFINE && typed.Tok != token.ASSIGN {
				return true
			}
			for index, target := range typed.Lhs {
				if index >= len(typed.Rhs) {
					break
				}
				if name, ok := target.(*ast.Ident); ok {
					record(name.Name, typed.Rhs[index])
				}
			}
		}
		return true
	})
	return out
}

func mergeBindings(into, from map[string]string) {
	for name, value := range from {
		if previous, seen := into[name]; seen && previous != value {
			into[name] = ambiguousBinding
			continue
		}
		into[name] = value
	}
}

// fileSites returns every structured logging call site in file that carries at
// least one zap field. A call with no zap field cannot carry an event either,
// and the type-aware policy gate already reports those as missing_event.
func fileSites(fset *token.FileSet, file *ast.File, rel string, bindings map[string]string) []Site {
	sites := make([]Site, 0)
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !loggerMethods[selector.Sel.Name] {
			return true
		}
		fields := zapFieldCalls(call)
		if len(fields) == 0 {
			return true
		}
		site := Site{File: rel, Line: fset.Position(call.Pos()).Line, Level: selector.Sel.Name}
		for _, field := range fields {
			key, ok := stringLiteral(field.Args[0])
			if !ok {
				continue
			}
			// The event tag is a field key like any other: it stays in Keys so
			// the dictionary covers it too, and its value is resolved on top.
			site.Keys = append(site.Keys, key)
			if key != "event" || len(field.Args) < 2 {
				continue
			}
			value, resolved := resolveString(field.Args[1], bindings)
			switch {
			case resolved && value != "":
				site.Event = value
			case !resolved:
				site.EventReference = expressionText(field.Args[1])
			}
		}
		sites = append(sites, site)
		return true
	})
	return sites
}

// zapFieldCalls returns every keyed zap field constructor reached from call,
// including the ones sitting in a derived logger (logger.With(...).Info(...)).
func zapFieldCalls(call *ast.CallExpr) []*ast.CallExpr {
	out := make([]*ast.CallExpr, 0)
	ast.Inspect(call, func(node ast.Node) bool {
		candidate, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := candidate.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "zap" || !keyedFieldConstructors[selector.Sel.Name] {
			return true
		}
		if len(candidate.Args) == 0 {
			return true
		}
		if _, ok := stringLiteral(candidate.Args[0]); !ok {
			return true
		}
		out = append(out, candidate)
		return true
	})
	// ast.Inspect visits the outer call first; sort by position so keys keep
	// source order and findings read top to bottom.
	sort.Slice(out, func(i, j int) bool { return out[i].Pos() < out[j].Pos() })
	return out
}

// keyedFieldConstructors are the zap constructors whose first argument is the
// field key. Single-value constructors (zap.Error, zap.Skip, zap.Namespace)
// are not here: they carry no key, or their argument is not a field name.
var keyedFieldConstructors = map[string]bool{
	"String": true, "ByteString": true, "Binary": true,
	"Int": true, "Int8": true, "Int16": true, "Int32": true, "Int64": true,
	"Uint": true, "Uint8": true, "Uint16": true, "Uint32": true,
	"Uint64": true, "Uintptr": true,
	"Float32": true, "Float64": true, "Complex64": true, "Complex128": true,
	"Bool": true, "Duration": true, "Time": true, "Any": true,
	"Stringer": true, "Object": true, "NamedError": true,
	"Strings": true, "Ints": true, "Int32s": true, "Int64s": true,
	"Uints": true, "Uint32s": true, "Uint64s": true,
	"Float32s": true, "Float64s": true, "Durations": true, "Times": true,
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func resolveString(expression ast.Expr, bindings map[string]string) (string, bool) {
	if value, ok := stringLiteral(expression); ok {
		return value, true
	}
	name, ok := expression.(*ast.Ident)
	if !ok {
		return "", false
	}
	value, seen := bindings[name.Name]
	if !seen || value == ambiguousBinding {
		return "", false
	}
	return value, true
}

// expressionText renders only the shape of an expression. Literal values never
// reach a finding, so a report cannot become a value leak.
func expressionText(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return expressionText(typed.X) + "." + typed.Sel.Name
	case *ast.CallExpr:
		return expressionText(typed.Fun) + "(...)"
	case *ast.BasicLit:
		return typed.Kind.String()
	}
	return "expression"
}

func normalize(key string) string {
	return strings.ToLower(strings.ReplaceAll(key, "_", ""))
}

// dedupeLocating keeps the first spelling seen for each normalized name, so the
// registry stores what the source actually writes (device_id) while comparing
// on the normalized form (deviceid), the same way scan-logging.py does it.
func dedupeLocating(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		key := normalize(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func lastSegment(event string) string {
	index := strings.LastIndex(event, ".")
	if index < 0 || index == len(event)-1 {
		return ""
	}
	return event[index+1:]
}

// ReadRegistry reads a registry from disk; used by the reviewed-input tests and
// by the regeneration path.
func ReadRegistry(path string) (Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Registry{}, err
	}
	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return Registry{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return registry, nil
}
