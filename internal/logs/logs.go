// Package logs is kakei's audit trail: one row per logical action, whoever
// caused it and however many rows it wrote.
package logs

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
)

// Who caused an action. AI is reserved: nothing writes it yet.
const (
	User   = "user"
	System = "system"
	AI     = "ai"
)

// DB is the writing half of a handle — *sql.DB and *sql.Tx both satisfy it, so
// a log can join the transaction of the action it describes and roll back with
// it.
type DB interface {
	Exec(string, ...any) (sql.Result, error)
}

// Change is one field's move, keyed by field name in the changes JSON.
type Change struct {
	Old any `json:"old"`
	New any `json:"new"`
}

// Field is one candidate for a diff: a name and both sides.
type Field struct {
	Name     string
	Old, New any
}

func F(name string, old, new any) Field { return Field{name, old, new} }

// Diff keeps only the fields that actually moved. DeepEqual, because tags are
// a []string.
func Diff(fields ...Field) map[string]Change {
	out := map[string]Change{}
	for _, f := range fields {
		if !reflect.DeepEqual(f.Old, f.New) {
			out[f.Name] = Change{f.Old, f.New}
		}
	}
	return out
}

// Record writes one created or deleted row. The row is the whole story, so
// changes stays empty.
func Record(db DB, source, action, entity string, id int64) error {
	_, err := db.Exec(
		`INSERT INTO logs (source, action, entity, entity_id) VALUES (?, ?, ?, ?)`,
		source, action, entity, id)
	return err
}

// RecordEdit writes one edited row carrying the diff. An edit that changed
// nothing is not an event: an empty diff writes nothing.
func RecordEdit(db DB, source, entity string, id int64, changes map[string]Change) error {
	if len(changes) == 0 {
		return nil
	}
	blob, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT INTO logs (source, action, entity, entity_id, changes) VALUES (?, 'edited', ?, ?, ?)`,
		source, entity, id, string(blob))
	return err
}

// Entry is one row of the trail. Changes is the raw JSON, empty unless edited.
type Entry struct {
	ID        int64
	Source    string
	Action    string
	Entity    string
	EntityID  int64
	Changes   string
	CreatedAt string
}

// Filter narrows the listing. Zero values mean "any"; From and To are
// YYYY-MM-DD and both inclusive; a Limit of zero means 10.
type Filter struct {
	Source, Action, Entity string
	EntityID               int64
	From, To               string
	Limit                  int
}

// List returns the trail newest first.
func List(db *sql.DB, f Filter) ([]Entry, error) {
	var where []string
	var args []any
	add := func(clause string, v ...any) {
		where = append(where, clause)
		args = append(args, v...)
	}
	if f.Source != "" {
		add(`source = ?`, f.Source)
	}
	if f.Action != "" {
		add(`action = ?`, f.Action)
	}
	if f.Entity != "" {
		add(`entity = ?`, f.Entity)
	}
	if f.EntityID != 0 {
		add(`entity_id = ?`, f.EntityID)
	}
	if f.From != "" {
		add(`created_at >= ?`, f.From)
	}
	if f.To != "" {
		// created_at carries a time of day, so a plain <= would end the range
		// at midnight and lose the named day itself.
		add(`created_at < date(?, '+1 day')`, f.To)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 10
	}

	query := `SELECT id, source, action, entity, entity_id, changes, created_at FROM logs`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Source, &e.Action, &e.Entity, &e.EntityID, &e.Changes, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
