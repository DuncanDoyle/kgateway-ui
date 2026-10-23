package cache

// SetHandlerHook makes every event handler of c run f before it notifies.
func SetHandlerHook(c *Cache, f func()) { c.handlerHook.Store(&f) }
