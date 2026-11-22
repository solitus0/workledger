package worklogs

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Metadata stores JSON text. Its zero value represents absent metadata, and
// the JSON literal null represents a stored value.
type Metadata string

func ParseMetadata(value string) (Metadata, error) {
	if !utf8.ValidString(value) || !json.Valid([]byte(value)) {
		return "", errors.New("must contain one valid JSON value")
	}
	return Metadata(strings.TrimSpace(value)), nil
}

func validateMetadata(value Metadata, field string) error {
	if value == "" {
		return nil
	}
	if _, err := ParseMetadata(string(value)); err != nil {
		return ValidationError{Issues: []ValidationIssue{{Field: field, Message: err.Error()}}}
	}
	return nil
}

func (m Metadata) Value() (driver.Value, error) {
	if m == "" {
		return nil, nil
	}
	return string(m), nil
}

func (m *Metadata) Scan(src any) error {
	var value sql.NullString
	if err := value.Scan(src); err != nil {
		return err
	}
	*m = Metadata(value.String)
	return nil
}

type MetadataFilter struct {
	tokens []string
	value  any
}

func NewMetadataFilter(pointer, value string) (*MetadataFilter, error) {
	filter := &MetadataFilter{}
	if pointer != "" {
		path, ok := strings.CutPrefix(pointer, "/")
		if !ok {
			return nil, errors.New("--metadata-path must be a JSON Pointer, starting with / or empty for the root")
		}
		filter.tokens = strings.Split(path, "/")
		unescape := strings.NewReplacer("~1", "/", "~0", "~")
		for i, token := range filter.tokens {
			for rest := token; strings.Contains(rest, "~"); {
				_, escape, _ := strings.Cut(rest, "~")
				if escape == "" || (escape[0] != '0' && escape[0] != '1') {
					return nil, errors.New("--metadata-path accepts only ~0 and ~1 as escapes")
				}
				rest = escape[1:]
			}
			filter.tokens[i] = unescape.Replace(token)
		}
	}
	metadata, err := ParseMetadata(value)
	if err != nil {
		return nil, errors.New("--metadata-value must be a valid JSON scalar")
	}
	filter.value, err = decodeMetadata(metadata)
	if err != nil {
		return nil, err
	}
	switch filter.value.(type) {
	case nil, bool, string, json.Number:
		return filter, nil
	default:
		return nil, errors.New("--metadata-value must be a JSON scalar")
	}
}

func decodeMetadata(metadata Metadata) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(metadata)))
	decoder.UseNumber()
	var value any
	err := decoder.Decode(&value)
	return value, err
}

func (filter *MetadataFilter) matches(metadata Metadata) bool {
	if metadata == "" {
		return false
	}
	value, err := decodeMetadata(metadata)
	if err != nil {
		return false
	}
	for _, token := range filter.tokens {
		switch node := value.(type) {
		case map[string]any:
			var found bool
			value, found = node[token]
			if !found {
				return false
			}
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(node) || strconv.Itoa(index) != token {
				return false
			}
			value = node[index]
		default:
			return false
		}
	}
	if array, ok := value.([]any); ok {
		for _, member := range array {
			if member == filter.value {
				return true
			}
		}
		return false
	}
	return value == filter.value
}
