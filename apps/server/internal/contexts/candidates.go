package contexts

// CandidateInput binds an explicit selection to a freshly authorized projection
// page. HTTP and MCP share this check; client evidence never supplies candidates.
func CandidateInput(request ResolveRequest, candidates []Ref, evidence CandidateEvidence, outsideCode string) (ResolveRequest, *Gap) {
	allowed := make(map[Ref]bool, len(candidates))
	for _, ref := range candidates {
		allowed[ref] = true
	}
	for _, refs := range [][]Ref{request.Selection.Include, request.Selection.Exclude} {
		for _, ref := range refs {
			if !allowed[ref] {
				return request, &Gap{Code: outsideCode, Ref: &ref}
			}
		}
	}
	request.Evidence = append(request.Evidence, evidence)
	return request, nil
}
