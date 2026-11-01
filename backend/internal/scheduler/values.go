package scheduler

import "strconv"

func valueString(value any) string {
	text, _ := value.(string)
	return text
}

func valueBool(value any) bool {
	if parsed, ok := value.(bool); ok {
		return parsed
	}
	if text, ok := value.(string); ok {
		parsed, _ := strconv.ParseBool(text)
		return parsed
	}
	return false
}

func valueInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}
