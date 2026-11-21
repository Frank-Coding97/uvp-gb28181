package playauth

// OpenAPIViewerBinding is supplied only by an authenticated ZLM on_play hook.
// Client and grant authority remain signed in Claims and durable in the DB.
type OpenAPIViewerBinding struct {
	NodeUUID        string
	BootNonce       string
	Identifier      string
	Protocol        string
	Schema          string
	VHost           string
	App             string
	Stream          string
	MediaGeneration uint64
}

// OpenAPIFlowReport identifies one final player flow from an authenticated
// ZLM hook. Unknown viewers are ignored by the durable quota store.
type OpenAPIFlowReport struct {
	NodeUUID   string
	BootNonce  string
	Identifier string
	Protocol   string
	Schema     string
	VHost      string
	App        string
	Stream     string
	Player     bool
}
