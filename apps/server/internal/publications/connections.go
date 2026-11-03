package publications

import (
	"sort"
)

func connectionRefs(c Connection) []Ref {
	out := []Ref{}
	for _, ref := range []Ref{c.Resource, c.Credential} {
		if ref.ID != "" {
			out = append(out, ref)
		}
	}
	return out
}
func (r *Remote) Connections(w string) []Connection {
	out := []Connection{}
	if r != nil && r.config != nil {
		for _, c := range r.config.Connections {
			if c.Workspace == w {
				out = append(out, c)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
