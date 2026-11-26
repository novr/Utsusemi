//go:build darwin

package hostdns

func defaultResolver() Resolver {
	return NewDSCResolver(nil)
}
