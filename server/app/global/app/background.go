package app

import "uvplatform.cn/uvp-gb28181/app/utils/asyncgroup"

// BackgroundWork owns application work that outlives an HTTP request. It is
// intentionally package-global and zero-valued so its admission stays open
// for the whole application lifetime until shutdown closes it.
var BackgroundWork asyncgroup.Group
