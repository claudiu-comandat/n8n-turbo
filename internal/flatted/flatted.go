package flatted

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

func Parse(s string) (any, error) {
	var raw []json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader([]byte(s)))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse flatted: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("parse flatted: empty array")
	}
	var root string
	if err := json.Unmarshal(raw[0], &root); err == nil {
		rootIndex, err := strconv.Atoi(root)
		if err != nil {
			return nil, fmt.Errorf("parse flatted root index %q: %w", root, err)
		}
		values := raw[1:]
		if rootIndex < 0 || rootIndex >= len(values) {
			return nil, fmt.Errorf("parse flatted root index %d out of range", rootIndex)
		}
		cache := make([]any, len(values))
		seen := make([]bool, len(values))
		return reconstruct(values, rootIndex, cache, seen)
	}
	values := raw
	cache := make([]any, len(values))
	seen := make([]bool, len(values))
	return reconstruct(values, 0, cache, seen)
}

func parseLegacyRoot(raw []json.RawMessage) (any, error) {
	var root string
	if err := json.Unmarshal(raw[0], &root); err != nil {
		return nil, fmt.Errorf("parse flatted root: %w", err)
	}
	rootIndex, err := strconv.Atoi(root)
	if err != nil {
		return nil, fmt.Errorf("parse flatted root index %q: %w", root, err)
	}
	values := raw[1:]
	if rootIndex < 0 || rootIndex >= len(values) {
		return nil, fmt.Errorf("parse flatted root index %d out of range", rootIndex)
	}
	cache := make([]any, len(values))
	seen := make([]bool, len(values))
	return reconstruct(values, rootIndex, cache, seen)
}

func SimpleParse(s string) (any, error) {
	return Parse(s)
}

func ParseInto(s string, dst any) error {
	value, err := Parse(s)
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal parsed flatted: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("decode parsed flatted: %w", err)
	}
	return nil
}

func Stringify(v any) (string, error) {
	normal, err := normalize(v)
	if err != nil {
		return "", err
	}
	state := &stringifier{
		seen:   map[identity]int{},
		values: []any{},
	}
	root := state.flatten(reflect.ValueOf(normal))
	if root != 0 {
		state.values[0], state.values[root] = state.values[root], state.values[0]
		state.rewriteReferenceIndexes(map[int]int{0: root, root: 0})
	}
	data, err := json.Marshal(state.values)
	if err != nil {
		return "", fmt.Errorf("stringify flatted: %w", err)
	}
	return string(data), nil
}

func SimpleStringify(v any) (string, error) {
	return Stringify(v)
}

func IsFlattedJSON(s string) bool {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil || len(raw) == 0 {
		return false
	}
	var root string
	if json.Unmarshal(raw[0], &root) == nil {
		return true
	}
	var object map[string]any
	if json.Unmarshal(raw[0], &object) == nil {
		return true
	}
	var list []any
	return json.Unmarshal(raw[0], &list) == nil
}

func reconstruct(values []json.RawMessage, index int, cache []any, seen []bool) (any, error) {
	if index < 0 || index >= len(values) {
		return nil, fmt.Errorf("reference %d out of range", index)
	}
	if seen[index] {
		return cache[index], nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(values[index]))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode value %d: %w", index, err)
	}
	switch typed := value.(type) {
	case map[string]any:
		result := map[string]any{}
		cache[index] = result
		seen[index] = true
		for key, child := range typed {
			resolved, err := resolve(child, values, cache, seen)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			result[key] = resolved
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		cache[index] = result
		seen[index] = true
		for i, child := range typed {
			resolved, err := resolve(child, values, cache, seen)
			if err != nil {
				return nil, fmt.Errorf("%d: %w", i, err)
			}
			result[i] = resolved
		}
		return result, nil
	default:
		cache[index] = typed
		seen[index] = true
		return typed, nil
	}
}

func resolve(value any, values []json.RawMessage, cache []any, seen []bool) (any, error) {
	switch typed := value.(type) {
	case string:
		index, err := strconv.Atoi(typed)
		if err == nil && index >= 0 && index < len(values) {
			return reconstruct(values, index, cache, seen)
		}
		return typed, nil
	case []any:
		result := make([]any, len(typed))
		for i, child := range typed {
			resolved, err := resolve(child, values, cache, seen)
			if err != nil {
				return nil, err
			}
			result[i] = resolved
		}
		return result, nil
	case map[string]any:
		result := map[string]any{}
		for key, child := range typed {
			resolved, err := resolve(child, values, cache, seen)
			if err != nil {
				return nil, err
			}
			result[key] = resolved
		}
		return result, nil
	default:
		return typed, nil
	}
}

type identity struct {
	kind reflect.Kind
	ptr  uintptr
}

type stringifier struct {
	seen   map[identity]int
	values []any
}

func (s *stringifier) flatten(value reflect.Value) int {
	if !value.IsValid() {
		return s.add(nil)
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return s.add(nil)
		}
		if value.Kind() == reflect.Pointer {
			key := identity{kind: value.Kind(), ptr: value.Pointer()}
			if index, ok := s.seen[key]; ok {
				return index
			}
			index := s.reserve(key)
			s.values[index] = s.encode(value.Elem())
			return index
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Map:
		if value.IsNil() {
			return s.add(nil)
		}
		key := identity{kind: value.Kind(), ptr: value.Pointer()}
		if index, ok := s.seen[key]; ok {
			return index
		}
		index := s.reserve(key)
		s.values[index] = s.encodeMap(value)
		return index
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return s.add(nil)
		}
		if value.Kind() == reflect.Slice {
			key := identity{kind: value.Kind(), ptr: value.Pointer()}
			if index, ok := s.seen[key]; ok {
				return index
			}
			index := s.reserve(key)
			s.values[index] = s.encodeSlice(value)
			return index
		}
		return s.add(s.encodeSlice(value))
	case reflect.Struct:
		data, err := json.Marshal(value.Interface())
		if err != nil {
			return s.add(nil)
		}
		var decoded any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return s.add(nil)
		}
		return s.flatten(reflect.ValueOf(decoded))
	default:
		if value.CanInterface() {
			return s.add(value.Interface())
		}
		return s.add(nil)
	}
}

func (s *stringifier) encode(value reflect.Value) any {
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		return s.encodeMap(value)
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil
		}
		return s.encodeSlice(value)
	case reflect.Struct:
		data, err := json.Marshal(value.Interface())
		if err != nil {
			return nil
		}
		var decoded any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return nil
		}
		return strconv.Itoa(s.flatten(reflect.ValueOf(decoded)))
	default:
		if value.CanInterface() {
			return value.Interface()
		}
		return nil
	}
}

func (s *stringifier) encodeMap(value reflect.Value) map[string]any {
	result := map[string]any{}
	for _, key := range value.MapKeys() {
		result[fmt.Sprint(key.Interface())] = s.flattenReference(value.MapIndex(key))
	}
	return result
}

func (s *stringifier) encodeSlice(value reflect.Value) []any {
	result := make([]any, value.Len())
	for i := 0; i < value.Len(); i++ {
		result[i] = s.flattenReference(value.Index(i))
	}
	return result
}

func (s *stringifier) flattenReference(value reflect.Value) any {
	if isNilJSONValue(value) {
		return nil
	}
	return strconv.Itoa(s.flatten(value))
}

func isNilJSONValue(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return true
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Map, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (s *stringifier) reserve(key identity) int {
	index := len(s.values)
	s.values = append(s.values, nil)
	s.seen[key] = index
	return index
}

func (s *stringifier) add(value any) int {
	index := len(s.values)
	s.values = append(s.values, value)
	return index
}

func (s *stringifier) rewriteReferenceIndexes(mapping map[int]int) {
	for i, value := range s.values {
		s.values[i] = rewriteReferenceValue(value, mapping)
	}
}

func rewriteReferenceValue(value any, mapping map[int]int) any {
	switch typed := value.(type) {
	case string:
		index, err := strconv.Atoi(typed)
		if err == nil {
			if replacement, ok := mapping[index]; ok {
				return strconv.Itoa(replacement)
			}
		}
		return typed
	case map[string]any:
		for key, child := range typed {
			typed[key] = rewriteReferenceValue(child, mapping)
		}
		return typed
	case []any:
		for i, child := range typed {
			typed[i] = rewriteReferenceValue(child, mapping)
		}
		return typed
	default:
		return typed
	}
}

func normalize(value any) (any, error) {
	data, err := json.Marshal(value)
	if err == nil {
		var decoded any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return nil, fmt.Errorf("normalize json: %w", err)
		}
		return decoded, nil
	}
	return value, nil
}
