// Package torrent 读取种子元数据，并按原始 info 字节计算散列。
package torrent

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxBencodeDepth = 128

// File 表示种子中的一个文件；Index 保持种子内的稳定顺序。
type File struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Length int64  `json:"length"`
}

// Metadata 同时暴露 v1/v2 散列，避免混合种子被当作纯 v2。
type Metadata struct {
	Name             string `json:"name"`
	HashV1           string `json:"hashV1,omitempty"`
	HashV2           string `json:"hashV2,omitempty"`
	TransmissionHash string `json:"transmissionHash"`
	MagnetURI        string `json:"magnetURI"`
	Files            []File `json:"files"`
}

type value struct {
	kind    byte
	bytes   []byte
	integer int64
	list    []value
	dict    []entry
	index   map[string]int
	start   int
	end     int
}

type entry struct {
	key   []byte
	value value
}

// Parse 解析一份 bencoded 种子文件。
func Parse(data []byte) (*Metadata, error) {
	if len(data) == 0 {
		return nil, errors.New("种子文件为空")
	}
	position := 0
	root, err := parseValue(data, &position, 0)
	if err != nil {
		return nil, fmt.Errorf("解析 bencode 失败：%w", err)
	}
	if position != len(data) {
		return nil, errors.New("种子末尾包含多余数据")
	}
	if root.kind != 'd' {
		return nil, errors.New("种子根节点必须是字典")
	}
	info, ok := root.get("info")
	if !ok || info.kind != 'd' {
		return nil, errors.New("种子缺少 info 字典")
	}
	metadata, err := parseMetadata(info, data[info.start:info.end])
	if err != nil {
		return nil, err
	}
	return metadata, nil
}

func parseMetadata(info value, rawInfo []byte) (*Metadata, error) {
	name := textField(info, "name.utf-8", "name")
	if name == "" {
		return nil, errors.New("info 字典缺少种子名称")
	}
	metadata := &Metadata{Name: name, Files: []File{}}
	hasV1 := false
	if pieces, ok := info.get("pieces"); ok {
		if pieces.kind != 's' {
			return nil, errors.New("v1 pieces 字段必须是字节字符串")
		}
		hash := sha1.Sum(rawInfo)
		metadata.HashV1 = hex.EncodeToString(hash[:])
		hasV1 = true
	}
	metaVersion, hasMetaVersion := info.get("meta version")
	hasV2 := hasMetaVersion && metaVersion.kind == 'i' && metaVersion.integer == 2
	if hasV1 {
		if files, ok := info.get("files"); ok {
			if files.kind != 'l' {
				return nil, errors.New("v1 files 字段必须是列表")
			}
			if err := appendV1Files(&metadata.Files, files.list); err != nil {
				return nil, err
			}
		} else {
			length, ok := integerField(info, "length")
			if !ok || length < 0 {
				return nil, errors.New("单文件种子缺少有效长度")
			}
			metadata.Files = append(metadata.Files, File{Name: name, Length: length})
		}
	}
	if hasV2 {
		tree, ok := info.get("file tree")
		if !ok || tree.kind != 'd' {
			return nil, errors.New("v2 种子缺少 file tree 字典")
		}
		hash := sha256.Sum256(rawInfo)
		metadata.HashV2 = hex.EncodeToString(hash[:])
		if !hasV1 {
			if err := appendV2Files(&metadata.Files, tree, nil); err != nil {
				return nil, err
			}
		}
	}
	if !hasV1 && !hasV2 {
		return nil, errors.New("种子没有可识别的 v1 或 v2 元数据")
	}
	if len(metadata.Files) == 0 {
		return nil, errors.New("种子没有文件条目")
	}
	for i := range metadata.Files {
		metadata.Files[i].Index = i
	}
	metadata.TransmissionHash = metadata.HashV1
	if metadata.TransmissionHash == "" && metadata.HashV2 != "" {
		metadata.TransmissionHash = metadata.HashV2[:40]
	}
	topics := make([]string, 0, 2)
	if metadata.HashV1 != "" {
		topics = append(topics, "xt=urn:btih:"+metadata.HashV1)
	}
	if metadata.HashV2 != "" {
		topics = append(topics, "xt=urn:btmh:1220"+metadata.HashV2)
	}
	metadata.MagnetURI = "magnet:?" + strings.Join(topics, "&")
	return metadata, nil
}

func appendV1Files(files *[]File, list []value) error {
	for _, item := range list {
		if item.kind != 'd' {
			return errors.New("v1 文件条目必须是字典")
		}
		length, ok := integerField(item, "length")
		if !ok || length < 0 {
			return errors.New("v1 文件条目缺少有效长度")
		}
		path, ok := item.get("path.utf-8")
		if !ok || path.kind != 'l' {
			path, ok = item.get("path")
		}
		if !ok || path.kind != 'l' || len(path.list) == 0 {
			return errors.New("v1 文件条目缺少有效路径")
		}
		segments := make([]string, 0, len(path.list))
		for _, segment := range path.list {
			if segment.kind != 's' || len(segment.bytes) == 0 {
				return errors.New("v1 文件路径必须由非空字节字符串组成")
			}
			segments = append(segments, displayText(segment.bytes))
		}
		*files = append(*files, File{Name: strings.Join(segments, "/"), Length: length})
	}
	return nil
}

func appendV2Files(files *[]File, tree value, path []string) error {
	for _, item := range tree.dict {
		if len(item.key) == 0 {
			if item.value.kind != 'd' {
				return errors.New("v2 文件叶节点必须是字典")
			}
			length, ok := integerField(item.value, "length")
			if !ok || length < 0 {
				return errors.New("v2 文件叶节点缺少有效长度")
			}
			if len(path) == 0 {
				return errors.New("v2 文件树中的文件路径为空")
			}
			*files = append(*files, File{Name: strings.Join(path, "/"), Length: length})
			continue
		}
		if item.value.kind != 'd' {
			return errors.New("v2 文件树目录必须是字典")
		}
		child := append(append([]string(nil), path...), displayText(item.key))
		if err := appendV2Files(files, item.value, child); err != nil {
			return err
		}
	}
	return nil
}

func textField(info value, preferred, fallback string) string {
	if item, ok := info.get(preferred); ok && item.kind == 's' && utf8.Valid(item.bytes) {
		return string(item.bytes)
	}
	if item, ok := info.get(fallback); ok && item.kind == 's' {
		return displayText(item.bytes)
	}
	return ""
}

func integerField(info value, key string) (int64, bool) {
	item, ok := info.get(key)
	return item.integer, ok && item.kind == 'i'
}

func displayText(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	runes := make([]rune, len(data))
	for i, b := range data {
		runes[i] = rune(b)
	}
	return string(runes)
}

func (v value) get(key string) (value, bool) {
	if v.kind != 'd' || v.index == nil {
		return value{}, false
	}
	index, ok := v.index[key]
	if !ok {
		return value{}, false
	}
	return v.dict[index].value, true
}

func parseValue(data []byte, position *int, depth int) (value, error) {
	if depth > maxBencodeDepth {
		return value{}, errors.New("bencode 嵌套层数过深")
	}
	if *position >= len(data) {
		return value{}, errors.New("bencode 数据意外结束")
	}
	result := value{start: *position}
	switch data[*position] {
	case 'i':
		result.kind = 'i'
		*position++
		start := *position
		for *position < len(data) && data[*position] != 'e' {
			*position++
		}
		if *position >= len(data) || *position == start {
			return value{}, errors.New("bencode 整数格式无效")
		}
		raw := string(data[start:*position])
		if raw == "-0" || (len(raw) > 1 && raw[0] == '0') || (len(raw) > 2 && raw[0] == '-' && raw[1] == '0') {
			return value{}, errors.New("bencode 整数格式无效")
		}
		integer, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return value{}, errors.New("bencode 整数超出范围")
		}
		result.integer = integer
		*position++
	case 'l':
		result.kind = 'l'
		*position++
		for {
			if *position >= len(data) {
				return value{}, errors.New("bencode 列表未闭合")
			}
			if data[*position] == 'e' {
				*position++
				break
			}
			item, err := parseValue(data, position, depth+1)
			if err != nil {
				return value{}, err
			}
			result.list = append(result.list, item)
		}
	case 'd':
		result.kind = 'd'
		result.index = make(map[string]int)
		*position++
		for {
			if *position >= len(data) {
				return value{}, errors.New("bencode 字典未闭合")
			}
			if data[*position] == 'e' {
				*position++
				break
			}
			key, err := parseValue(data, position, depth+1)
			if err != nil || key.kind != 's' {
				return value{}, errors.New("bencode 字典键必须是字节字符串")
			}
			name := string(key.bytes)
			if _, exists := result.index[name]; exists {
				return value{}, errors.New("bencode 字典包含重复键")
			}
			item, err := parseValue(data, position, depth+1)
			if err != nil {
				return value{}, err
			}
			result.index[name] = len(result.dict)
			result.dict = append(result.dict, entry{key: key.bytes, value: item})
		}
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		result.kind = 's'
		length, err := readStringLength(data, position)
		if err != nil {
			return value{}, err
		}
		if length > len(data)-*position {
			return value{}, errors.New("bencode 字节字符串长度超出输入")
		}
		result.bytes = data[*position : *position+length]
		*position += length
	default:
		return value{}, fmt.Errorf("未知 bencode 标记 0x%02x", data[*position])
	}
	result.end = *position
	return result, nil
}

func readStringLength(data []byte, position *int) (int, error) {
	start := *position
	for *position < len(data) && data[*position] != ':' {
		if data[*position] < '0' || data[*position] > '9' {
			return 0, errors.New("bencode 字节字符串长度格式无效")
		}
		*position++
	}
	if *position == start || *position >= len(data) {
		return 0, errors.New("bencode 字节字符串缺少长度或冒号")
	}
	raw := data[start:*position]
	if len(raw) > 1 && raw[0] == '0' {
		return 0, errors.New("bencode 字节字符串长度格式无效")
	}
	length, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil || length > uint64(math.MaxInt) {
		return 0, errors.New("bencode 字节字符串长度超出范围")
	}
	*position++
	return int(length), nil
}
