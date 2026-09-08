package gb28181

import openapimedia "uvplatform.cn/uvp-gb28181/app/openapi/media"

// This facade is process-lifetime. Reload changes its admitted generation only
// after the previous generation's actual OpenAPI calls have returned.
var openAPILivePlayer = openapimedia.NewLivePlayerRuntime()

func OpenAPILivePlayer() *openapimedia.LivePlayerRuntime { return openAPILivePlayer }
