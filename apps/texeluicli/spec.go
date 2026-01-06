package texeluicli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type Spec struct {
	Title   string       `json:"title"`
	Layout  LayoutSpec   `json:"layout"`
	Widgets []WidgetSpec `json:"widgets"`
}

type LayoutSpec struct {
	Type       string `json:"type"`
	Gap        int    `json:"gap"`
	Padding    int    `json:"padding"`
	LabelWidth int    `json:"label_width"`
}

type WidgetSpec struct {
	ID          string      `json:"id"`
	Type        string      `json:"type"`
	Label       string      `json:"label,omitempty"`
	Text        string      `json:"text,omitempty"`
	Value       interface{} `json:"value,omitempty"`
	Options     []string    `json:"options,omitempty"`
	Height      int         `json:"height,omitempty"`
	Width       int         `json:"width,omitempty"`
	ReadOnly    bool        `json:"readonly,omitempty"`
	Placeholder string      `json:"placeholder,omitempty"`
	Flex        bool        `json:"flex,omitempty"`
	Editable    bool        `json:"editable,omitempty"`
}

func DecodeSpec(r io.Reader) (Spec, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Spec{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var spec Spec
	if err := dec.Decode(&spec); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func (s Spec) LayoutType() string {
	if s.Layout.Type == "" {
		return "form"
	}
	return s.Layout.Type
}

func (w WidgetSpec) ValueString() string {
	switch v := w.Value.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return fmt.Sprintf("%v", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (w WidgetSpec) ValueBool() bool {
	switch v := w.Value.(type) {
	case bool:
		return v
	case json.Number:
		if v == "1" {
			return true
		}
		if v == "0" {
			return false
		}
	case float64:
		return v != 0
	case string:
		return v == "true" || v == "1" || v == "yes" || v == "on"
	}
	return false
}
