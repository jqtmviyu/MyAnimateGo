package discovery

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// 上游数字和布尔值按 Java String 字段转换，前端继续接收字符串。
func decodeSourceStrings(data []byte, target any, names ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, name := range names {
		raw := bytes.TrimSpace(fields[name])
		if len(raw) == 0 || raw[0] == '"' || bytes.Equal(raw, []byte("null")) {
			continue
		}
		value := string(raw)
		if value != "true" && value != "false" {
			var number json.Number
			if err := json.Unmarshal(raw, &number); err != nil {
				return fmt.Errorf("解析字段 %s：%w", name, err)
			}
			value = number.String()
		}
		fields[name], _ = json.Marshal(value)
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, target)
}

func (anime *aniBTAnime) UnmarshalJSON(data []byte) error {
	type plain aniBTAnime
	return decodeSourceStrings(data, (*plain)(anime), "bgmId")
}

func (subject *animeGardenSubject) UnmarshalJSON(data []byte) error {
	type plain animeGardenSubject
	return decodeSourceStrings(data, (*plain)(subject), "id", "isArchived")
}

func (item *animeGardenItem) UnmarshalJSON(data []byte) error {
	type plain animeGardenItem
	return decodeSourceStrings(data, (*plain)(item), "id", "subjectId")
}

func (fansub *animeGardenFansub) UnmarshalJSON(data []byte) error {
	type plain animeGardenFansub
	return decodeSourceStrings(data, (*plain)(fansub), "id")
}

func (publisher *animeGardenPublisher) UnmarshalJSON(data []byte) error {
	type plain animeGardenPublisher
	return decodeSourceStrings(data, (*plain)(publisher), "id")
}
