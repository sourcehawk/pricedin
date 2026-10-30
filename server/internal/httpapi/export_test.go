package httpapi

// Routes returns every operation the API serves, as "METHOD /pattern".
func Routes() []string {
	a := &api{}
	routes := append(a.authRoutes(), a.v1Routes()...)
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		out = append(out, r.method+" "+r.pattern)
	}
	return out
}
