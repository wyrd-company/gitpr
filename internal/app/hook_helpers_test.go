//go:build test

package app

func oneShotBlockingHook(service *Service, loaded chan<- struct{}, release <-chan struct{}) func() {
	return func() {
		close(loaded)
		<-release
		service.store.SetBeforeSaveHook(nil)
	}
}
