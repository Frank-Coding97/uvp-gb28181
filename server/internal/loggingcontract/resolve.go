package loggingcontract

import (
	"go/ast"
	"path/filepath"
	"sort"
	"strings"
)

func resolveCallGraph(state *scanState, report *Report) {
	for index, edge := range state.edges {
		candidates := state.byName[edge.to]
		resolved := len(candidates) == 1
		note := "no matching declaration; library, generated or dynamic call"
		if len(candidates) == 1 {
			note = "matched a unique declaration by function name; Go type/interface resolution was not run"
			state.edges[index].to = candidates[0].id
		} else if len(candidates) > 1 {
			note = "ambiguous function name; package/type resolution was not run"
		}
		caller, ok := findFunction(state.functions, edge.from)
		if !ok {
			continue
		}
		line := 0
		if caller.decl.Body != nil {
			// The edge list stores only names. Finding the first matching call
			// keeps output useful without pretending the edge is type-resolved.
			line = firstCallLine(state, caller, edge.to)
		}
		report.CallEdges = append(report.CallEdges, CallEdge{
			File:           caller.file.rel,
			Line:           line,
			Caller:         caller.name,
			Callee:         edge.to,
			Resolved:       resolved,
			ResolutionNote: note,
		})
	}
}

func assignReachabilityAndOwnership(state *scanState, report *Report) {
	adjacency := make(map[string][]string)
	for _, edge := range state.edges {
		if _, ok := findFunction(state.functions, edge.to); !ok {
			continue
		}
		adjacency[edge.from] = append(adjacency[edge.from], edge.to)
	}

	rootRoutes := make(map[string][]string)
	for index := range report.Routes {
		route := &report.Routes[index]
		for _, handler := range route.Handlers {
			name := handler
			if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
				name = name[dot+1:]
			}
			candidates := state.byName[name]
			if len(candidates) != 1 {
				continue
			}
			route.Resolved = true
			rootRoutes[candidates[0].id] = append(rootRoutes[candidates[0].id], routeID(*route))
		}
		if route.Resolved {
			route.ResolutionNote += "; at least one handler name matched a unique declaration"
		} else {
			route.ResolutionNote += "; no handler name matched a unique declaration"
		}
	}

	reachable := make(map[string]map[string]bool)
	for root, names := range rootRoutes {
		seen := make(map[string]bool)
		queue := []string{root}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			if seen[current] {
				continue
			}
			seen[current] = true
			queue = append(queue, adjacency[current]...)
		}
		for current := range seen {
			if reachable[current] == nil {
				reachable[current] = make(map[string]bool)
			}
			for _, name := range names {
				reachable[current][name] = true
			}
		}
	}

	for index := range report.Sites {
		site := &report.Sites[index]
		site.OwnerTask, site.OwnerReason = ownerForSite(site.File, site.Function, site.Kind, site.ContextSource)
		site.RouteNames = append(site.RouteNames, routeNamesFor(site.functionID, reachable)...)
		if len(site.RouteNames) > 0 {
			site.ContextSource = ContextHTTP
			site.ResolutionNote = "reachable from a registered route through unique function-name edges; dynamic/interface edges may be missing"
		} else {
			site.ContextSource = ContextBackground
			site.ResolutionNote = "no bounded route path proven; SIP/background/unresolved code remains separate"
		}
		if site.OwnerTask == "" {
			report.Unassigned = append(report.Unassigned, *site)
		}
	}
	for index := range report.DBBoundaries {
		boundary := &report.DBBoundaries[index]
		boundary.OwnerTask, boundary.OwnerReason = ownerForSite(boundary.File, boundary.Function, KindDBBoundary, boundary.ContextSource)
		boundary.RouteNames = append(boundary.RouteNames, routeNamesFor(functionIDFor(state, boundary.File, boundary.Function), reachable)...)
		if len(boundary.RouteNames) > 0 {
			boundary.ContextSource = ContextHTTP
			boundary.ResolutionNote += "; route path is name-based and may miss interfaces or dynamic calls"
		} else {
			boundary.ContextSource = ContextBackground
			boundary.ResolutionNote += "; no bounded HTTP route path was proven"
		}
	}
	for index := range report.Issues {
		issue := &report.Issues[index]
		issue.OwnerTask, _ = ownerForSite(issue.File, issue.Function, SiteKind(issue.Kind), ContextBackground)
	}
}

func findFunction(functions []*functionDecl, id string) (*functionDecl, bool) {
	for _, fn := range functions {
		if fn.id == id {
			return fn, true
		}
	}
	return nil, false
}

func routeNamesFor(functionID string, reachable map[string]map[string]bool) []string {
	names := make([]string, 0)
	for routeName := range reachable[functionID] {
		names = append(names, routeName)
	}
	sort.Strings(names)
	return names
}

func routeID(route Route) string {
	return route.Method + " " + route.Path
}

func functionIDFor(state *scanState, file, function string) string {
	for _, fn := range state.functions {
		if fn.file.rel == file && fn.name == function {
			return fn.id
		}
	}
	return ""
}

func firstCallLine(state *scanState, fn *functionDecl, target string) int {
	line := 0
	if fn.decl.Body == nil {
		return line
	}
	ast.Inspect(fn.decl.Body, func(node ast.Node) bool {
		if line != 0 {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := selectorName(call.Fun)
		if name == "" {
			name = identName(call.Fun)
		}
		if target == name || (strings.HasSuffix(target, "."+name) && name != "") {
			line = state.fset.Position(call.Pos()).Line
		}
		return true
	})
	return line
}

func ownerForSite(file, function string, kind SiteKind, context ContextSource) (string, string) {
	file = filepath.ToSlash(file)
	if strings.HasPrefix(file, "bootstrap/") || file == "service/zaphooks.go" || strings.HasPrefix(file, "app/gb28181/bootstrap") {
		return "T05", "bootstrap, legacy bridge or GB bootstrap logging is owned by T05"
	}
	if kind == KindDBBoundary && (strings.HasPrefix(file, "app/utils/gormhelper/") || strings.HasPrefix(file, "app/global/app/")) {
		return "T07", "GORM/global DB boundary is owned by T07"
	}
	if strings.HasPrefix(file, "app/utils/ginhelper/") || strings.HasPrefix(file, "app/utils/response/") || file == "app/main.go" || file == "main.go" {
		return "T12", "shared Gin/response/lifecycle source is tracked by T12; T06 consumes its HTTP contract"
	}
	if strings.HasPrefix(file, "app/utils/schedulerhelper/") || strings.HasPrefix(file, "app/scheduler/") || strings.Contains(file, "/recordingplan/") {
		return "T10", "scheduler and recording-plan logging is owned by T10"
	}
	if strings.Contains(file, "/zlm/heartbeat/") || strings.Contains(file, "/zlm/streamprobe/") || strings.HasPrefix(file, "app/gb28181/cascade/") {
		return "T11", "repeatable node/cascade state logging is owned by T11"
	}
	if strings.HasPrefix(file, "app/gb28181/controllers/") || strings.HasPrefix(file, "app/gb28181/handler/") || strings.HasPrefix(file, "app/gb28181/cascade/controller/") {
		if context == ContextHTTP {
			return "T09", "reachable GB28181 HTTP/Hook logging is owned by T09"
		}
		return "T14", "GB28181 handler candidate without a proven HTTP route stays in T14 for SIP/background review"
	}
	if strings.HasPrefix(file, "app/controllers/") || strings.HasPrefix(file, "app/service/") || strings.HasPrefix(file, "app/models/") || strings.HasPrefix(file, "app/middleware/") || strings.HasPrefix(file, "app/routes/") {
		if context == ContextHTTP {
			return "T08", "reachable base HTTP logging is owned by T08"
		}
		return "T14", "base package candidate without a proven HTTP route stays in T14"
	}
	if kind == KindDBBoundary && context == ContextHTTP {
		return "T08", "HTTP-reachable DB boundary requires the owning API task"
	}
	return "T14", "remaining root/SIP/background logging candidate is explicitly assigned to T14"
}

func sortReport(report *Report) {
	sort.Slice(report.Sites, func(i, j int) bool {
		return siteKey(report.Sites[i]) < siteKey(report.Sites[j])
	})
	sort.Slice(report.Routes, func(i, j int) bool {
		return routeKey(report.Routes[i]) < routeKey(report.Routes[j])
	})
	sort.Slice(report.CallEdges, func(i, j int) bool {
		if report.CallEdges[i].File != report.CallEdges[j].File {
			return report.CallEdges[i].File < report.CallEdges[j].File
		}
		return report.CallEdges[i].Line < report.CallEdges[j].Line
	})
	sort.Slice(report.DBBoundaries, func(i, j int) bool {
		return dbKey(report.DBBoundaries[i]) < dbKey(report.DBBoundaries[j])
	})
	sort.Slice(report.Issues, func(i, j int) bool {
		if report.Issues[i].File != report.Issues[j].File {
			return report.Issues[i].File < report.Issues[j].File
		}
		return report.Issues[i].Line < report.Issues[j].Line
	})
	sort.Slice(report.Excluded, func(i, j int) bool { return report.Excluded[i].Path < report.Excluded[j].Path })
}

func siteKey(site Site) string {
	return site.File + ":" + string(rune(site.Line)) + ":" + string(site.Kind) + ":" + site.Operation
}

func routeKey(route Route) string {
	return route.File + ":" + string(rune(route.Line)) + ":" + route.Method + ":" + route.Path
}

func dbKey(boundary DBBoundary) string {
	return boundary.File + ":" + string(rune(boundary.Line)) + ":" + boundary.Operation
}
