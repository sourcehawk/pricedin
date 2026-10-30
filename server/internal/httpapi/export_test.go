package httpapi

// Routes returns every operation the API serves, as "METHOD /pattern".
func Routes() []string {
	a := &api{}
	var out []string
	for _, r := range append(a.authRoutes(), a.v1Routes()...) {
		out = append(out, r.method+" "+r.pattern)
	}
	return out
}
