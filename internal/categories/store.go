package categories

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"pecunia/internal/core"
	"pecunia/internal/logs"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const columns = `id, code, name, description, color, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (Category, error) {
	var c Category
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.Description, &c.Color, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (s *Store) List() ([]Category, error) {
	rows, err := s.db.Query(`SELECT ` + columns + ` FROM categories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Category
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Get(id int64) (Category, error) {
	c, err := scan(s.db.QueryRow(`SELECT `+columns+` FROM categories WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) ByCode(code string) (Category, error) {
	c, err := scan(s.db.QueryRow(`SELECT `+columns+` FROM categories WHERE code = ?`, core.NormalizeCode(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// Resolve looks a reference up as an id when it is all digits, otherwise as a
// code — that is what lets every command take {CODE|ID}.
func (s *Store) Resolve(ref string) (Category, error) {
	ref = strings.TrimSpace(ref)
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return s.Get(id)
	}
	return s.ByCode(ref)
}

// Create takes who is creating — the only write with two authors: the user at
// their form, and Seed laying down the starter set.
func (s *Store) Create(c *Category, source string) error {
	if err := core.ValidateName(c.Name); err != nil {
		return err
	}
	c.Code = core.NormalizeCode(c.Code)
	res, err := s.db.Exec(
		`INSERT INTO categories (code, name, description, color) VALUES (?, ?, ?, ?)`,
		c.Code, c.Name, c.Description, c.Color)
	if err != nil {
		return core.CodeErr(err, c.Code)
	}
	if c.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	return logs.Record(s.db, source, "created", "category", c.ID)
}

func (s *Store) Update(c Category) error {
	if err := core.ValidateName(c.Name); err != nil {
		return err
	}
	old, err := s.Get(c.ID)
	if err != nil {
		return err
	}
	c.Code = core.NormalizeCode(c.Code)
	res, err := s.db.Exec(
		`UPDATE categories SET code = ?, name = ?, description = ?, color = ?,
		 updated_at = datetime('now') WHERE id = ?`,
		c.Code, c.Name, c.Description, c.Color, c.ID)
	if err != nil {
		return core.CodeErr(err, c.Code)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return logs.RecordEdit(s.db, logs.Actor, "category", c.ID, logs.Diff(
		logs.F("code", old.Code, c.Code),
		logs.F("name", old.Name, c.Name),
		logs.F("description", old.Description, c.Description),
		logs.F("color", old.Color, c.Color),
	))
}

func (s *Store) Delete(id int64) error {
	res, err := s.db.Exec(`DELETE FROM categories WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return logs.Record(s.db, logs.Actor, "deleted", "category", id)
}

func (s *Store) CodeTaken(code string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM categories WHERE code = ?`, core.NormalizeCode(code)).Scan(&n)
	return n > 0, err
}

// SuggestCode returns a free code to pre-fill the form with.
func (s *Store) SuggestCode() (string, error) { return core.SuggestCode(s.CodeTaken) }

// Starter is what a database begins with. The codes are written by hand rather
// than generated so they read like what they name, and they are the user's rows
// from the first run on — edit or delete them freely.
var Starter = []Category{
	{Code: "HOME1", Name: "Home", Description: "rent, mortgage, repairs", Color: "blue"},
	{Code: "UTILS", Name: "Utilities", Description: "power, water, internet, phone", Color: "cyan"},
	{Code: "FOOD1", Name: "Food & Groceries", Description: "supermarket and the market", Color: "lime"},
	{Code: "TRANS", Name: "Transport", Description: "fuel, fares, rideshare", Color: "teal"},
	{Code: "HLTH1", Name: "Health & Medical", Description: "doctors, pharmacy, insurance", Color: "red"},
	{Code: "RESTA", Name: "Restaurants", Description: "eating out and delivery", Color: "orange"},
	{Code: "ENTER", Name: "Entertainment", Description: "streaming, games, cinema", Color: "violet"},
	{Code: "CARE1", Name: "Personal Care", Description: "haircuts, cosmetics, gym", Color: "pink"},
	{Code: "GIFTS", Name: "Gifts", Description: "presents given and received", Color: "amber"},
	{Code: "EDUC1", Name: "Educational", Description: "courses, books, tuition", Color: "indigo"},
	{Code: "LOVE1", Name: "Love", Color: "pink"},
	{Code: "FMLY1", Name: "Family", Color: "amber"},
	{Code: "PETS1", Name: "Pets", Description: "food, vet, grooming", Color: "orange"},
	{Code: "HOBBY", Name: "Hobbies", Color: "yellow"},
	{Code: "WORK1", Name: "Work", Description: "work expenses and reimbursements", Color: "indigo"},
	{Code: "SLRY1", Name: "Salary", Description: "what the job pays", Color: "green"},
	{Code: "INVST", Name: "Investment", Description: "contributions and returns", Color: "green"},
	{Code: "DEBT1", Name: "Debts & Loan", Description: "instalments and interest", Color: "red"},
	{Code: "LSURE", Name: "Leisure", Description: "trips, outings, days off", Color: "violet"},
}

// Seed inserts every Starter category whose code is free and reports how many
// it added. Skipping taken codes is what makes it safe to run on every setup: a
// starter the user renamed keeps its edit, and one they deleted is only ever
// restored by its code coming free again.
func Seed(s *Store) (int, error) {
	n := 0
	for _, c := range Starter {
		taken, err := s.CodeTaken(c.Code)
		if err != nil {
			return n, err
		}
		if taken {
			continue
		}
		if err := s.Create(&c, logs.System); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
