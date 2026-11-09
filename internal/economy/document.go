package economy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

type document map[string]any

func parseDocument(raw json.RawMessage) (document, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var doc document
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("profile is not a JSON object: %w", err)
	}
	return doc, nil
}

func (d document) encode() (json.RawMessage, error) {
	return json.Marshal(map[string]any(d))
}

func (d document) section(name string) (document, error) {
	value, ok := d[name]
	if !ok {
		created := document{}
		d[name] = map[string]any(created)
		return created, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("profile %s is not an object", name)
	}
	return document(object), nil
}

func (d document) int64Field(name string) (int64, error) {
	return numberValue(d[name], name)
}

func (d document) intField(name string) (int, error) {
	value, err := d.int64Field(name)
	if err != nil {
		return 0, err
	}
	if value > math.MaxInt32 || value < math.MinInt32 {
		return 0, fmt.Errorf("%s is out of range", name)
	}
	return int(value), nil
}

func (d document) optionalInt(name string) (int, error) {
	if _, ok := d[name]; !ok {
		return 0, nil
	}
	return d.intField(name)
}

func (d document) optionalInt64(name string) (int64, error) {
	if _, ok := d[name]; !ok {
		return 0, nil
	}
	return d.int64Field(name)
}

func (d document) setInt(name string, value int64) {
	d[name] = json.Number(strconv.FormatInt(value, 10))
}

func (d document) array(name string) ([]any, error) {
	value, ok := d[name]
	if !ok {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s is not an array", name)
	}
	return items, nil
}

func (d document) boolField(name string) bool {
	value, _ := d[name].(bool)
	return value
}

func numberValue(value any, name string) (int64, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("%s is not a number", name)
	}
	if parsed, err := number.Int64(); err == nil {
		return parsed, nil
	}
	parsed, err := number.Float64()
	if err != nil || parsed != math.Trunc(parsed) {
		return 0, fmt.Errorf("%s is not an integer", name)
	}
	return int64(parsed), nil
}

func intArray(items []any, name string) ([]int64, error) {
	values := make([]int64, len(items))
	for i, item := range items {
		value, err := numberValue(item, fmt.Sprintf("%s[%d]", name, i))
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}

func numberArray(values []int64) []any {
	items := make([]any, len(values))
	for i, value := range values {
		items[i] = json.Number(strconv.FormatInt(value, 10))
	}
	return items
}

func (d document) floatField(name string) (float64, error) {
	number, ok := d[name].(json.Number)
	if !ok {
		return 0, fmt.Errorf("%s is not a number", name)
	}
	value, err := number.Float64()
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s is not a finite number", name)
	}
	return value, nil
}

func (d document) setFloat(name string, value float64) {
	d[name] = json.Number(strconv.FormatFloat(value, 'f', -1, 64))
}

func (d document) optionalSection(name string) (document, bool) {
	object, ok := d[name].(map[string]any)
	return document(object), ok
}
