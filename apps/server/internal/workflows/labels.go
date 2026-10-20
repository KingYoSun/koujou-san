package workflows

import "encoding/json"

// Labels never include command parameters, prompts or Context bodies.
type definitionLabel struct {
	Ref
	Kind   string   `json:"kind,omitempty"`
	Name   string   `json:"name,omitempty"`
	Inputs []string `json:"inputs,omitempty"`
}

func labelOf(record Record) definitionLabel {
	label := definitionLabel{Ref: record.Ref, Kind: record.Kind}
	var value struct {
		Name   string   `json:"name"`
		Inputs []string `json:"inputs"`
	}
	if json.Unmarshal(record.Value, &value) == nil {
		label.Name, label.Inputs = value.Name, value.Inputs
	}
	return label
}
