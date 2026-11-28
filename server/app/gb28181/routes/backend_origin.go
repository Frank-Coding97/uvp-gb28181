package routes

import "uvplatform.com/uvp-gb28181/app/gb28181/handler"

func SetVideoShareBackendOriginObserver(observer handler.BackendOriginObserver) {
	hookController.SetBackendOriginObserver(observer)
}
