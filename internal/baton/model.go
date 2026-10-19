package baton

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const SchemaVersion = 1
const maxJSON = 8 << 20 // Resource safety limit, not the summary budget.

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Project struct {
	Schema    int       `json:"schema"`
	ID        string    `json:"id"`
	Root      string    `json:"root"`
	CreatedAt time.Time `json:"created_at"`
}

type Task struct {
	Schema    int       `json:"schema"`
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

type Decision struct {
	Decision  string   `json:"decision"`
	Rationale string   `json:"rationale"`
	Evidence  []string `json:"evidence"`
}

type Verification struct {
	Check    string `json:"check"`
	Result   string `json:"result"` // passed, failed, not_run, partial
	Evidence string `json:"evidence"`
}

type Content struct {
	Goal                string         `json:"goal"`
	Acceptance          []string       `json:"acceptance"`
	NonGoals            []string       `json:"non_goals"`
	Completed           []string       `json:"completed"`
	InProgress          []string       `json:"in_progress"`
	Decisions           []Decision     `json:"decisions"`
	Blockers            []string       `json:"blockers"`
	NextSteps           []string       `json:"next_steps"`
	Verification        []Verification `json:"verification"`
	Unverified          []string       `json:"unverified"`
	Evidence            []string       `json:"evidence"`
	PermissionReminders []string       `json:"permission_reminders"`
}

type FileStamp struct {
	Path     string `json:"path"`
	Mode     uint32 `json:"mode"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified_ns"`
	Exists   bool   `json:"exists"`
}

type Workspace struct {
	Root        string      `json:"root"`
	RootID      string      `json:"root_id,omitempty"`
	Kind        string      `json:"kind"`
	GitDir      string      `json:"git_dir,omitempty"`
	GitID       string      `json:"git_id,omitempty"`
	Branch      string      `json:"branch,omitempty"`
	Commit      string      `json:"commit,omitempty"`
	IndexDigest string      `json:"index_digest,omitempty"`
	Status      string      `json:"status,omitempty"`
	Files       []FileStamp `json:"files"`
	WIPPaths    []string    `json:"wip_paths"`
}

type Revision struct {
	Schema       int       `json:"schema"`
	ProjectID    string    `json:"project_id"`
	TaskID       string    `json:"task_id"`
	Number       int       `json:"revision"`
	ParentDigest string    `json:"parent_digest"`
	Source       string    `json:"source"`
	Session      string    `json:"session"`
	CreatedAt    time.Time `json:"created_at"`
	RestoredFrom int       `json:"restored_from,omitempty"`
	Workspace    Workspace `json:"workspace"`
	Content      Content   `json:"content"`
}

type Head struct {
	Revision int    `json:"revision"`
	Digest   string `json:"sha256"`
}

type Binding struct {
	Schema    int       `json:"schema"`
	ProjectID string    `json:"project_id"`
	TaskID    string    `json:"task_id"`
	Session   string    `json:"session"`
	Revision  int       `json:"revision"`
	Workspace Workspace `json:"workspace"`
}

type Writer struct {
	Session string `json:"session"`
	TaskID  string `json:"task_id"`
}

type ConflictError struct {
	Expected int
	Actual   int
	Draft    string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("revision conflict: expected %d, current %d; draft retained at %s; reload before reconciling", e.Expected, e.Actual, e.Draft)
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func digest(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func validID(id string) error {
	if !idPattern.MatchString(id) {
		return errors.New("ID must contain 32 lowercase hexadecimal characters")
	}
	return nil
}
func validLabel(s string) error {
	if strings.TrimSpace(s) == "" || len(s) > 200 || strings.ContainsAny(s, "\x00\r\n") || !utf8.ValidString(s) {
		return errors.New("session/source/title must be a nonempty UTF-8 label (up to 200 bytes, no newlines)")
	}
	return nil
}

func (c Content) Validate() error {
	if strings.TrimSpace(c.Goal) == "" {
		return errors.New("goal is required")
	}
	for _, d := range c.Decisions {
		if strings.TrimSpace(d.Decision) == "" || strings.TrimSpace(d.Rationale) == "" {
			return errors.New("each decision needs a decision and rationale")
		}
	}
	for _, v := range c.Verification {
		if strings.TrimSpace(v.Check) == "" {
			return errors.New("verification check is required")
		}
		switch v.Result {
		case "passed", "failed", "not_run", "partial":
		default:
			return fmt.Errorf("invalid verification result %q", v.Result)
		}
	}
	return nil
}

// DecodeStrict rejects unknown fields, duplicate object keys, invalid UTF-8 and
// trailing values. Duplicate keys otherwise silently replace safety-critical data.
func DecodeStrict(b []byte, v any) error {
	if len(b) > maxJSON {
		return errors.New("JSON exceeds 8 MiB resource limit")
	}
	if !utf8.Valid(b) {
		return errors.New("JSON must be UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if err := uniqueValue(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	if err := exactKeys(b, reflect.TypeOf(v)); err != nil {
		return err
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// encoding/json accepts case-insensitive field names. Reject those aliases so
// GOAL and goal cannot silently overwrite the same schema field.
func exactKeys(raw json.RawMessage, typ reflect.Type) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.Struct && reflect.PointerTo(typ).Implements(reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()) {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		known := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				known[name] = f.Type
			}
		}
		for key, value := range fields {
			t, ok := known[key]
			if !ok {
				return fmt.Errorf("unknown JSON field %q", key)
			}
			if err := exactKeys(value, t); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, value := range values {
			if err := exactKeys(value, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func uniqueValue(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	if delim, ok := t.(json.Delim); ok {
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				s, ok := key.(string)
				if !ok || seen[s] {
					return errors.New("invalid or duplicate JSON key")
				}
				seen[s] = true
				if err := uniqueValue(d); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := uniqueValue(d); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = d.Token()
	}
	return err
}

func encode(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	if len(b) > maxJSON {
		return nil, errors.New("JSON exceeds 8 MiB resource limit")
	}
	return b, nil
}
