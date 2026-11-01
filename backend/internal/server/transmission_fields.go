package server

import (
	_ "embed"
	"encoding/json"
	"math"
	"strings"
)

//go:embed transmission-fields.json
var transmissionFieldsJSON []byte

type transmissionField struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Group   string `json:"group"`
	Unit    string `json:"unit,omitempty"`
	MinRPC  int    `json:"minRPC,omitempty"`
	Options []struct {
		Value any    `json:"value"`
		Label string `json:"label"`
	} `json:"options,omitempty"`
}

var transmissionFields = func() map[string][]transmissionField {
	var fields map[string][]transmissionField
	if err := json.Unmarshal(transmissionFieldsJSON, &fields); err != nil {
		panic(err)
	}
	return fields
}()

func transmissionFieldNames(group string) []string {
	fields := []string{}
	for _, field := range transmissionFields[group] {
		fields = append(fields, field.Key)
	}
	return fields
}
func transmissionFieldValid(value any, field transmissionField) bool {
	switch field.Kind {
	case "bool":
		_, ok := value.(bool)
		return ok
	case "string", "text":
		text, ok := value.(string)
		return ok && !strings.ContainsRune(text, 0)
	case "enum":
		for _, option := range field.Options {
			if value == option.Value {
				return true
			}
		}
		return false
	default:
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > math.MaxInt32 {
			return false
		}
		if field.Kind == "number" {
			return true
		}
		if math.Trunc(number) != number {
			return false
		}
		if field.Kind == "port" {
			return number >= 1 && number <= 65535
		}
		if field.Kind == "time" {
			return number < 1440
		}
		if field.Kind == "days" {
			return number <= 127
		}
		return true
	}
}
