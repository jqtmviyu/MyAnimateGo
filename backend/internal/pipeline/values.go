package pipeline

import (
	"encoding/json"
	"strconv"
)

func valueString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func valueBool(value any) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	if text, ok := value.(string); ok {
		parsed, _ := strconv.ParseBool(text)
		return parsed
	}
	return valueFloat(value) != 0
}

func valueFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case int32:
		return float64(typed)
	case json.Number:
		value, _ := typed.Float64()
		return value
	case string:
		value, _ := strconv.ParseFloat(typed, 64)
		return value
	default:
		return 0
	}
}

func valueInt(value any) int { return int(valueFloat(value)) }

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}
